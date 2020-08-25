package consensus

import (
	abcix "github.com/tendermint/tendermint/abcix/types"
	"github.com/tendermint/tendermint/libs/clist"
	mempl "github.com/tendermint/tendermint/mempool"
	tmstate "github.com/tendermint/tendermint/proto/tendermint/state"
	"github.com/tendermint/tendermint/proxy"
	sm "github.com/tendermint/tendermint/state"
	"github.com/tendermint/tendermint/types"
)

//-----------------------------------------------------------------------------

type emptyMempool struct{}

var _ mempl.Mempool = emptyMempool{}

func (emptyMempool) Lock()     {}
func (emptyMempool) Unlock()   {}
func (emptyMempool) Size() int { return 0 }
func (emptyMempool) CheckTx(_ types.Tx, _ func(*abcix.Response), _ mempl.TxInfo) error {
	return nil
}
func (emptyMempool) ReapMaxBytesMaxGas(_, _ int64) types.Txs { return types.Txs{} }
func (emptyMempool) ReapMaxTxs(n int) types.Txs              { return types.Txs{} }
func (emptyMempool) GetNextTxBytes(_ int64, _ int64, _ []byte) ([]byte, error) {
	return types.Tx{}, nil
}
func (emptyMempool) Update(
	_ int64,
	_ types.Txs,
	_ []*abcix.ResponseDeliverTx,
	_ mempl.PreCheckFunc,
	_ mempl.PostCheckFunc,
) error {
	return nil
}
func (emptyMempool) Flush()                        {}
func (emptyMempool) FlushAppConn() error           { return nil }
func (emptyMempool) TxsAvailable() <-chan struct{} { return make(chan struct{}) }
func (emptyMempool) EnableTxsAvailable()           {}
func (emptyMempool) TxsBytes() int64               { return 0 }

func (emptyMempool) TxsFront() *clist.CElement    { return nil }
func (emptyMempool) TxsWaitChan() <-chan struct{} { return nil }

func (emptyMempool) InitWAL() error              { return nil }
func (emptyMempool) CloseWAL()                   {}
func (emptyMempool) RemoveTxs(_ types.Txs) error { return nil }

//-----------------------------------------------------------------------------

type emptyEvidencePool struct{}

var _ sm.EvidencePool = emptyEvidencePool{}

func (emptyEvidencePool) PendingEvidence(uint32) []types.Evidence { return nil }
func (emptyEvidencePool) AddEvidence(types.Evidence) error        { return nil }
func (emptyEvidencePool) Update(*types.Block, sm.State)           {}
func (emptyEvidencePool) IsCommitted(types.Evidence) bool         { return false }
func (emptyEvidencePool) IsPending(types.Evidence) bool           { return false }
func (emptyEvidencePool) AddPOLC(*types.ProofOfLockChange) error  { return nil }
func (emptyEvidencePool) Header(int64) *types.Header              { return nil }

//-----------------------------------------------------------------------------
// mockProxyApp uses ABCIResponses to give the right results.
//
// Useful because we don't want to call Commit() twice for the same block on
// the real app.

func newMockProxyApp(appHash []byte, abciResponses *tmstate.ABCIResponses) proxy.AppConnConsensus {
	clientCreator := proxy.NewLocalClientCreator(&mockProxyApp{
		appHash:       appHash,
		abciResponses: abciResponses,
	})
	cli, _ := clientCreator.NewABCIClient()
	err := cli.Start()
	if err != nil {
		panic(err)
	}
	return proxy.NewAppConnConsensus(cli)
}

type mockProxyApp struct {
	abcix.BaseApplication

	appHash       []byte
	abciResponses *tmstate.ABCIResponses
}

func (mock *mockProxyApp) DeliverBlock(req abcix.RequestDeliverBlock) abcix.ResponseDeliverBlock {
	ret := abcix.ResponseDeliverBlock{}
	// DeliverTx
	for i := range req.Txs {
		r := mock.abciResponses.DeliverBlock.DeliverTxs[i]
		if r == nil {
			r = &abcix.ResponseDeliverTx{}
		}
		ret.DeliverTxs = append(ret.DeliverTxs, r)
	}
	return ret
}

func (mock *mockProxyApp) Commit() abcix.ResponseCommit {
	return abcix.ResponseCommit{Data: mock.appHash}
}
