package mempool

import (
	"sync"
	"time"

	"github.com/pkg/errors"

	abcix "github.com/tendermint/tendermint/abcix/types"
	cfg "github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/libs/llrb"
	tmmath "github.com/tendermint/tendermint/libs/math"
	"github.com/tendermint/tendermint/proxy"
	"github.com/tendermint/tendermint/types"
)

//--------------------------------------------------------------------------------

// lElement is used to insert, remove, or recheck transactions
type lElement struct {
	nodeKey llrb.NodeKey
	tx      *mempoolTx
}

//--------------------------------------------------------------------------------

// enumllrbmempool is an ordered in-memory pool for transactions before they are
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
	recheckCursor *lElement    // next expected response
	recheckEnd    llrb.NodeKey // re-checking stops here

	waitChWrapper struct {
		ch     chan struct{}
		closed bool
		mtx    sync.RWMutex
	}
}

// NewLLRBMempool returns a new mempool with the given configuration and connection to an application.
func NewLLRBMempool(
	config *cfg.MempoolConfig,
	proxyAppConn proxy.AppConnMempool,
	height int64,
	options ...Option,
) Mempool {
	llrbMempool := &llrbMempool{txs: llrb.New()}
	llrbMempool.waitChWrapper.ch = make(chan struct{})
	return newBasemempool(llrbMempool, config, proxyAppConn, height, options...)
}

// Safe for concurrent use by multiple goroutines.
func (mem *llrbMempool) Size() int {
	return mem.txs.Size()
}

// Called from:
//  - resCbFirstTime (lock not held) if tx is valid
func (mem *llrbMempool) addTx(memTx *mempoolTx, priority uint64) {
	prevSize := mem.txs.Size()
	hash := TxKey(memTx.tx)
	nodeKey := llrb.NodeKey{Priority: priority, TS: time.Now(), Hash: hash}
	if err := mem.txs.Insert(nodeKey, memTx); err != nil {
		// TODO: better error handling here
		panic("failed to insert tx into llrb mempool: " + err.Error())
	}
	mem.txsMap.Store(hash, &lElement{nodeKey, memTx})

	// Notify subscribers now have at least one tx. Note we may have
	// race closing the channel based only on `mem.txs`, thus a bool flag
	// is used to avoid closing an already closed channel
	if prevSize == 0 {
		mem.waitChWrapper.mtx.Lock()
		if !mem.waitChWrapper.closed {
			close(mem.waitChWrapper.ch)
			mem.waitChWrapper.closed = true
		}
		mem.waitChWrapper.mtx.Unlock()
	}
}

// Called from:
//  - Update (lock held) if tx was committed
//  - resCbRecheck (lock not held) if tx was invalidated
//  - RemoveTxs (lock held) for invalid txs from CreateBlock response
func (mem *llrbMempool) removeTx(tx types.Tx) (elemRemoved bool) {
	if e, ok := mem.txsMap.Load(TxKey(tx)); ok {
		elem := e.(*lElement)
		if _, err := mem.txs.Remove(elem.nodeKey); err != nil {
			// TODO: better error handling here
			// considering this function may be called concurrently from resCbRecheck,
			// should probably just log the errors
			panic("deleting an nonexistent node from tree")
		}
		elemRemoved = true
	}
	mem.txsMap.Delete(TxKey(tx))

	// Re-init tx wait ch
	if elemRemoved && mem.txs.Size() == 0 {
		mem.waitChWrapper.mtx.Lock()
		if mem.waitChWrapper.closed {
			mem.waitChWrapper.ch = make(chan struct{})
			mem.waitChWrapper.closed = false
		}
		mem.waitChWrapper.mtx.Unlock()
	}
	return
}

func (mem *llrbMempool) updateRecheckCursor() {
	if mem.recheckCursor.nodeKey == mem.recheckEnd {
		mem.recheckCursor = nil
	} else {
		memTx, next, err := mem.txs.GetNext(&mem.recheckCursor.nodeKey, nil)
		if err != nil {
			mem.recheckCursor = nil
		} else {
			mem.recheckCursor = &lElement{nodeKey: next, tx: memTx.(*mempoolTx)}
		}
	}
}

func (mem *llrbMempool) reapMaxTxs(max int) types.Txs {
	if max < 0 {
		max = mem.txs.Size()
	}

	txs := make([]types.Tx, 0, tmmath.MinInt(mem.txs.Size(), max))

	var starter *llrb.NodeKey
	for len(txs) <= max {
		memTx, next, err := mem.txs.GetNext(starter, nil)
		if err != nil {
			break
		}
		txs = append(txs, memTx.(*mempoolTx).tx)
		starter = &next
	}
	return txs
}

// iterate txs and recheck them one by one
func (mem *llrbMempool) recheckTxs(proxyAppConn proxy.AppConnMempool) {
	if mem.Size() == 0 {
		panic("recheckTxs is called, but the mempool is empty")
	}

	mem.recheckCursor = nil
	// Push txs to proxyAppConn
	// NOTE: globalCb may be called concurrently.
	var starter *llrb.NodeKey
	for {
		result, next, err := mem.txs.GetNext(starter, nil)
		if err != nil {
			if starter == nil {
				panic("recheckTxs is called when size > 0, but iteration failed")
			}
			mem.recheckEnd = *starter
			break
		}

		mptx := result.(*mempoolTx)
		if mem.recheckCursor == nil {
			mem.recheckCursor = &lElement{nodeKey: next, tx: mptx}
		}

		proxyAppConn.CheckTxAsync(abcix.RequestCheckTx{
			Tx:   mptx.tx,
			Type: abcix.CheckTxType_Recheck,
		})

		starter = &next
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
	memTx, _, err := mem.txs.GetNext(prevNodeKey, func(i interface{}) bool {
		return i.(*mempoolTx).gasWanted <= remainGas && (int64(len(i.(*mempoolTx).tx)) <= remainBytes)
	})
	if err == llrb.ErrorStopIteration {
		return nil, nil
	} else if err != nil {
		return nil, errors.Wrap(err, "failed to get next tx from llrb")
	}
	return memTx.(*mempoolTx).tx, nil
}

// not really being used (unless in unsafe code)
func (mem *llrbMempool) deleteAll() {
	mem.txs = llrb.New()
	mem.txsMap.Range(func(key, _ interface{}) bool {
		mem.txsMap.Delete(key)
		return true
	})
}

func (mem *llrbMempool) isRecheckCursorNil() bool {
	return mem.recheckCursor == nil
}

func (mem *llrbMempool) getRecheckCursorTx() *mempoolTx {
	return mem.recheckCursor.tx
}

func (mem *llrbMempool) getMempoolTx(tx types.Tx) *mempoolTx {
	if e, ok := mem.txsMap.Load(TxKey(tx)); ok {
		return e.(*lElement).tx
	}
	return nil
}

func (mem *llrbMempool) txsWaitChan() <-chan struct{} {
	mem.waitChWrapper.mtx.RLock()
	defer mem.waitChWrapper.mtx.RUnlock()
	return mem.waitChWrapper.ch
}
