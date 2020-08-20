package state

import (
	"fmt"
	"time"

	dbm "github.com/tendermint/tm-db"

	abcix "github.com/tendermint/tendermint/abcix/types"
	cryptoenc "github.com/tendermint/tendermint/crypto/encoding"
	"github.com/tendermint/tendermint/libs/fail"
	"github.com/tendermint/tendermint/libs/log"
	mempl "github.com/tendermint/tendermint/mempool"
	tmstate "github.com/tendermint/tendermint/proto/tendermint/state"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	"github.com/tendermint/tendermint/proxy"
	"github.com/tendermint/tendermint/types"
)

//-----------------------------------------------------------------------------
// BlockExecutor handles block execution and state updates.
// It exposes ApplyBlock(), which validates & executes the block, updates state w/ ABCI responses,
// then commits and updates the mempool atomically, then saves state.

// BlockExecutor provides the context and accessories for properly executing a block.
type BlockExecutor struct {
	// save state, validators, consensus params, abci responses here
	db dbm.DB

	// execute the app against this
	proxyApp proxy.AppConnConsensus

	// events
	eventBus types.BlockEventPublisher

	// manage the mempool lock during commit
	// and update both with block results after commit.
	mempool mempl.Mempool
	evpool  EvidencePool

	logger log.Logger

	metrics *Metrics
}

type BlockExecutorOption func(executor *BlockExecutor)

func BlockExecutorWithMetrics(metrics *Metrics) BlockExecutorOption {
	return func(blockExec *BlockExecutor) {
		blockExec.metrics = metrics
	}
}

// NewBlockExecutor returns a new BlockExecutor with a NopEventBus.
// Call SetEventBus to provide one.
func NewBlockExecutor(
	db dbm.DB,
	logger log.Logger,
	proxyApp proxy.AppConnConsensus,
	mempool mempl.Mempool,
	evpool EvidencePool,
	options ...BlockExecutorOption,
) *BlockExecutor {
	res := &BlockExecutor{
		db:       db,
		proxyApp: proxyApp,
		eventBus: types.NopEventBus{},
		mempool:  mempool,
		evpool:   evpool,
		logger:   logger,
		metrics:  NopMetrics(),
	}

	for _, option := range options {
		option(res)
	}

	return res
}

func (blockExec *BlockExecutor) DB() dbm.DB {
	return blockExec.db
}

// SetEventBus - sets the event bus for publishing block related events.
// If not called, it defaults to types.NopEventBus.
func (blockExec *BlockExecutor) SetEventBus(eventBus types.BlockEventPublisher) {
	blockExec.eventBus = eventBus
}

// CreateProposalBlock calls state.MakeBlock with evidence from the evpool
// and txs from the mempool. The max bytes must be big enough to fit the commit.
// Up to 1/10th of the block space is allocated for maximum sized evidence.
// The rest is given to txs, up to the max gas.
func (blockExec *BlockExecutor) CreateProposalBlock(
	height int64,
	state State, commit *types.Commit,
	proposerAddr []byte,
) (*types.Block, *types.PartSet) {
	var txs types.Txs
	var timestamp time.Time
	evidence := blockExec.evpool.PendingEvidence(state.ConsensusParams.Evidence.MaxNum)

	if height == 1 {
		timestamp = state.LastBlockTime // genesis time
	} else {
		timestamp = MedianTime(commit, state.LastValidators)
	}
	lastCommitInfo, byzVals := getBlockValidatorInfo(timestamp, height, commit, evidence, blockExec.db)
	resp, err := blockExec.proxyApp.CreateBlockSync(abcix.RequestCreateBlock{
		Height:              height,
		LastCommitInfo:      lastCommitInfo,
		ByzantineValidators: byzVals,
	}, abcix.NewMempoolIter(blockExec.mempool))
	if err != nil {
		panic(err)
	}

	// remove invalid txs from mempool
	var invalidTxs = make([]types.Tx, len(resp.InvalidTxs))
	var deliverTxResponses = make([]*abcix.ResponseDeliverTx, len(resp.InvalidTxs))
	for i, invalidTx := range resp.InvalidTxs {
		copy(invalidTxs[i], invalidTx)
		deliverTxResponses[i].Code = 1
	}
	err = blockExec.mempool.Update(
		height,
		invalidTxs,
		deliverTxResponses,
		TxPreCheck(state),
		TxPostCheck(state),
	)
	if err != nil {
		panic(err)
	}

	for _, txBytes := range resp.Txs {
		txs = append(txs, txBytes)
	}
	return state.MakeBlock(height, txs, commit, evidence, proposerAddr)
}

