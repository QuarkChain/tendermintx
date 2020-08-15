package consensus

import (
	"encoding/binary"
	"fmt"
	"os"
	"testing"
	"time"

	abcix "github.com/tendermint/tendermint/abcix/types"

	"github.com/stretchr/testify/assert"

	dbm "github.com/tendermint/tm-db"

	"github.com/tendermint/tendermint/abci/example/code"
	mempl "github.com/tendermint/tendermint/mempool"
	sm "github.com/tendermint/tendermint/state"
	"github.com/tendermint/tendermint/types"
)

// for testing
func assertMempool(txn txNotifier) mempl.Mempool {
	return txn.(mempl.Mempool)
}

func TestMempoolNoProgressUntilTxsAvailable(t *testing.T) {
	config := ResetConfig("consensus_mempool_txs_available_test")
	defer os.RemoveAll(config.RootDir)
	config.Consensus.CreateEmptyBlocks = false
	state, privVals := randGenesisState(1, false, 10)
	cs := newStateWithConfig(config, state, privVals[0], NewCounterApplication())
	assertMempool(cs.txNotifier).EnableTxsAvailable()
	height, round := cs.Height, cs.Round
	newBlockCh := subscribe(cs.eventBus, types.EventQueryNewBlock)
	startTestRound(cs, height, round)

	ensureNewEventOnChannel(newBlockCh) // first block gets committed
	ensureNoNewEventOnChannel(newBlockCh)
	deliverTxsRange(cs, 0, 1)
	ensureNewEventOnChannel(newBlockCh) // commit txs
	ensureNewEventOnChannel(newBlockCh) // commit updated app hash
	ensureNoNewEventOnChannel(newBlockCh)
}

func TestMempoolProgressAfterCreateEmptyBlocksInterval(t *testing.T) {
	config := ResetConfig("consensus_mempool_txs_available_test")
	defer os.RemoveAll(config.RootDir)

	config.Consensus.CreateEmptyBlocksInterval = ensureTimeout
	state, privVals := randGenesisState(1, false, 10)
	cs := newStateWithConfig(config, state, privVals[0], NewCounterApplication())

	assertMempool(cs.txNotifier).EnableTxsAvailable()

	newBlockCh := subscribe(cs.eventBus, types.EventQueryNewBlock)
	startTestRound(cs, cs.Height, cs.Round)

	ensureNewEventOnChannel(newBlockCh)   // first block gets committed
	ensureNoNewEventOnChannel(newBlockCh) // then we dont make a block ...
	ensureNewEventOnChannel(newBlockCh)   // until the CreateEmptyBlocksInterval has passed
}

func TestMempoolProgressInHigherRound(t *testing.T) {
	config := ResetConfig("consensus_mempool_txs_available_test")
	defer os.RemoveAll(config.RootDir)
	config.Consensus.CreateEmptyBlocks = false
	state, privVals := randGenesisState(1, false, 10)
	cs := newStateWithConfig(config, state, privVals[0], NewCounterApplication())
	assertMempool(cs.txNotifier).EnableTxsAvailable()
	height, round := cs.Height, cs.Round
	newBlockCh := subscribe(cs.eventBus, types.EventQueryNewBlock)
	newRoundCh := subscribe(cs.eventBus, types.EventQueryNewRound)
	timeoutCh := subscribe(cs.eventBus, types.EventQueryTimeoutPropose)
	cs.setProposal = func(proposal *types.Proposal) error {
		if cs.Height == 2 && cs.Round == 0 {
			// dont set the proposal in round 0 so we timeout and
			// go to next round
			cs.Logger.Info("Ignoring set proposal at height 2, round 0")
			return nil
		}
		return cs.defaultSetProposal(proposal)
	}
	startTestRound(cs, height, round)

	ensureNewRound(newRoundCh, height, round) // first round at first height
	ensureNewEventOnChannel(newBlockCh)       // first block gets committed

	height++ // moving to the next height
	round = 0

	ensureNewRound(newRoundCh, height, round) // first round at next height
	deliverTxsRange(cs, 0, 1)                 // we deliver txs, but dont set a proposal so we get the next round
	ensureNewTimeout(timeoutCh, height, round, cs.config.TimeoutPropose.Nanoseconds())

	round++                                   // moving to the next round
	ensureNewRound(newRoundCh, height, round) // wait for the next round
	ensureNewEventOnChannel(newBlockCh)       // now we can commit the block
}

