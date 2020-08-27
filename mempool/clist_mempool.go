package mempool

import (
	"bytes"
	"crypto/sha256"
	"math"
	"sync"

	abcix "github.com/tendermint/tendermint/abcix/types"
	cfg "github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/libs/clist"
	tmmath "github.com/tendermint/tendermint/libs/math"
	"github.com/tendermint/tendermint/proxy"
	"github.com/tendermint/tendermint/types"
)

// TxKeySize is the size of the transaction key index
const TxKeySize = sha256.Size

//--------------------------------------------------------------------------------

// cListMempool is an ordered in-memory pool for transactions before they are
// proposed in a consensus round. Transaction validity is checked using the
// CheckTx abci message before the transaction is added to the pool. The
// mempool uses a concurrent list structure for storing transactions that can
// be efficiently accessed by multiple concurrent readers.
type cListMempool struct {
	txs *clist.CList // concurrent linked-list of good txs

	// Map for quick access to txs to record sender in CheckTx.
	// txsMap: txKey -> CElement
	txsMap sync.Map

	// Track whether we're rechecking txs.
	// These are not protected by a mutex and are expected to be mutated in
	// serial (ie. by abci responses which are called in serial).
	recheckCursor *clist.CElement // next expected response
	recheckEnd    *clist.CElement // re-checking stops here
	server        *mempoolServer
}

// NewCListMempool returns a new mempool with the given configuration and connection to an application.
func NewCListMempool(
	config *cfg.MempoolConfig,
	proxyAppConn proxy.AppConnMempool,
	height int64,
	options ...Option,
) Mempool {

	clistMempool := &cListMempool{txs: clist.New()}
	ret := newBasemempool(clistMempool, config, proxyAppConn, height, options...)

	// TODO: mempool server should be bound to balance tree-based mempool. use clist here for now
	if config.ServerHostPort != "" {
		server, err := newMempoolServer(config.ServerHostPort)
		if err != nil {
			panic(err)
		}
		clistMempool.server = server
	}

	return ret
}

// TxsFront returns the first transaction in the ordered list for peer
// goroutines to call .NextWait() on.
// FIXME: leaking implementation details!
//
// Safe for concurrent use by multiple goroutines.
func (mem *cListMempool) TxsFront() *clist.CElement {
	return mem.txs.Front()
}

// TxsWaitChan returns a channel to wait on transactions. It will be closed
// once the mempool is not empty (ie. the internal `mem.txs` has at least one
// element)
//
// Safe for concurrent use by multiple goroutines.
func (mem *cListMempool) TxsWaitChan() <-chan struct{} {
	return mem.txs.WaitChan()
}

// Safe for concurrent use by multiple goroutines.
func (mem *cListMempool) Size() int {
	return mem.txs.Len()
}

// Called from:
//  - resCbFirstTime (lock not held) if tx is valid
func (mem *cListMempool) addTx(memTx *mempoolTx, priority uint64) {
	e := mem.txs.PushBackWithPriority(memTx, priority)
	mem.txsMap.Store(TxKey(memTx.tx), e)
}

// Called from:
//  - Update (lock held) if tx was committed
// 	- resCbRecheck (lock not held) if tx was invalidated
//  - RemoveTxs (lock held) for invalid txs from CreateBlock response
func (mem *cListMempool) removeTx(tx types.Tx) (elemRemoved bool) {
	if e, ok := mem.txsMap.Load(TxKey(tx)); ok {
		elem := e.(*clist.CElement)
		mem.txs.Remove(elem)
		elem.DetachPrev()
		elemRemoved = true
	}
	mem.txsMap.Delete(TxKey(tx))
	return
}

func (mem *cListMempool) updateRecheckCursor() {
	if mem.recheckCursor == mem.recheckEnd {
		mem.recheckCursor = nil
	} else {
		mem.recheckCursor = mem.recheckCursor.Next()
	}
}

func (mem *cListMempool) reapMaxTxs(max int) types.Txs {
	if max < 0 {
		max = mem.txs.Len()
	}

	txs := make([]types.Tx, 0, tmmath.MinInt(mem.txs.Len(), max))
	for e := mem.txs.Front(); e != nil && len(txs) <= max; e = e.Next() {
		memTx := e.Value.(*mempoolTx)
		txs = append(txs, memTx.tx)
	}
	return txs
}

func (mem *cListMempool) recheckTxs(proxyAppConn proxy.AppConnMempool) {
	if mem.Size() == 0 {
		panic("recheckTxs is called, but the mempool is empty")
	}

	mem.recheckCursor = mem.txs.Front()
	mem.recheckEnd = mem.txs.Back()

	// Push txs to proxyAppConn
	// NOTE: globalCb may be called concurrently.
	for e := mem.txs.Front(); e != nil; e = e.Next() {
		memTx := e.Value.(*mempoolTx)
		proxyAppConn.CheckTxAsync(abcix.RequestCheckTx{
			Tx:   memTx.tx,
			Type: abcix.CheckTxType_Recheck,
		})
	}

	proxyAppConn.FlushAsync()
}

func (mem *cListMempool) getNextTxBytes(remainBytes int64, remainGas int64, starter []byte) ([]byte, error) {
	prevIdx, prevPriority := -1, uint64(math.MaxUint64)
	if len(starter) > 0 {
		for elem, idx := mem.txs.Front(), 0; elem != nil; elem, idx = elem.Next(), idx+1 {
			if bytes.Equal(elem.Value.(*mempoolTx).tx, starter) {
				prevIdx = idx
				prevPriority = elem.Priority
				break
			}
		}
	}

	var candidate *clist.CElement
	for elem, idx := mem.txs.Front(), 0; elem != nil; elem, idx = elem.Next(), idx+1 {
		mTx := elem.Value.(*mempoolTx)
		if (mTx.gasWanted > remainGas || int64(len(mTx.tx)) > remainBytes) || // tx requirement not met
			(elem.Priority > prevPriority) || // higher priority should have been iterated before
			(elem.Priority == prevPriority && idx <= prevIdx) { // equal priority but already sent
			continue
		}
		if candidate == nil || elem.Priority > candidate.Priority {
			candidate = elem
		}
	}
	if candidate == nil {
		// Target tx not found
		return nil, nil
	}
	return candidate.Value.(*mempoolTx).tx, nil
}

func (mem *cListMempool) deleteAll() {
	for e := mem.txs.Front(); e != nil; e = e.Next() {
		mem.txs.Remove(e)
		e.DetachPrev()
	}

	mem.txsMap.Range(func(key, _ interface{}) bool {
		mem.txsMap.Delete(key)
		return true
	})
}

func (mem *cListMempool) isRecheckCursorNil() bool {
	return mem.recheckCursor == nil
}

func (mem *cListMempool) getRecheckCursorTx() *mempoolTx {
	return mem.recheckCursor.Value.(*mempoolTx)
}

func (mem *cListMempool) getMempoolTx(tx types.Tx) *mempoolTx {
	if e, ok := mem.txsMap.Load(TxKey(tx)); ok {
		return e.(*clist.CElement).Value.(*mempoolTx)
	}
	return nil
}
