package mempool

import (
	"sync"
	"time"

	tmmath "github.com/tendermint/tendermint/libs/math"
	"github.com/tendermint/tendermint/types"

	abcix "github.com/tendermint/tendermint/abcix/types"
	cfg "github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/libs/llrb"
	"github.com/tendermint/tendermint/proxy"
)

//--------------------------------------------------------------------------------

// lElement is used to insert, remove, or recheck transactions
type lElement struct {
	nodeKey llrb.NodeKey
	tx      *mempoolTx
}

//--------------------------------------------------------------------------------

// llrbmempool is an ordered in-memory pool for transactions before they are
// proposed in a consensus round. Transaction validity is checked using the
// CheckTx abci message before the transaction is added to the pool. The
// mempool uses a left-leaning red-black tree structure for storing transactions that can
// be efficiently accessed by multiple concurrent readers.
type llrbMempool struct {
	txs llrb.LLRB // left-leaning red-black tree of good txs

	// Map for quick access to txs to record sender in CheckTx.
	// txsMap: txKey -> lElement
	txsMap sync.Map

	// Track whether we're rechecking txs.
	// These are not protected by a mutex and are expected to be mutated in
	// serial (ie. by abci responses which are called in serial).
	recheckCursor *lElement // next expected response
	recheckEnd    *lElement // re-checking stops here
}

// NewLLRBMempool returns a new mempool with the given configuration and connection to an application.
func NewLLRBMempool(
	config *cfg.MempoolConfig,
	proxyAppConn proxy.AppConnMempool,
	height int64,
	options ...Option,
) Mempool {
	llrbMempool := &llrbMempool{txs: llrb.New()}
	ret := newBasemempool(llrbMempool, config, proxyAppConn, height, options...)

	return ret
}

// Safe for concurrent use by multiple goroutines.
func (mem *llrbMempool) Size() int {
	return mem.txs.Size()
}

// Called from:
//  - resCbFirstTime (lock not held) if tx is valid
func (mem *llrbMempool) addTx(memTx *mempoolTx, priority uint64) {
	timeStamp := time.Now()
	e := &lElement{nodeKey: llrb.NodeKey{Priority: priority, TS: timeStamp}}
	if err := mem.txs.Insert(e.nodeKey, &memTx); err != nil {
		panic("failed to insert tx into llrb mempool")
	}
	mem.txsMap.Store(TxKey(memTx.tx), &lElement{e.nodeKey, memTx})
}

// Called from:
//  - Update (lock held) if tx was committed
// 	- resCbRecheck (lock not held) if tx was invalidated
//  - RemoveTxs (lock held) for invalid txs from CreateBlock response
func (mem *llrbMempool) removeTx(tx types.Tx) (elemRemoved bool) {
	if e, ok := mem.txsMap.Load(TxKey(tx)); ok {
		elem := e.(*lElement)
		if _, err := mem.txs.Remove(elem.nodeKey); err != nil {
			panic("deleting an nonexistent node from tree")
		}
		elemRemoved = true
	}
	mem.txsMap.Delete(TxKey(tx))
	return
}

func (mem *llrbMempool) updateRecheckCursor() {
	if mem.recheckCursor == mem.recheckEnd {
		mem.recheckCursor = nil
	} else {
		memTx, err := mem.txs.GetNext(&(mem.recheckCursor.nodeKey), nil)
		if err != nil {
			mem.recheckCursor = nil
		} else {
			tx := (*memTx.(**mempoolTx)).tx
			if e, ok := mem.txsMap.Load(TxKey(tx)); ok {
				mem.recheckCursor = e.(*lElement)
			}
		}
	}
}

func (mem *llrbMempool) reapMaxTxs(max int) types.Txs {
	if max < 0 {
		max = mem.txs.Size()
	}

	txs := make([]types.Tx, 0, tmmath.MinInt(mem.txs.Size(), max))
	for len(txs) <= max {
		memTx, err := mem.txs.GetNext(nil, nil)
		if err != nil {
			break
		}
		tx := (*memTx.(**mempoolTx)).tx
		txs = append(txs, tx)
	}
	return txs
}

func (mem *llrbMempool) recheckTxs(proxyAppConn proxy.AppConnMempool) {
	var tempE lElement
	if mem.Size() == 0 {
		panic("recheckTxs is called, but the mempool is empty")
	}

	memTx, err := mem.txs.GetNext(nil, nil)
	if err != nil {
		mem.recheckCursor = nil
		mem.recheckEnd = nil
	} else {
		tx := (*memTx.(**mempoolTx)).tx
		if e, ok := mem.txsMap.Load(TxKey(tx)); ok {
			mem.recheckCursor = e.(*lElement)
		}
	}

	// Push txs to proxyAppConn
	// NOTE: globalCb may be called concurrently.
	for {
		memTx, err := mem.txs.GetNext(&(tempE.nodeKey), nil)
		if err != nil {
			mem.recheckEnd = &tempE
			break
		}
		tx := (*memTx.(**mempoolTx)).tx
		proxyAppConn.CheckTxAsync(abcix.RequestCheckTx{
			Tx:   tx,
			Type: abcix.CheckTxType_Recheck,
		})
		if e, ok := mem.txsMap.Load(TxKey(tx)); ok {
			tempE = (*e.(*lElement))
		}
	}

	proxyAppConn.FlushAsync()
}

func (mem *llrbMempool) getNextTxBytes(remainBytes int64, remainGas int64, starter []byte) ([]byte, error) {
	var prevNodeKey *llrb.NodeKey
	if len(starter) > 0 {
		if e, ok := mem.txsMap.Load(TxKey(starter)); ok {
			prevNodeKey = &e.(*lElement).nodeKey
		}
	}
	memTx, err := mem.txs.GetNext(prevNodeKey, func(i interface{}) bool {
		return ((*i.(**mempoolTx)).gasWanted <= remainGas) && (int64(len((*i.(**mempoolTx)).tx)) <= remainBytes)
	})
	if err != nil {
		return nil, nil
	}
	tx := (*memTx.(**mempoolTx)).tx
	return tx, nil
}

func (mem *llrbMempool) deleteAll() {
	mem.txs = llrb.New()
	mem.txsMap.Range(func(key, _ interface{}) bool {
		mem.txsMap.Delete(key)
		return true
	})
}

func (mem *llrbMempool) recordNewSender(tx types.Tx, txInfo TxInfo) {
	if e, ok := mem.txsMap.Load(TxKey(tx)); ok {
		memTx := e.(*lElement).tx
		memTx.senders.LoadOrStore(txInfo.SenderID, true)
		// TODO: consider punishing peer for dups,
		// its non-trivial since invalid txs can become valid,
		// but they can spam the same tx with little cost to them atm.
	}
}

func (mem *llrbMempool) isRecheckCursorNil() bool {
	return mem.recheckCursor == nil
}

func (mem *llrbMempool) getRecheckCursorTx() *mempoolTx {
	return mem.recheckCursor.tx
}
