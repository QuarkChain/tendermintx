package state_test

import (
	"bytes"
	"fmt"
	"math"
	"time"

	"github.com/tendermint/tendermint/abcix/adapter"

	dbm "github.com/tendermint/tm-db"

	abcix "github.com/tendermint/tendermint/abcix/types"
	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/crypto/ed25519"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	tmstate "github.com/tendermint/tendermint/proto/tendermint/state"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	"github.com/tendermint/tendermint/proxy"
	sm "github.com/tendermint/tendermint/state"
	"github.com/tendermint/tendermint/types"
	tmtime "github.com/tendermint/tendermint/types/time"
)

type paramsChangeTestCase struct {
	height int64
	params tmproto.ConsensusParams
}

func newTestApp() proxy.AppConns {
	app := &testApp{}
	cc := proxy.NewLocalClientCreator(app)
	return proxy.NewAppConns(cc)
}

func makeAndCommitGoodBlock(
	state sm.State,
	height int64,
	lastCommit *types.Commit,
	proposerAddr []byte,
	blockExec *sm.BlockExecutor,
	privVals map[string]types.PrivValidator,
	evidence []types.Evidence) (sm.State, types.BlockID, *types.Commit, error) {
	// A good block passes
	state, blockID, err := makeAndApplyGoodBlock(state, height, lastCommit, proposerAddr, blockExec, evidence)
	if err != nil {
		return state, types.BlockID{}, nil, err
	}

	// Simulate a lastCommit for this block from all validators for the next height
	commit, err := makeValidCommit(height, blockID, state.Validators, privVals)
	if err != nil {
		return state, types.BlockID{}, nil, err
	}
	return state, blockID, commit, nil
}

func makeAndApplyGoodBlock(state sm.State, height int64, lastCommit *types.Commit, proposerAddr []byte,
	blockExec *sm.BlockExecutor, evidence []types.Evidence) (sm.State, types.BlockID, error) {
	block, _ := state.MakeBlock(height, makeTxs(height), lastCommit, evidence, proposerAddr, nil, nil)
	if err := blockExec.ValidateBlock(state, block); err != nil {
		return state, types.BlockID{}, err
	}
	blockID := types.BlockID{Hash: block.Hash(),
		PartSetHeader: types.PartSetHeader{Total: 3, Hash: tmrand.Bytes(32)}}
	state, _, _, err := blockExec.ApplyBlock(state, blockID, block)
	if err != nil {
		return state, types.BlockID{}, err
	}
	return state, blockID, nil
}

func makeValidCommit(
	height int64,
	blockID types.BlockID,
	vals *types.ValidatorSet,
	privVals map[string]types.PrivValidator,
) (*types.Commit, error) {
	sigs := make([]types.CommitSig, 0)
	for i := 0; i < vals.Size(); i++ {
		_, val := vals.GetByIndex(int32(i))
		vote, err := types.MakeVote(height, blockID, vals, privVals[val.Address.String()], chainID, time.Now())
		if err != nil {
			return nil, err
		}
		sigs = append(sigs, vote.CommitSig())
	}
	return types.NewCommit(height, 0, blockID, sigs), nil
}

// make some bogus txs
func makeTxs(height int64) (txs []types.Tx) {
	for i := 0; i < nTxsPerBlock; i++ {
		txs = append(txs, types.Tx([]byte{byte(height), byte(i)}))
	}
	return txs
}