func deliverTxsRange(cs *State, start, end int) {
	// Deliver some txs.
	for i := start; i < end; i++ {
		txBytes := make([]byte, 8)
		binary.BigEndian.PutUint64(txBytes, uint64(i))
		err := assertMempool(cs.txNotifier).CheckTx(txBytes, nil, mempl.TxInfo{})
		if err != nil {
			panic(fmt.Sprintf("Error after CheckTx: %v", err))
		}
	}
}

func TestMempoolTxConcurrentWithCommit(t *testing.T) {
	state, privVals := randGenesisState(1, false, 10)
	blockDB := dbm.NewMemDB()
	cs := newStateWithConfigAndBlockStore(config, state, privVals[0], NewCounterApplication(), blockDB)
	sm.SaveState(blockDB, state)
	newBlockHeaderCh := subscribe(cs.eventBus, types.EventQueryNewBlockHeader)

	const numTxs int64 = 3000
	go deliverTxsRange(cs, 0, int(numTxs))

	startTestRound(cs, cs.Height, cs.Round)
	for n := int64(0); n < numTxs; {
		select {
		case msg := <-newBlockHeaderCh:
			headerEvent := msg.Data().(types.EventDataNewBlockHeader)
			n += headerEvent.NumTxs
		case <-time.After(30 * time.Second):
			t.Fatal("Timed out waiting 30s to commit blocks with transactions")
		}
	}
}

