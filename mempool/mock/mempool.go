package mock

import (
	abcix "github.com/tendermint/tendermint/abcix/types"
	"github.com/tendermint/tendermint/libs/clist"
	"github.com/tendermint/tendermint/libs/log"
	mempl "github.com/tendermint/tendermint/mempool"
	"github.com/tendermint/tendermint/types"
)

// Mempool is an empty implementation of a Mempool, useful for testing.
type Mempool struct{}

var _ mempl.Mempool = Mempool{}

func (Mempool) Lock()     {}
func (Mempool) Unlock()   {}
func (Mempool) Size() int { return 0 }
func (Mempool) CheckTx(_ types.Tx, _ func(*abcix.Response), _ mempl.TxInfo) error {
	return nil
}
func (Mempool) ReapMaxTxs(n int) types.Txs { return types.Txs{} }
func (Mempool) GetNextTxBytes(_ int64, _ int64, _ []byte) ([]byte, error) {
	return types.Tx{}, nil
}
func (Mempool) Update(
	_ int64,
	_ types.Txs,
	_ []*abcix.ResponseDeliverTx,
	_ mempl.PreCheckFunc,
	_ mempl.PostCheckFunc,
) error {
	return nil
}
func (Mempool) Flush()                        {}
func (Mempool) FlushAppConn() error           { return nil }
func (Mempool) TxsAvailable() <-chan struct{} { return make(chan struct{}) }
func (Mempool) EnableTxsAvailable()           {}
func (Mempool) TxsBytes() int64               { return 0 }

func (Mempool) TxsFront() *clist.CElement    { return nil }
func (Mempool) TxsWaitChan() <-chan struct{} { return nil }

func (Mempool) InitWAL() error { return nil }
func (Mempool) CloseWAL()      {}

func (m Mempool) SetLogger(_ log.Logger)    {}
func (Mempool) RemoveTxs(_ types.Txs) error { return nil }