// ValidateBlock validates the given block against the given state.
// If the block is invalid, it returns an error.
// Validation does not mutate state, but does require historical information from the stateDB,
// ie. to verify evidence from a validator at an old height.
func (blockExec *BlockExecutor) ValidateBlock(state State, block *types.Block) error {
	return validateBlock(blockExec.evpool, blockExec.db, state, block)
}

// ApplyBlock validates the block against the state, executes it against the app,
// fires the relevant events, commits the app, and saves the new state and responses.
// It returns the new state and the block height to retain (pruning older blocks).
// It's the only function that needs to be called
// from outside this package to process and commit an entire block.
// It takes a blockID to avoid recomputing the parts hash.
func (blockExec *BlockExecutor) ApplyBlock(
	state State, blockID types.BlockID, block *types.Block,
) (State, int64, error) {

	if err := blockExec.ValidateBlock(state, block); err != nil {
		return state, 0, ErrInvalidBlock(err)
	}

	startTime := time.Now().UnixNano()
	abciResponses, err := execBlockOnProxyApp(blockExec.logger, blockExec.proxyApp, block, blockExec.db)
	endTime := time.Now().UnixNano()
	blockExec.metrics.BlockProcessingTime.Observe(float64(endTime-startTime) / 1000000)
	if err != nil {
		return state, 0, ErrProxyAppConn(err)
	}

	fail.Fail() // XXX

	// Save the results before we commit.
	SaveABCIResponses(blockExec.db, block.Height, abciResponses)

	fail.Fail() // XXX

	// validate the validator updates and convert to tendermint types
	abciValUpdates := abciResponses.DeliverBlock.ValidatorUpdates
	err = validateValidatorUpdates(abciValUpdates, state.ConsensusParams.Validator)
	if err != nil {
		return state, 0, fmt.Errorf("error in validator updates: %v", err)
	}
	validatorUpdates, err := types.PB2TM.ValidatorUpdates(abciValUpdates)
	if err != nil {
		return state, 0, err
	}
	if len(validatorUpdates) > 0 {
		blockExec.logger.Info("Updates to validators", "updates", types.ValidatorListString(validatorUpdates))
	}

	// Update the state with the block and responses.
	state, err = updateState(state, blockID, &block.Header, abciResponses, validatorUpdates)
	if err != nil {
		return state, 0, fmt.Errorf("commit failed for application: %v", err)
	}

	// Lock mempool, commit app state, update mempoool.
	appHash, retainHeight, err := blockExec.Commit(state, block, abciResponses.DeliverBlock)
	if err != nil {
		return state, 0, fmt.Errorf("commit failed for application: %v", err)
	}

	// Update evpool with the block and state.
	blockExec.evpool.Update(block, state)

	fail.Fail() // XXX

	// Update the app hash and save the state.
	state.AppHash = appHash
	SaveState(blockExec.db, state)

	fail.Fail() // XXX

	// Events are fired after everything else.
	// NOTE: if we crash between Commit and Save, events wont be fired during replay
	fireEvents(blockExec.logger, blockExec.eventBus, block, abciResponses, validatorUpdates)

	return state, retainHeight, nil
}

// Commit locks the mempool, runs the ABCI Commit message, and updates the
// mempool.
// It returns the result of calling abci.Commit (the AppHash) and the height to retain (if any).
// The Mempool must be locked during commit and update because state is
// typically reset on Commit and old txs must be replayed against committed
// state before new txs are run in the mempool, lest they be invalid.
func (blockExec *BlockExecutor) Commit(
	state State,
	block *types.Block,
	deliverBlockResponse *abcix.ResponseDeliverBlock,
) ([]byte, int64, error) {
	blockExec.mempool.Lock()
	defer blockExec.mempool.Unlock()

	// while mempool is Locked, flush to ensure all async requests have completed
	// in the ABCI app before Commit.
	err := blockExec.mempool.FlushAppConn()
	if err != nil {
		blockExec.logger.Error("Client error during mempool.FlushAppConn", "err", err)
		return nil, 0, err
	}

	// Commit block, get hash back
	res, err := blockExec.proxyApp.CommitSync()
	if err != nil {
		blockExec.logger.Error(
			"Client error during proxyAppConn.CommitSync",
			"err", err,
		)
		return nil, 0, err
	}
	// ResponseCommit has no error code - just data

	blockExec.logger.Info(
		"Committed state",
		"height", block.Height,
		"txs", len(block.Txs),
		"appHash", fmt.Sprintf("%X", res.Data),
	)

	// Update mempool.
	err = blockExec.mempool.Update(
		block.Height,
		block.Txs,
		deliverBlockResponse.DeliverTxs,
		TxPreCheck(state),
		TxPostCheck(state),
	)

	return res.Data, res.RetainHeight, err
}