func TestMempoolRmBadTx(t *testing.T) {
	state, privVals := randGenesisState(1, false, 10)
	app := NewCounterApplication()
	blockDB := dbm.NewMemDB()
	cs := newStateWithConfigAndBlockStore(config, state, privVals[0], app, blockDB)
	sm.SaveState(blockDB, state)

	// increment the counter by 1
	txBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(txBytes, uint64(0))

	resDeliver := app.DeliverBlock(abcix.RequestDeliverBlock{
		Txs: [][]byte{txBytes},
	})
	assert.False(t, resDeliver.DeliverTxs[0].IsErr(), fmt.Sprintf("expected no error. got %v", resDeliver))

	resCommit := app.Commit()
	assert.True(t, len(resCommit.Data) > 0)

	emptyMempoolCh := make(chan struct{})
	checkTxRespCh := make(chan struct{})
	go func() {
		// Try to send the tx through the mempool.
		// CheckTx should not err, but the app should return a bad abci code
		// and the tx should get removed from the pool
		err := assertMempool(cs.txNotifier).CheckTx(txBytes, func(r *abcix.Response) {
			if r.GetCheckTx().Code != code.CodeTypeBadNonce {
				t.Errorf("expected checktx to return bad nonce, got %v", r)
				return
			}
			checkTxRespCh <- struct{}{}
		}, mempl.TxInfo{})
		if err != nil {
			t.Errorf("error after CheckTx: %v", err)
			return
		}

		// check for the tx
		for {
			txs := assertMempool(cs.txNotifier).ReapMaxBytesMaxGas(int64(len(txBytes)), -1)
			if len(txs) == 0 {
				emptyMempoolCh <- struct{}{}
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()

	// Wait until the tx returns
	ticker := time.After(time.Second * 5)
	select {
	case <-checkTxRespCh:
		// success
	case <-ticker:
		t.Errorf("timed out waiting for tx to return")
		return
	}

	// Wait until the tx is removed
	ticker = time.After(time.Second * 5)
	select {
	case <-emptyMempoolCh:
		// success
	case <-ticker:
		t.Errorf("timed out waiting for tx to be removed")
		return
	}
}

// CounterApplication that maintains a mempool state and resets it upon commit
type CounterApplication struct {
	abcix.BaseApplication

	txCount        int
	mempoolTxCount int
}

func NewCounterApplication() *CounterApplication {
	return &CounterApplication{}
}

func (app *CounterApplication) Info(req abcix.RequestInfo) abcix.ResponseInfo {
	return abcix.ResponseInfo{Data: fmt.Sprintf("txs:%v", app.txCount)}
}

func (app *CounterApplication) CreateBlock(
	req abcix.RequestCreateBlock,
	iter *abcix.MempoolIter,
) abcix.ResponseCreateBlock {
	ret := abcix.ResponseCreateBlock{}

	remainBytes := types.DefaultConsensusParams().Block.MaxBytes -
		types.MaxOverheadForBlock -
		types.MaxHeaderBytes -
		int64(len(req.LastCommitInfo.Votes))*types.MaxVoteBytes -
		int64(len(req.ByzantineValidators))*types.MaxEvidenceBytes
	remainGas := int64(1<<(64-1) - 1)
	count := app.txCount

	for iter.HasNext() {
		tx, err := iter.GetNextTransaction(remainBytes, remainGas)
		if err != nil {
			panic(err)
		}
		if len(tx) == 0 {
			break
		}
		txValue := txAsUint64(tx)
		if txValue != uint64(count) {
			ret.InvalidTxs = append(ret.InvalidTxs, tx)
			continue
		}
		ret.Txs = append(ret.Txs, tx)
		count++
		remainBytes -= int64(len(tx))
		remainGas--
	}

	hash := make([]byte, 8)
	binary.BigEndian.PutUint64(hash, uint64(count))
	ret.Hash = hash
	return ret
}

func (app *CounterApplication) DeliverBlock(req abcix.RequestDeliverBlock) abcix.ResponseDeliverBlock {
	ret := abcix.ResponseDeliverBlock{}
	for _, tx := range req.Txs {
		txValue := txAsUint64(tx)
		var txResp abcix.ResponseDeliverTx
		if txValue != uint64(app.txCount) {
			txResp = abcix.ResponseDeliverTx{
				Code: code.CodeTypeBadNonce,
				Log:  fmt.Sprintf("Invalid nonce. Expected %v, got %v", app.txCount, txValue),
			}
		} else {
			txResp = abcix.ResponseDeliverTx{Code: code.CodeTypeOK}
		}
		ret.DeliverTxs = append(ret.DeliverTxs, &txResp)
		app.txCount++
	}
	return ret
}

func (app *CounterApplication) CheckTx(req abcix.RequestCheckTx) abcix.ResponseCheckTx {
	txValue := txAsUint64(req.Tx)
	if txValue != uint64(app.mempoolTxCount) {
		return abcix.ResponseCheckTx{
			Code: code.CodeTypeBadNonce,
			Log:  fmt.Sprintf("Invalid nonce. Expected %v, got %v", app.mempoolTxCount, txValue)}
	}
	app.mempoolTxCount++
	return abcix.ResponseCheckTx{Code: code.CodeTypeOK}
}

func txAsUint64(tx []byte) uint64 {
	tx8 := make([]byte, 8)
	copy(tx8[len(tx8)-len(tx):], tx)
	return binary.BigEndian.Uint64(tx8)
}

func (app *CounterApplication) Commit() abcix.ResponseCommit {
	app.mempoolTxCount = app.txCount
	if app.txCount == 0 {
		return abcix.ResponseCommit{}
	}
	hash := make([]byte, 8)
	binary.BigEndian.PutUint64(hash, uint64(app.txCount))
	return abcix.ResponseCommit{Data: hash}
}