func makeState(nVals, height int) (sm.State, dbm.DB, map[string]types.PrivValidator) {
	vals := make([]types.GenesisValidator, nVals)
	privVals := make(map[string]types.PrivValidator, nVals)
	for i := 0; i < nVals; i++ {
		secret := []byte(fmt.Sprintf("test%d", i))
		pk := ed25519.GenPrivKeyFromSecret(secret)
		valAddr := pk.PubKey().Address()
		vals[i] = types.GenesisValidator{
			Address: valAddr,
			PubKey:  pk.PubKey(),
			Power:   1000,
			Name:    fmt.Sprintf("test%d", i),
		}
		privVals[valAddr.String()] = types.NewMockPVWithParams(pk, false, false)
	}
	s, _ := sm.MakeGenesisState(&types.GenesisDoc{
		ChainID:    chainID,
		Validators: vals,
		AppHash:    nil,
	})

	stateDB := dbm.NewMemDB()
	sm.SaveState(stateDB, s)

	for i := 1; i < height; i++ {
		s.LastBlockHeight++
		s.LastValidators = s.Validators.Copy()
		sm.SaveState(stateDB, s)
	}

	return s, stateDB, privVals
}

func makeBlock(state sm.State, height int64) *types.Block {
	block, _ := state.MakeBlock(
		height,
		makeTxs(state.LastBlockHeight),
		new(types.Commit),
		nil,
		state.Validators.GetProposer().Address,
		nil,
		nil,
	)
	return block
}

func genValSet(size int) *types.ValidatorSet {
	vals := make([]*types.Validator, size)
	for i := 0; i < size; i++ {
		vals[i] = types.NewValidator(ed25519.GenPrivKey().PubKey(), 10)
	}
	return types.NewValidatorSet(vals)
}

func makeHeaderPartsResponsesValPubKeyChange(
	state sm.State,
	pubkey crypto.PubKey,
) (types.Header, types.BlockID, *tmstate.ABCIResponses) {

	block := makeBlock(state, state.LastBlockHeight+1)
	abciResponses := &tmstate.ABCIResponses{
		DeliverBlock: &abcix.ResponseDeliverBlock{ValidatorUpdates: nil},
	}
	// If the pubkey is new, remove the old and add the new.
	_, val := state.NextValidators.GetByIndex(0)
	if !bytes.Equal(pubkey.Bytes(), val.PubKey.Bytes()) {
		abciResponses.DeliverBlock = &abcix.ResponseDeliverBlock{
			ValidatorUpdates: []abcix.ValidatorUpdate{
				types.TM2PB.NewValidatorUpdate(val.PubKey, 0),
				types.TM2PB.NewValidatorUpdate(pubkey, 10),
			},
		}
	}

	return block.Header, types.BlockID{Hash: block.Hash(), PartSetHeader: types.PartSetHeader{}}, abciResponses
}

func makeHeaderPartsResponsesValPowerChange(
	state sm.State,
	power int64,
) (types.Header, types.BlockID, *tmstate.ABCIResponses) {

	block := makeBlock(state, state.LastBlockHeight+1)
	abciResponses := &tmstate.ABCIResponses{
		DeliverBlock: &abcix.ResponseDeliverBlock{ValidatorUpdates: nil},
	}

	// If the pubkey is new, remove the old and add the new.
	_, val := state.NextValidators.GetByIndex(0)
	if val.VotingPower != power {
		abciResponses.DeliverBlock = &abcix.ResponseDeliverBlock{
			ValidatorUpdates: []abcix.ValidatorUpdate{
				types.TM2PB.NewValidatorUpdate(val.PubKey, power),
			},
		}
	}

	return block.Header, types.BlockID{Hash: block.Hash(), PartSetHeader: types.PartSetHeader{}}, abciResponses
}

func makeHeaderPartsResponsesParams(
	state sm.State,
	params tmproto.ConsensusParams,
) (types.Header, types.BlockID, *tmstate.ABCIResponses) {

	block := makeBlock(state, state.LastBlockHeight+1)
	abciResponses := &tmstate.ABCIResponses{
		DeliverBlock: &abcix.ResponseDeliverBlock{ConsensusParamUpdates: types.TM2PB.ConsensusParams(&params)},
	}
	return block.Header, types.BlockID{Hash: block.Hash(), PartSetHeader: types.PartSetHeader{}}, abciResponses
}