//---------------------------------------------------------
// Helper functions for executing blocks and updating state

// Executes block's transactions on proxyAppConn.
// Returns a list of transaction results and updates to the validator set
func execBlockOnProxyApp(
	logger log.Logger,
	proxyAppConn proxy.AppConnConsensus,
	block *types.Block,
	stateDB dbm.DB,
) (*tmstate.ABCIResponses, error) {
	abciResponses := new(tmstate.ABCIResponses)
	dtxs := make([]*abcix.ResponseDeliverTx, len(block.Txs))
	abciResponses.DeliverBlock = &abcix.ResponseDeliverBlock{
		DeliverTxs: dtxs,
	}

	commitInfo, byzVals := getBlockValidatorInfo(
		block.Time,
		block.Height,
		block.LastCommit,
		block.Evidence.Evidence,
		stateDB,
	)

	pbh := block.Header.ToProto()
	var err error

	txs := make([][]byte, 0, len(block.Txs))
	for _, tx := range block.Txs {
		txs = append(txs, tx)
	}
	abciResponses.DeliverBlock, err = proxyAppConn.DeliverBlockSync(abcix.RequestDeliverBlock{
		Height:              block.Height,
		Hash:                block.Hash(),
		Header:              *pbh,
		LastCommitInfo:      commitInfo,
		ByzantineValidators: byzVals,
		Txs:                 txs,
	})
	if err != nil {
		logger.Error("Error in proxyAppConn.DeliverBlock", "err", err)
	}

	for _, txRes := range abciResponses.DeliverBlock.DeliverTxs {
		if txRes.Code != abcix.CodeTypeOK {
			// Consensus failure, because invalid tx should already be thrown away when creating block
			panic("invalid tx found in block")
		}
	}

	logger.Info("Executed block", "height", block.Height)
	return abciResponses, nil
}

func getBlockValidatorInfo(
	time time.Time,
	height int64,
	lastCommit *types.Commit,
	evidence []types.Evidence,
	stateDB dbm.DB,
) (abcix.LastCommitInfo, []abcix.Evidence) {
	voteInfos := make([]abcix.VoteInfo, lastCommit.Size())
	// block.Height=1 -> LastCommitInfo.Votes are empty.
	// Remember that the first LastCommit is intentionally empty, so it makes
	// sense for LastCommitInfo.Votes to also be empty.
	if height > 1 {
		lastValSet, err := LoadValidators(stateDB, height-1)
		if err != nil {
			panic(err)
		}

		var (
			commitSize = lastCommit.Size()
			valSetLen  = len(lastValSet.Validators)
		)
		if commitSize != valSetLen {
			panic(fmt.Sprintf("commit size (%d) doesn't match valset length (%d) at height %d\n\n%v\n\n%v",
				commitSize, valSetLen, height, lastCommit.Signatures, lastValSet.Validators))
		}

		for i, val := range lastValSet.Validators {
			commitSig := lastCommit.Signatures[i]
			voteInfos[i] = abcix.VoteInfo{
				Validator:       types.TM2PB.Validator(val),
				SignedLastBlock: !commitSig.Absent(),
			}
		}
	}

	byzVals := make([]abcix.Evidence, len(evidence))
	for i, ev := range evidence {
		valset, err := LoadValidators(stateDB, ev.Height())
		if err != nil {
			panic(err)
		}
		byzVals[i] = types.TM2PB.Evidence(ev, valset, time)
	}

	return abcix.LastCommitInfo{
		Round: lastCommit.Round,
		Votes: voteInfos,
	}, byzVals
}

func validateValidatorUpdates(abciUpdates []abcix.ValidatorUpdate,
	params tmproto.ValidatorParams) error {
	for _, valUpdate := range abciUpdates {
		if valUpdate.GetPower() < 0 {
			return fmt.Errorf("voting power can't be negative %v", valUpdate)
		} else if valUpdate.GetPower() == 0 {
			// continue, since this is deleting the validator, and thus there is no
			// pubkey to check
			continue
		}

		// Check if validator's pubkey matches an ABCI type in the consensus params
		pk, err := cryptoenc.PubKeyFromProto(valUpdate.PubKey)
		if err != nil {
			return err
		}

		if !types.IsValidPubkeyType(params, pk.Type()) {
			return fmt.Errorf("validator %v is using pubkey %s, which is unsupported for consensus",
				valUpdate, pk.Type())
		}
	}
	return nil
}

