package mempool

import (
	"bytes"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	tmos "github.com/tendermint/tendermint/libs/os"

	auto "github.com/tendermint/tendermint/libs/autofile"

	tmmath "github.com/tendermint/tendermint/libs/math"

	abcix "github.com/tendermint/tendermint/abcix/types"
	cfg "github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/libs/llrb"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/p2p"
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

// LlrbMempool is an ordered in-memory pool for transactions before they are
// proposed in a consensus round. Transaction validity is checked using the
// CheckTx abci message before the transaction is added to the pool. The
// mempool uses a left-leaning red-black tree structure for storing transactions that can
// be efficiently accessed by multiple concurrent readers.
type LlrbMempool struct {
	umempool
	txs llrb.LLRB // left-leaning red-black tree of good txs
	// Map for quick access to txs to record sender in CheckTx.
	// txsMap: txKey -> lElement
	txsMap   sync.Map
	preCheck PreCheckFunc
	// Track whether we're rechecking txs.
	// These are not protected by a mutex and are expected to be mutated in
	// serial (ie. by abci responses which are called in serial).
	recheckCursor *lElement // next expected response
	recheckEnd    *lElement // re-checking stops here
	server        *mempoolServer
}

var _ Mempool = &LlrbMempool{}

// LlrbMempoolOption sets an optional parameter on the mempool.
type LlrbMempoolOption func(*LlrbMempool)

// NewLlrbMempool returns a new mempool with the given configuration and connection to an application.
func NewLlrbMempool(
	config *cfg.MempoolConfig,
	proxyAppConn proxy.AppConnMempool,
	height int64,
	options ...LlrbMempoolOption,
) *LlrbMempool {
	mempool := &LlrbMempool{
		umempool: umempool{
			config:       config,
			proxyAppConn: proxyAppConn,
			height:       height,
			logger:       log.NewNopLogger(),
			metrics:      NopMetrics(),
		},
		txs:           llrb.New(),
		recheckCursor: nil,
		recheckEnd:    nil,
	}

	if config.CacheSize > 0 {
		mempool.cache = newMapTxCache(config.CacheSize)
	} else {
		mempool.cache = nopTxCache{}
	}
	proxyAppConn.SetResponseCallback(mempool.globalCb)
	for _, option := range options {
		option(mempool)
	}
	// TODO: mempool server should be bound to balance tree-based mempool. use clist here for now
	if config.ServerHostPort != "" {
		server, err := newMempoolServer(config.ServerHostPort)
		if err != nil {
			panic(err)
		}
		mempool.server = server
	}

	mempool.umempool.txAdder = func(tx *mempoolTx, priority uint64) { mempool.addTx(tx, priority) }
	mempool.umempool.sizer = mempool.Size

	return mempool
}

func (mem *LlrbMempool) InitWAL() error {
	var (
		walDir  = mem.config.WalDir()
		walFile = walDir + "/lwal"
	)

	const perm = 0700
	if err := tmos.EnsureDir(walDir, perm); err != nil {
		return err
	}

	af, err := auto.OpenAutoFile(walFile)
	if err != nil {
		return fmt.Errorf("can't open autofile %s: %w", walFile, err)
	}

	mem.wal = af
	return nil
}

// Safe for concurrent use by multiple goroutines.
func (mem *LlrbMempool) Size() int {
	return mem.txs.Size()
}

// XXX: Unsafe! Calling Flush may leave mempool in inconsistent state.
func (mem *LlrbMempool) Flush() {
	var e *lElement
	mem.updateMtx.RLock()
	defer mem.updateMtx.RUnlock()

	_ = atomic.SwapInt64(&mem.txsBytes, 0)
	mem.cache.Reset()

	for {
		memTx, err := mem.txs.GetNext(nil, nil)
		if err != nil {
			break
		}
		tx := (*memTx.(**mempoolTx)).tx
		if i, ok := mem.txsMap.Load(TxKey(tx)); ok {
			e = i.(*lElement)
		}
		if _, err := mem.txs.Remove(e.nodeKey); err != nil {
			panic(err)
		}
	}

	mem.txsMap.Range(func(key, _ interface{}) bool {
		mem.txsMap.Delete(key)
		return true
	})
}

// It blocks if we're waiting on Update() or Reap().
// cb: A callback from the CheckTx command.
//     It gets called from another goroutine.
// CONTRACT: Either cb will get called, or err returned.
//
// Safe for concurrent use by multiple goroutines.
func (mem *LlrbMempool) CheckTx(tx types.Tx, cb func(*abcix.Response), txInfo TxInfo) error {
	mem.updateMtx.RLock()
	// use defer to unlock mutex because application (*local client*) might panic
	defer mem.updateMtx.RUnlock()

	txSize := len(tx)

	if err := mem.isFull(txSize); err != nil {
		return err
	}

	// The size of the corresponding TxMessage
	// can't be larger than the maxMsgSize, otherwise we can't
	// relay it to peers.
	if txSize > mem.config.MaxTxBytes {
		return ErrTxTooLarge{mem.config.MaxTxBytes, txSize}
	}

	if mem.preCheck != nil {
		if err := mem.preCheck(tx); err != nil {
			return ErrPreCheck{err}
		}
	}

	// CACHE
	if !mem.cache.Push(tx) {
		// Record a new sender for a tx we've already seen.
		// Note it's possible a tx is still in the cache but no longer in the mempool
		// (eg. after committing a block, txs are removed from mempool but not cache),
		// so we only record the sender for txs still in the mempool.
		if e, ok := mem.txsMap.Load(TxKey(tx)); ok {
			memTx := e.(*lElement).tx
			memTx.senders.LoadOrStore(txInfo.SenderID, true)
			// TODO: consider punishing peer for dups,
			// its non-trivial since invalid txs can become valid,
			// but they can spam the same tx with little cost to them atm.

		}

		return ErrTxInCache
	}
	// END CACHE

	// WAL
	if mem.wal != nil {
		// TODO: Notify administrators when WAL fails
		_, err := mem.wal.Write([]byte(tx))
		if err != nil {
			mem.logger.Error("Error writing to lWAL", "err", err)
		}
		_, err = mem.wal.Write([]byte("\n"))
		if err != nil {
			mem.logger.Error("Error writing to lWAL", "err", err)
		}
	}
	// END WAL

	// NOTE: proxyAppConn may error if tx buffer is full
	if err := mem.proxyAppConn.Error(); err != nil {
		return err
	}

	reqRes := mem.proxyAppConn.CheckTxAsync(abcix.RequestCheckTx{Tx: tx})
	reqRes.SetCallback(mem.reqResCb(tx, txInfo.SenderID, txInfo.SenderP2PID, cb))

	return nil
}

// Global callback that will be called after every ABCI response.
// Having a single global callback avoids needing to set a callback for each request.
// However, processing the checkTx response requires the peerID (so we can track which txs we heard from who),
// and peerID is not included in the ABCI request, so we have to set request-specific callbacks that
// include this information. If we're not in the midst of a recheck, this function will just return,
// so the request specific callback can do the work.
//
// When rechecking, we don't need the peerID, so the recheck callback happens
// here.
func (mem *LlrbMempool) globalCb(req *abcix.Request, res *abcix.Response) {
	if mem.recheckCursor == nil {
		return
	}

	mem.metrics.RecheckTimes.Add(1)
	mem.resCbRecheck(req, res)

	// update metrics
	mem.metrics.Size.Set(float64(mem.Size()))
}

// Request specific callback that should be set on individual reqRes objects
// to incorporate local information when processing the response.
// This allows us to track the peer that sent us this tx, so we can avoid sending it back to them.
// NOTE: alternatively, we could include this information in the ABCI request itself.
//
// External callers of CheckTx, like the RPC, can also pass an externalCb through here that is called
// when all other response processing is complete.
//
// Used in CheckTx to record PeerID who sent us the tx.
func (mem *LlrbMempool) reqResCb(
	tx []byte,
	peerID uint16,
	peerP2PID p2p.ID,
	externalCb func(*abcix.Response),
) func(res *abcix.Response) {
	return func(res *abcix.Response) {
		if mem.recheckCursor != nil {
			// this should never happen
			panic("recheck cursor is not nil in reqResCb")
		}

		mem.resCbFirstTime(tx, peerID, peerP2PID, res)

		// update metrics
		mem.metrics.Size.Set(float64(mem.Size()))

		// passed in by the caller of CheckTx, eg. the RPC
		if externalCb != nil {
			externalCb(res)
		}
	}
}

// Called from:
//  - resCbFirstTime (lock not held) if tx is valid
func (mem *LlrbMempool) addTx(memTx *mempoolTx, priority uint64) {
	timeStamp := time.Now()
	e := &lElement{nodeKey: llrb.NodeKey{Priority: priority, TS: timeStamp}}
	if err := mem.txs.Insert(e.nodeKey, &memTx); err != nil {
		panic("failed to insert tx into llrb mempool")
	}
	mem.txsMap.Store(TxKey(memTx.tx), &lElement{e.nodeKey, memTx})
	atomic.AddInt64(&mem.txsBytes, int64(len(memTx.tx)))
	mem.metrics.TxSizeBytes.Observe(float64(len(memTx.tx)))
}

// Called from:
//  - Update (lock held) if tx was committed
// 	- resCbRecheck (lock not held) if tx was invalidated
func (mem *LlrbMempool) removeTx(tx types.Tx, elem *lElement, removeFromCache bool) {
	mem.Lock()
	mem.txs.Remove(elem.nodeKey)
	mem.Unlock()
	mem.txsMap.Delete(TxKey(tx))
	atomic.AddInt64(&mem.txsBytes, int64(-len(tx)))

	if removeFromCache {
		mem.cache.Remove(tx)
	}
}

// callback, which is called after the app rechecked the tx.
//
// The case where the app checks the tx for the first time is handled by the
// resCbFirstTime callback.
func (mem *LlrbMempool) resCbRecheck(req *abcix.Request, res *abcix.Response) {
	switch r := res.Value.(type) {
	case *abcix.Response_CheckTx:
		tx := req.GetCheckTx().Tx
		memTx := mem.recheckCursor.tx
		if !bytes.Equal(tx, memTx.tx) {
			panic(fmt.Sprintf(
				"Unexpected tx response from proxy during recheck\nExpected %X, got %X",
				memTx.tx,
				tx))
		}
		var postCheckErr error
		if mem.postCheck != nil {
			postCheckErr = mem.postCheck(tx, r.CheckTx)
		}
		if (r.CheckTx.Code == abcix.CodeTypeOK) && postCheckErr == nil {
			// Good, nothing to do.
		} else {
			// Tx became invalidated due to newly committed block.
			mem.logger.Info("Tx is no longer valid", "tx", txID(tx), "res", r, "err", postCheckErr)
			// NOTE: we remove tx from the cache because it might be good later
			mem.removeTx(tx, mem.recheckCursor, true)
		}
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
		if mem.recheckCursor == nil {
			// Done!
			mem.logger.Info("Done rechecking txs")

			// incase the recheck removed all txs
			if mem.Size() > 0 {
				mem.notifyTxsAvailable()
			}
		}
	default:
		// ignore other messages
	}
}

func (mem *LlrbMempool) ReapMaxBytesMaxGas(maxBytes, maxGas int64) types.Txs {
	panic("implement me")
}

func (mem *LlrbMempool) ReapMaxTxs(max int) types.Txs {
	mem.updateMtx.RLock()
	defer mem.updateMtx.RUnlock()

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

// Lock() must be help by the caller during execution.
func (mem *LlrbMempool) Update(
	height int64,
	txs types.Txs,
	deliverTxResponses []*abcix.ResponseDeliverTx,
	preCheck PreCheckFunc,
	postCheck PostCheckFunc,
) error {
	// Set height
	mem.height = height
	mem.notifiedTxsAvailable = false

	if preCheck != nil {
		mem.preCheck = preCheck
	}
	if postCheck != nil {
		mem.postCheck = postCheck
	}

	for i, tx := range txs {
		if deliverTxResponses[i].Code == abcix.CodeTypeOK {
			// Add valid committed tx to the cache (if missing).
			_ = mem.cache.Push(tx)
		} else {
			// Allow invalid transactions to be resubmitted.
			mem.cache.Remove(tx)
		}

		// Remove committed tx from the mempool.
		//
		// Note an evil proposer can drop valid txs!
		// Mempool before:
		//   100 -> 101 -> 102
		// Block, proposed by an evil proposer:
		//   101 -> 102
		// Mempool after:
		//   100
		// https://github.com/tendermint/tendermint/issues/3322.
		if e, ok := mem.txsMap.Load(TxKey(tx)); ok {
			mem.removeTx(tx, e.(*lElement), false)
		}
	}

	// Either recheck non-committed txs to see if they became invalid
	// or just notify there're some txs left.
	if mem.Size() > 0 {
		if mem.config.Recheck {
			mem.logger.Info("Recheck txs", "numtxs", mem.Size(), "height", height)
			mem.recheckTxs()
			// At this point, mem.txs are being rechecked.
			// mem.recheckCursor re-scans mem.txs and possibly removes some txs.
			// Before mem.Reap(), we should wait for mem.recheckCursor to be nil.
		} else {
			mem.notifyTxsAvailable()
		}
	}

	// Update metrics
	mem.metrics.Size.Set(float64(mem.Size()))

	return nil
}

func (mem *LlrbMempool) recheckTxs() {
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
		mem.proxyAppConn.CheckTxAsync(abcix.RequestCheckTx{
			Tx:   tx,
			Type: abcix.CheckTxType_Recheck,
		})
		if e, ok := mem.txsMap.Load(TxKey(tx)); ok {
			tempE = (*e.(*lElement))
		}
	}

	mem.proxyAppConn.FlushAsync()
}

// GetNextTxBytes finds satisfied tx with two iterations which cost O(N) time, will be optimized with balance tree
// or other techniques to reduce the time complexity to O(logN) or even O(1)
func (mem *LlrbMempool) GetNextTxBytes(remainBytes int64, remainGas int64, starter []byte) ([]byte, error) {

	mem.updateMtx.RLock()
	defer mem.updateMtx.RUnlock()

	var prevElement lElement
	if len(starter) > 0 {
		if e, ok := mem.txsMap.Load(TxKey(starter)); ok {
			prevElement = *e.(*lElement)
		}
	}
	memTx, err := mem.txs.GetNext(&(prevElement.nodeKey), func(i interface{}) bool {
		return ((*i.(**mempoolTx)).gasWanted <= remainGas) && (int64(len((*i.(**mempoolTx)).tx)) <= remainBytes)
	})
	if err != nil {
		return nil, nil
	}
	tx := (*memTx.(**mempoolTx)).tx
	return tx, nil
}