func randomGenesisDoc() *types.GenesisDoc {
	pubkey := ed25519.GenPrivKey().PubKey()
	return &types.GenesisDoc{
		GenesisTime: tmtime.Now(),
		ChainID:     "abc",
		Validators: []types.GenesisValidator{
			{
				Address: pubkey.Address(),
				PubKey:  pubkey,
				Power:   10,
				Name:    "myval",
			},
		},
		ConsensusParams: types.DefaultConsensusParams(),
	}
}

//----------------------------------------------------------------------------

type testApp struct {
	abcix.BaseApplication

	CommitVotes         []abcix.VoteInfo
	ByzantineValidators []abcix.Evidence
	ValidatorUpdates    []abcix.ValidatorUpdate
}

var _ abcix.Application = (*testApp)(nil)

func (app *testApp) Info(req abcix.RequestInfo) (resInfo abcix.ResponseInfo) {
	return abcix.ResponseInfo{}
}

func (app *testApp) CreateBlock(req abcix.RequestCreateBlock, iter *abcix.MempoolIter) abcix.ResponseCreateBlock {
	ret := abcix.ResponseCreateBlock{}

	remainBytes := adapter.CalcRemainBytes(req)
	remainGas := int64(math.MaxInt64)

	for {
		tx, err := iter.GetNextTransaction(remainBytes, remainGas)
		if err != nil {
			panic(err)
		}
		if len(tx) == 0 {
			break
		}
		ret.Txs = append(ret.Txs, tx)
		remainBytes -= int64(len(tx))
		remainGas--
		ret.DeliverTxs = append(ret.DeliverTxs, &abcix.ResponseDeliverTx{})
	}
	return ret
}

func (app *testApp) DeliverBlock(req abcix.RequestDeliverBlock) abcix.ResponseDeliverBlock {
	app.CommitVotes = req.LastCommitInfo.Votes
	app.ByzantineValidators = req.ByzantineValidators
	deliverTxResp := make([]*abcix.ResponseDeliverTx, 0, len(req.Txs))
	for range req.Txs {
		deliverTxResp = append(deliverTxResp, &abcix.ResponseDeliverTx{})
	}
	return abcix.ResponseDeliverBlock{
		ValidatorUpdates: app.ValidatorUpdates,
		ConsensusParamUpdates: &abcix.ConsensusParams{
			Version: &tmproto.VersionParams{
				AppVersion: 1,
			},
		},
		DeliverTxs: deliverTxResp,
	}
}

func (app *testApp) CheckTx(req abcix.RequestCheckTx) abcix.ResponseCheckTx {
	return abcix.ResponseCheckTx{}
}

func (app *testApp) Commit() abcix.ResponseCommit {
	return abcix.ResponseCommit{RetainHeight: 1}
}

func (app *testApp) CheckBlock(req abcix.RequestCheckBlock) abcix.ResponseCheckBlock {
	// mock Response with error code 1
	if req.Height == 1 {
		return abcix.ResponseCheckBlock{
			Code: 1,
		}
	}
	// mock Response with invalid tx
	if req.Height == 2 {
		return abcix.ResponseCheckBlock{
			DeliverTxs: []*abcix.ResponseDeliverTx{
				{
					Code: 1,
				},
			},
		}
	}
	// mock Response with fixed ResultHash
	if req.Height == 3 {
		return abcix.ResponseCheckBlock{
			DeliverTxs: []*abcix.ResponseDeliverTx{
				{
					Code: 0,
				},
			},
		}
	}
	// mock Response with random AppHash
	if req.Height == 4 {
		return abcix.ResponseCheckBlock{
			AppHash: tmrand.Bytes(20),
		}
	}
	return abcix.ResponseCheckBlock{}
}

func (app *testApp) Query(reqQuery abcix.RequestQuery) (resQuery abcix.ResponseQuery) {
	return
}
