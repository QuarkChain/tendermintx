package mempool

import (
	"sync"
	"time"

	"github.com/pkg/errors"

	abcix "github.com/tendermint/tendermint/abcix/types"
	cfg "github.com/tendermint/tendermint/config"
	tmmath "github.com/tendermint/tendermint/libs/math"
	"github.com/tendermint/tendermint/libs/tree"
	"github.com/tendermint/tendermint/proxy"
	"github.com/tendermint/tendermint/types"
)

//--------------------------------------------------------------------------------

// tElement is used to insert, remove, or recheck transactions
type tElement struct {
	nodeKey tree.NodeKey
	tx      *mempoolTx
}

//--------------------------------------------------------------------------------

// treemempool is an ordered in-memory pool for transactions before they are
// proposed in a consensus round. Transaction validity is checked using the
// CheckTx abci message before the transaction is added to the pool. The
// mempool uses a balanced tree structure for storing transactions that can
// be efficiently accessed by multiple concurrent readers.
type treeMempool struct {
	txs     tree.BalancedTree        // balanced tree of good txs
	treeGen func() tree.BalancedTree // return a specified balanced tree

	// Map for quick access to txs to record sender in CheckTx.
	// txsMap: txKey -> tElement
	txsMap sync.Map

	// Track whether we're rechecking txs.
	// These are not protected by a mutex and are expected to be mutated in
	// serial (ie. by abci responses which are called in serial).
	recheckCursor *tElement    // next expected response
	recheckEnd    tree.NodeKey // re-checking stops here
}

// newTreeMempool returns a new mempool with the given configuration and connection to an application.
func newTreeMempool(
	config *cfg.MempoolConfig,
	proxyAppConn proxy.AppConnMempool,
	height int64,
	treeGen func() tree.BalancedTree,
	options ...Option,
) Mempool {
	treeMempool := &treeMempool{txs: treeGen(), treeGen: treeGen}
	return newBasemempool(treeMempool, config, proxyAppConn, height, options...)
}

func NewLLRBMempool(
	config *cfg.MempoolConfig,
	proxyAppConn proxy.AppConnMempool,
	height int64,
	options ...Option,
) Mempool {
	return newTreeMempool(config, proxyAppConn, height, tree.NewLLRB, options...)
}

func NewBTreeMempool(
	config *cfg.MempoolConfig,
	proxyAppConn proxy.AppConnMempool,
	height int64,
	options ...Option,
) Mempool {
	return newTreeMempool(config, proxyAppConn, height, tree.NewBTree, options...)
}

// Safe for concurrent use by multiple goroutines.
func (mem *treeMempool) Size() int {
	return mem.txs.Size()
}

// Called from:
//  - resCbFirstTime (lock not held) if tx is valid
func (mem *treeMempool) addTx(memTx *mempoolTx, priority uint64) {
	hash := TxKey(memTx.tx)
	nodeKey := tree.NodeKey{Priority: priority, TS: time.Now(), Hash: hash}
	mem.txs.Insert(nodeKey, memTx)
	mem.txsMap.Store(hash, &tElement{nodeKey, memTx})
}

// Called from:
//  - Update (lock held) if tx was committed
//  - resCbRecheck (lock not held) if tx was invalidated
//  - RemoveTxs (lock held) for invalid txs from CreateBlock response
func (mem *treeMempool) removeTx(tx types.Tx) (elemRemoved bool) {
	if e, ok := mem.txsMap.Load(TxKey(tx)); ok {
		elem := e.(*tElement)
		if _, err := mem.txs.Remove(elem.nodeKey); err != nil {
			// TODO: better error handling here
			// considering this function may be called concurrently from resCbRecheck,
			// should probably just log the errors
			panic("deleting an nonexistent node from tree")
		}
		elemRemoved = true
	}
	mem.txsMap.Delete(TxKey(tx))
	return
}

func (mem *treeMempool) updateRecheckCursor() {
	if mem.recheckCursor.nodeKey == mem.recheckEnd {
		mem.recheckCursor = nil
	} else {
		memTx, next, err := mem.txs.GetNext(&mem.recheckCursor.nodeKey, nil)
		if err != nil {
			mem.recheckCursor = nil
		} else {
			mem.recheckCursor = &tElement{nodeKey: next, tx: memTx.(*mempoolTx)}
		}
	}
}

func (mem *treeMempool) reapMaxTxs(max int) types.Txs {
	if max < 0 {
		max = mem.txs.Size()
	}

	txs := make([]types.Tx, 0, tmmath.MinInt(mem.txs.Size(), max))

	var starter *tree.NodeKey
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

func (mem *treeMempool) recheckTxs(proxyAppConn proxy.AppConnMempool) {
	if mem.Size() == 0 {
		panic("recheckTxs is called, but the mempool is empty")
	}

	mem.recheckCursor = nil
	// Push txs to proxyAppConn
	// NOTE: globalCb may be called concurrently.
	var starter *tree.NodeKey
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
			mem.recheckCursor = &tElement{nodeKey: next, tx: mptx}
		}

		proxyAppConn.CheckTxAsync(abcix.RequestCheckTx{
			Tx:   mptx.tx,
			Type: abcix.CheckTxType_Recheck,
		})

		starter = &next
	}

	proxyAppConn.FlushAsync()
}

func (mem *treeMempool) getNextTxBytes(remainBytes int64, remainGas int64, starter []byte) ([]byte, error) {
	var prevNodeKey *tree.NodeKey
	if len(starter) > 0 {
		if e, ok := mem.txsMap.Load(TxKey(starter)); ok {
			prevNodeKey = &e.(*tElement).nodeKey
		}
	}
	memTx, _, err := mem.txs.GetNext(prevNodeKey, func(i interface{}) bool {
		return i.(*mempoolTx).gasWanted <= remainGas && (int64(len(i.(*mempoolTx).tx)) <= remainBytes)
	})
	if err == tree.ErrorStopIteration {
		return nil, nil
	} else if err != nil {
		return nil, errors.Wrap(err, "failed to get next tx from tree")
	}
	return memTx.(*mempoolTx).tx, nil
}

func (mem *treeMempool) deleteAll() {
	mem.txs = mem.treeGen()
	mem.txsMap.Range(func(key, _ interface{}) bool {
		mem.txsMap.Delete(key)
		return true
	})
}

func (mem *treeMempool) isRecheckCursorNil() bool {
	return mem.recheckCursor == nil
}

func (mem *treeMempool) getRecheckCursorTx() *mempoolTx {
	return mem.recheckCursor.tx
}

func (mem *treeMempool) getMempoolTx(tx types.Tx) *mempoolTx {
	if e, ok := mem.txsMap.Load(TxKey(tx)); ok {
		return e.(*tElement).tx
	}
	return nil
}
