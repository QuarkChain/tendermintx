package mempool

import (
	"bytes"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	abcix "github.com/tendermint/tendermint/abcix/types"
	cfg "github.com/tendermint/tendermint/config"
	auto "github.com/tendermint/tendermint/libs/autofile"
	"github.com/tendermint/tendermint/libs/llrb"
	"github.com/tendermint/tendermint/libs/log"
	tmos "github.com/tendermint/tendermint/libs/os"
	"github.com/tendermint/tendermint/p2p"
	"github.com/tendermint/tendermint/proxy"
	"github.com/tendermint/tendermint/types"
)

//--------------------------------------------------------------------------------
var (
	maxBytes       = types.DefaultConsensusParams().Block.MaxGas
	maxGas   int64 = 1<<(64-1) - 1
)

// lElement is used to insert, remove, or recheck transactions
type lElement struct {
	priority  uint64
	timeStamp time.Time
	tx        *mempoolTx
}

//--------------------------------------------------------------------------------

// LlrbMempool is an ordered in-memory pool for transactions before they are
// proposed in a consensus round. Transaction validity is checked using the
// CheckTx abci message before the transaction is added to the pool. The
// mempool uses a left-leaning red-black tree structure for storing transactions that can
// be efficiently accessed by multiple concurrent readers.
type LlrbMempool struct {
	// Atomic integers
	height   int64 // the last block Update()'d to
	txsBytes int64 // total size of mempool, in bytes

	// notify listeners (ie. consensus) when txs are available
	notifiedTxsAvailable bool
	txsAvailable         chan struct{} // fires once for each height, when the mempool is not empty

	config *cfg.MempoolConfig

	// Exclusive mutex for Update method to prevent concurrent execution of
	// CheckTx or ReapMaxBytesMaxGas(ReapMaxTxs) methods.
	updateMtx sync.RWMutex
	preCheck  PreCheckFunc
	postCheck PostCheckFunc

	wal          *auto.AutoFile // a log of mempool txs
	txs          *llrb.Llrb     // left-leaning red-black tree of good txs
	proxyAppConn proxy.AppConnMempool

	// Track whether we're rechecking txs.
	// These are not protected by a mutex and are expected to be mutated in
	// serial (ie. by abci responses which are called in serial).
	recheckCursor *lElement // next expected response
	recheckEnd    *lElement // re-checking stops here

	// Map for quick access to txs to record sender in CheckTx.
	// txsMap: txKey -> (priority, timestamp)
	txsMap sync.Map

	// Keep a cache of already-seen txs.
	// This reduces the pressure on the proxyApp.
	cache txCache

	logger log.Logger

	metrics *Metrics

	server *mempoolServer
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
		config:        config,
		proxyAppConn:  proxyAppConn,
		txs:           llrb.New(),
		height:        height,
		recheckCursor: nil,
		recheckEnd:    nil,
		logger:        log.NewNopLogger(),
		metrics:       NopMetrics(),
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
	return mempool
}

// NOTE: not thread safe - should only be called once, on startup
func (mem *LlrbMempool) EnableTxsAvailable() {
	mem.txsAvailable = make(chan struct{}, 1)
}

// SetLogger sets the Logger.
func (mem *LlrbMempool) SetLogger(l log.Logger) {
	mem.logger = l
}