// updateState returns a new State updated according to the header and responses.
func updateState(
	state State,
	blockID types.BlockID,
	header *types.Header,
	abciResponses *tmstate.ABCIResponses,
	validatorUpdates []*types.Validator,
) (State, error) {

	// Copy the valset so we can apply changes from EndBlock
	// and update s.LastValidators and s.Validators.
	nValSet := state.NextValidators.Copy()

	// Update the validator set with the latest abciResponses.
	lastHeightValsChanged := state.LastHeightValidatorsChanged
	if len(validatorUpdates) > 0 {
		err := nValSet.UpdateWithChangeSet(validatorUpdates)
		if err != nil {
			return state, fmt.Errorf("error changing validator set: %v", err)
		}
		// Change results from this height but only applies to the next next height.
		lastHeightValsChanged = header.Height + 1 + 1
	}

	// Update validator proposer priority and set state variables.
	nValSet.IncrementProposerPriority(1)

	// Update the params with the latest abciResponses.
	nextParams := state.ConsensusParams
	lastHeightParamsChanged := state.LastHeightConsensusParamsChanged
	if abciResponses.DeliverBlock.ConsensusParamUpdates != nil {
		// NOTE: must not mutate s.ConsensusParams
		nextParams = types.UpdateConsensusParams(state.ConsensusParams, abciResponses.DeliverBlock.ConsensusParamUpdates)
		err := types.ValidateConsensusParams(nextParams)
		if err != nil {
			return state, fmt.Errorf("error updating consensus params: %v", err)
		}

		state.Version.Consensus.App = nextParams.Version.AppVersion

		// Change results from this height but only applies to the next height.
		lastHeightParamsChanged = header.Height + 1
	}

	nextVersion := state.Version

	// NOTE: the AppHash has not been populated.
	// It will be filled on state.Save.
	return State{
		Version:                          nextVersion,
		ChainID:                          state.ChainID,
		LastBlockHeight:                  header.Height,
		LastBlockID:                      blockID,
		LastBlockTime:                    header.Time,
		NextValidators:                   nValSet,
		Validators:                       state.NextValidators.Copy(),
		LastValidators:                   state.Validators.Copy(),
		LastHeightValidatorsChanged:      lastHeightValsChanged,
		ConsensusParams:                  nextParams,
		LastHeightConsensusParamsChanged: lastHeightParamsChanged,
		LastResultsHash:                  ABCIResponsesResultsHash(abciResponses),
		AppHash:                          nil,
	}, nil
}

// Fire NewBlock, NewBlockHeader.
// Fire TxEvent for every tx.
// NOTE: if Tendermint crashes before commit, some or all of these events may be published again.
func fireEvents(
	logger log.Logger,
	eventBus types.BlockEventPublisher,
	block *types.Block,
	abciResponses *tmstate.ABCIResponses,
	validatorUpdates []*types.Validator,
) {
	eventBus.PublishEventNewBlock(types.EventDataNewBlock{
		Block:              block,
		ResultDeliverBlock: *abciResponses.DeliverBlock,
	})
	eventBus.PublishEventNewBlockHeader(types.EventDataNewBlockHeader{
		Header:             block.Header,
		NumTxs:             int64(len(block.Txs)),
		ResultDeliverBlock: *abciResponses.DeliverBlock,
	})

	for i, tx := range block.Data.Txs {
		eventBus.PublishEventTx(types.EventDataTx{TxResult: abcix.TxResult{
			Height: block.Height,
			Index:  uint32(i),
			Tx:     tx,
			Result: *(abciResponses.DeliverBlock.DeliverTxs[i]),
		}})
	}

	if len(validatorUpdates) > 0 {
		eventBus.PublishEventValidatorSetUpdates(
			types.EventDataValidatorSetUpdates{ValidatorUpdates: validatorUpdates})
	}
}

//----------------------------------------------------------------------------------------------------
// Execute block without state. TODO: eliminate

// ExecCommitBlock executes and commits a block on the proxyApp without validating or mutating the state.
// It returns the application root hash (result of abci.Commit).
func ExecCommitBlock(
	appConnConsensus proxy.AppConnConsensus,
	block *types.Block,
	logger log.Logger,
	stateDB dbm.DB,
) ([]byte, error) {
	_, err := execBlockOnProxyApp(logger, appConnConsensus, block, stateDB)
	if err != nil {
		logger.Error("Error executing block on proxy app", "height", block.Height, "err", err)
		return nil, err
	}
	// Commit block, get hash back
	res, err := appConnConsensus.CommitSync()
	if err != nil {
		logger.Error("Client error during proxyAppConn.CommitSync", "err", res)
		return nil, err
	}
	// ResponseCommit has no error or log, just data
	return res.Data, nil
}