func (mem *LlrbMempool) InitWAL() error {
	var (
		walDir  = mem.config.WalDir()
		walFile = walDir + "/wal"
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

func (mem *LlrbMempool) CloseWAL() {
	if err := mem.wal.Close(); err != nil {
		mem.logger.Error("Error closing WAL", "err", err)
	}
	mem.wal = nil
}

// Safe for concurrent use by multiple goroutines.
func (mem *LlrbMempool) Lock() {
	mem.updateMtx.Lock()
}

// Safe for concurrent use by multiple goroutines.
func (mem *LlrbMempool) Unlock() {
	mem.updateMtx.Unlock()
}

// Safe for concurrent use by multiple goroutines.
func (mem *LlrbMempool) Size() int {
	return mem.txs.Size()
}

// Safe for concurrent use by multiple goroutines.
func (mem *LlrbMempool) TxsBytes() int64 {
	return atomic.LoadInt64(&mem.txsBytes)
}

// Lock() must be help by the caller during execution.
func (mem *LlrbMempool) FlushAppConn() error {
	return mem.proxyAppConn.FlushSync()
}

// XXX: Unsafe! Calling Flush may leave mempool in inconsistent state.
func (mem *LlrbMempool) Flush() {
	var e *lElement
	mem.updateMtx.RLock()
	defer mem.updateMtx.RUnlock()

	_ = atomic.SwapInt64(&mem.txsBytes, 0)
	mem.cache.Reset()

	for {
		tx, err := mem.txs.GetNext(e.priority, e.timeStamp, maxBytes, maxGas)
		if err != nil {
			panic(err)
		}
		if len(tx) == 0 {
			break
		}
		if i, ok := mem.txsMap.Load(TxKey(tx)); ok {
			e = i.(*lElement)
		}
		mem.Lock()
		mem.txs.Delete(e.priority, e.timeStamp)
		mem.Unlock()
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
			mem.logger.Error("Error writing to WAL", "err", err)
		}
		_, err = mem.wal.Write([]byte("\n"))
		if err != nil {
			mem.logger.Error("Error writing to WAL", "err", err)
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
	mem.Lock()
	if err := mem.txs.Insert(priority, timeStamp, memTx); err != nil {
		panic("failed to insert tx into llrb mempool")
	}
	mem.Unlock()
	mem.txsMap.Store(TxKey(memTx.tx), lElement{priority, timeStamp, memTx})
	atomic.AddInt64(&mem.txsBytes, int64(len(memTx.tx)))
	mem.metrics.TxSizeBytes.Observe(float64(len(memTx.tx)))
}

// Called from:
//  - Update (lock held) if tx was committed
// 	- resCbRecheck (lock not held) if tx was invalidated
func (mem *LlrbMempool) removeTx(tx types.Tx, elem *lElement, removeFromCache bool) {
	mem.Lock()
	mem.txs.Delete(elem.priority, elem.timeStamp)
	mem.Unlock()
	mem.txsMap.Delete(TxKey(tx))
	atomic.AddInt64(&mem.txsBytes, int64(-len(tx)))

	if removeFromCache {
		mem.cache.Remove(tx)
	}
}

func (mem *LlrbMempool) isFull(txSize int) error {
	var (
		memSize  = mem.Size()
		txsBytes = mem.TxsBytes()
	)

	if memSize >= mem.config.Size || int64(txSize)+txsBytes > mem.config.MaxTxsBytes {
		return ErrMempoolIsFull{
			memSize, mem.config.Size,
			txsBytes, mem.config.MaxTxsBytes,
		}
	}

	return nil
}

// callback, which is called after the app checked the tx for the first time.
//
// The case where the app checks the tx for the second and subsequent times is
// handled by the resCbRecheck callback.
func (mem *LlrbMempool) resCbFirstTime(
	tx []byte,
	peerID uint16,
	peerP2PID p2p.ID,
	res *abcix.Response,
) {
	switch r := res.Value.(type) {
	case *abcix.Response_CheckTx:
		var postCheckErr error
		if mem.postCheck != nil {
			postCheckErr = mem.postCheck(tx, r.CheckTx)
		}
		if (r.CheckTx.Code == abcix.CodeTypeOK) && postCheckErr == nil {
			// Check mempool isn't full again to reduce the chance of exceeding the
			// limits.
			if err := mem.isFull(len(tx)); err != nil {
				// remove from cache (mempool might have a space later)
				mem.cache.Remove(tx)
				mem.logger.Error(err.Error())
				return
			}

			memTx := &mempoolTx{
				height:    mem.height,
				gasWanted: r.CheckTx.GasWanted,
				tx:        tx,
			}
			memTx.senders.Store(peerID, true)
			mem.addTx(memTx, r.CheckTx.Priority)
			mem.logger.Info("Added good transaction",
				"tx", txID(tx),
				"res", r,
				"height", memTx.height,
				"total", mem.Size(),
			)
			mem.notifyTxsAvailable()
		} else {
			// ignore bad transaction
			mem.logger.Info("Rejected bad transaction",
				"tx", txID(tx), "peerID", peerP2PID, "res", r, "err", postCheckErr)
			mem.metrics.FailedTxs.Add(1)
			// remove from cache (it might be good later)
			mem.cache.Remove(tx)
		}
	default:
		// ignore other messages
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
			tx, err := mem.txs.GetNext(mem.recheckCursor.priority, mem.recheckCursor.timeStamp, maxBytes, maxGas)
			if err != nil {
				panic("failed to get next tx from mempool")
			}
			if e, ok := mem.txsMap.Load(TxKey(tx)); ok {
				mem.recheckCursor = e.(*lElement)
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

// Safe for concurrent use by multiple goroutines.
func (mem *LlrbMempool) TxsAvailable() <-chan struct{} {
	return mem.txsAvailable
}

func (mem *LlrbMempool) notifyTxsAvailable() {
	if mem.Size() == 0 {
		panic("notified txs available but mempool is empty!")
	}
	if mem.txsAvailable != nil && !mem.notifiedTxsAvailable {
		// channel cap is 1, so this will send once
		mem.notifiedTxsAvailable = true
		select {
		case mem.txsAvailable <- struct{}{}:
		default:
		}
	}
}

func (mem *LlrbMempool) ReapMaxBytesMaxGas(maxBytes, maxGas int64) types.Txs {
	panic("implement me")
}

func (mem *LlrbMempool) ReapMaxTxs(max int) types.Txs {
	panic("implement me")
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
	if mem.Size() == 0 {
		panic("recheckTxs is called, but the mempool is empty")
	}

	tx, err := mem.txs.GetNext(0, time.Time{}, maxBytes, maxGas)
	if err != nil {
		panic("failed to find head")
	}
	if e, ok := mem.txsMap.Load(TxKey(tx)); ok {
		mem.recheckCursor = e.(*lElement)
		mem.recheckEnd = e.(*lElement)
	}

	// Push txs to proxyAppConn
	// NOTE: globalCb may be called concurrently.
	for {
		nextTx, err := mem.txs.GetNext(mem.recheckEnd.priority, mem.recheckEnd.timeStamp, maxBytes, maxGas)
		if err != nil {
			panic("failed to find next tx")
		}
		if len(nextTx) == 0 {
			break
		}
		mem.proxyAppConn.CheckTxAsync(abcix.RequestCheckTx{
			Tx:   nextTx,
			Type: abcix.CheckTxType_Recheck,
		})
		if e, ok := mem.txsMap.Load(TxKey(nextTx)); ok {
			mem.recheckEnd = e.(*lElement)
		}
	}

	mem.proxyAppConn.FlushAsync()
}

// GetNextTxBytes finds satisfied tx with two iterations which cost O(N) time, will be optimized with balance tree
// or other techniques to reduce the time complexity to O(logN) or even O(1)
func (mem *LlrbMempool) GetNextTxBytes(remainBytes int64, remainGas int64, starter []byte) ([]byte, error) {
	mem.updateMtx.RLock()
	defer mem.updateMtx.RUnlock()
	if e, ok := mem.txsMap.Load(TxKey(starter)); ok {
		tx, err := mem.txs.GetNext(e.(*lElement).priority, e.(*lElement).timeStamp, remainBytes, remainGas)
		if err != nil {
			return nil, err
		}
		return tx, nil
	}
	return nil, nil
}
