package mempool

import (
	"bytes"
	"container/list"
	"crypto/sha256"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/pkg/errors"

	abcix "github.com/tendermint/tendermint/abcix/types"
	cfg "github.com/tendermint/tendermint/config"
	auto "github.com/tendermint/tendermint/libs/autofile"
	"github.com/tendermint/tendermint/libs/log"
	tmos "github.com/tendermint/tendermint/libs/os"
	"github.com/tendermint/tendermint/p2p"
	"github.com/tendermint/tendermint/proxy"
	"github.com/tendermint/tendermint/types"
)

// Mempool defines the mempool interface.
//
// Updates to the mempool need to be synchronized with committing a block so
// apps can reset their transient state on Commit.
type Mempool interface {
	// CheckTx executes a new transaction against the application to determine
	// its validity and whether it should be added to the mempool.
	CheckTx(tx types.Tx, callback func(*abcix.Response), txInfo TxInfo) error

	// ReapMaxTxs reaps up to max transactions from the mempool.
	// If max is negative, there is no cap on the size of all returned
	// transactions (~ all available transactions).
	ReapMaxTxs(max int) types.Txs

	// GetNextTxBytes will return transaction with condition that Bytes and Gas
	// must be less than remainBytes and remainGas, and with highest priority less
	// than the priority of stater
	GetNextTxBytes(remainBytes int64, remainGas int64, starter []byte) ([]byte, error)

	// Lock locks the mempool. The consensus must be able to hold lock to safely update.
	Lock()

	// Unlock unlocks the mempool.
	Unlock()

	// Update informs the mempool that the given txs were committed and can be discarded.
	// NOTE: this should be called *after* block is committed by consensus.
	// NOTE: Lock/Unlock must be managed by caller
	Update(
		blockHeight int64,
		blockTxs types.Txs,
		deliverTxResponses []*abcix.ResponseDeliverTx,
		newPreFn PreCheckFunc,
		newPostFn PostCheckFunc,
	) error

	// FlushAppConn flushes the mempool connection to ensure async reqResCb calls are
	// done. E.g. from CheckTx.
	// NOTE: Lock/Unlock must be managed by caller
	FlushAppConn() error

	// Flush removes all transactions from the mempool and cache
	Flush()

	// TxsAvailable returns a channel which fires once for every height,
	// and only when transactions are available in the mempool.
	// NOTE: the returned channel may be nil if EnableTxsAvailable was not called.
	TxsAvailable() <-chan struct{}

	// EnableTxsAvailable initializes the TxsAvailable channel, ensuring it will
	// trigger once every height when transactions are available.
	EnableTxsAvailable()

	// Size returns the number of transactions in the mempool.
	Size() int

	// TxsBytes returns the total size of all txs in the mempool.
	TxsBytes() int64

	// InitWAL creates a directory for the WAL file and opens a file itself. If
	// there is an error, it will be of type *PathError.
	InitWAL() error

	// CloseWAL closes and discards the underlying WAL file.
	// Any further writes will not be relayed to disk.
	CloseWAL()

	SetLogger(l log.Logger)

	// RemoveTxs removes invalid txs from mempool included in ResponseCreateBlock
	RemoveTxs(blockTxs types.Txs) error
}

//--------------------------------------------------------------------------------

// PreCheckFunc is an optional filter executed before CheckTx and rejects
// transaction if false is returned. An example would be to ensure that a
// transaction doesn't exceeded the block size.
type PreCheckFunc func(types.Tx) error

// PostCheckFunc is an optional filter executed after CheckTx and rejects
// transaction if false is returned. An example would be to ensure a
// transaction doesn't require more gas than available for the block.
type PostCheckFunc func(types.Tx, *abcix.ResponseCheckTx) error

// TxInfo are parameters that get passed when attempting to add a tx to the
// mempool.
type TxInfo struct {
	// SenderID is the internal peer ID used in the mempool to identify the
	// sender, storing 2 bytes with each tx instead of 20 bytes for the p2p.ID.
	SenderID uint16
	// SenderP2PID is the actual p2p.ID of the sender, used e.g. for logging.
	SenderP2PID p2p.ID
}

//--------------------------------------------------------------------------------

// PreCheckMaxBytes checks that the size of the transaction is smaller or equal to the expected maxBytes.
func PreCheckMaxBytes(maxBytes int64) PreCheckFunc {
	return func(tx types.Tx) error {
		txSize := int64(len(tx))
		if txSize > maxBytes {
			return fmt.Errorf("tx size is too big: %d, max: %d",
				txSize, maxBytes)
		}
		return nil
	}
}

// PostCheckMaxGas checks that the wanted gas is smaller or equal to the passed
// maxGas. Returns nil if maxGas is -1.
func PostCheckMaxGas(maxGas int64) PostCheckFunc {
	return func(tx types.Tx, res *abcix.ResponseCheckTx) error {
		if maxGas == -1 {
			return nil
		}
		if res.GasWanted < 0 {
			return fmt.Errorf("gas wanted %d is negative",
				res.GasWanted)
		}
		if res.GasWanted > maxGas {
			return fmt.Errorf("gas wanted %d is greater than max gas %d",
				res.GasWanted, maxGas)
		}
		return nil
	}
}

//--------------------------------------------------------------------------------

var _ Mempool = &basemempool{}

type basemempool struct {
	mempoolImpl

	// Atomic integers
	height   int64 // the last block Update()'d to
	txsBytes int64 // total size of mempool, in bytes

	// Notify listeners (ie. consensus) when txs are available
	notifiedTxsAvailable bool
	txsAvailable         chan struct{} // fires once for each height, when the mempool is not empty

	// Keep a cache of already-seen txs.
	// This reduces the pressure on the proxyApp.
	cache     txCache
	preCheck  PreCheckFunc
	postCheck PostCheckFunc

	config *cfg.MempoolConfig

	// Exclusive mutex for Update method to prevent concurrent execution of
	// CheckTx or ReapMaxBytesMaxGas(ReapMaxTxs) methods.
	updateMtx sync.RWMutex

	wal          *auto.AutoFile // a log of mempool txs
	proxyAppConn proxy.AppConnMempool

	logger  log.Logger
	metrics *Metrics

	// Slice of subscribers, will notify them when new tx comes
	reactorSubs   []reactorSub
	reactorSubMtx sync.Mutex
}

// Mempool reactor subscription
type reactorSub struct {
	txCh chan *mempoolTx
	quit chan struct{}
}

type mempoolImpl interface {
	Size() int
	GetNextTxBytes(remainBytes int64, remainGas int64, starter []byte) ([]byte, error)

	addTx(*mempoolTx, uint64)
	removeTx(types.Tx) bool // return whether corresponding element is removed or not
	updateRecheckCursor()
	reapMaxTxs(int) types.Txs
	recheckTxs(proxy.AppConnMempool)
	isRecheckCursorNil() bool
	getRecheckCursorTx() *mempoolTx
	getMempoolTx(types.Tx) *mempoolTx
	deleteAll()
	nextTx(tx *mempoolTx) *mempoolTx // not based on priority
}

// Option sets an optional parameter on the basemempool.
type Option func(*basemempool)

func newBasemempool(
	impl mempoolImpl,
	config *cfg.MempoolConfig,
	proxyAppConn proxy.AppConnMempool,
	height int64,
	options ...Option,
) *basemempool {
	mempool := &basemempool{
		mempoolImpl:  impl,
		config:       config,
		proxyAppConn: proxyAppConn,
		height:       height,
		logger:       log.NewNopLogger(),
		metrics:      NopMetrics(),
	}
	for _, option := range options {
		option(mempool)
	}
	proxyAppConn.SetResponseCallback(mempool.globalCb)
	if config.CacheSize > 0 {
		mempool.cache = newMapTxCache(config.CacheSize)
	} else {
		mempool.cache = nopTxCache{}
	}

	return mempool
}

// NOTE: not thread safe - should only be called once, on startup
func (mem *basemempool) EnableTxsAvailable() {
	mem.txsAvailable = make(chan struct{}, 1)
}

// SetLogger sets the Logger.
func (mem *basemempool) SetLogger(l log.Logger) {
	mem.logger = l
}

// WithPreCheck sets a filter for the mempool to reject a tx if f(tx) returns
// false. This is ran before CheckTx. Only applies to the first created block.
// After that, Update overwrites the existing value.
func WithPreCheck(f PreCheckFunc) Option {
	return func(mem *basemempool) { mem.preCheck = f }
}

// WithPostCheck sets a filter for the mempool to reject a tx if f(tx) returns
// false. This is ran after CheckTx. Only applies to the first created block.
// After that, Update overwrites the existing value.
func WithPostCheck(f PostCheckFunc) Option {
	return func(mem *basemempool) { mem.postCheck = f }
}

// WithMetrics sets the metrics.
func WithMetrics(metrics *Metrics) Option {
	return func(mem *basemempool) { mem.metrics = metrics }
}

func (mem *basemempool) InitWAL() error {
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

func (mem *basemempool) CloseWAL() {
	if err := mem.wal.Close(); err != nil {
		mem.logger.Error("Error closing WAL", "err", err)
	}
	mem.wal = nil
}

// Safe for concurrent use by multiple goroutines.
func (mem *basemempool) Lock() {
	mem.updateMtx.Lock()
}

// Safe for concurrent use by multiple goroutines.
func (mem *basemempool) Unlock() {
	mem.updateMtx.Unlock()
}

// Safe for concurrent use by multiple goroutines.
func (mem *basemempool) TxsBytes() int64 {
	return atomic.LoadInt64(&mem.txsBytes)
}

// Lock() must be held by the caller during execution.
func (mem *basemempool) FlushAppConn() error {
	return mem.proxyAppConn.FlushSync()
}

// XXX: Unsafe! Calling Flush may leave mempool in inconsistent state.
func (mem *basemempool) Flush() {
	mem.updateMtx.RLock()
	defer mem.updateMtx.RUnlock()

	_ = atomic.SwapInt64(&mem.txsBytes, 0)
	mem.cache.Reset()

	mem.deleteAll()
}

// It blocks if we're waiting on Update() or Reap().
// cb: A callback from the CheckTx command.
//     It gets called from another goroutine.
// CONTRACT: Either cb will get called, or err returned.
//
// Safe for concurrent use by multiple goroutines.
func (mem *basemempool) CheckTx(tx types.Tx, cb func(*abcix.Response), txInfo TxInfo) error {
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
		if memTx := mem.getMempoolTx(tx); memTx != nil {
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
func (mem *basemempool) globalCb(req *abcix.Request, res *abcix.Response) {
	if mem.isRecheckCursorNil() {
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
func (mem *basemempool) reqResCb(
	tx []byte,
	peerID uint16,
	peerP2PID p2p.ID,
	externalCb func(*abcix.Response),
) func(res *abcix.Response) {
	return func(res *abcix.Response) {
		if !mem.isRecheckCursorNil() {
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

func (mem *basemempool) isFull(txSize int) error {
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
func (mem *basemempool) resCbFirstTime(
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
			atomic.AddInt64(&mem.txsBytes, int64(len(memTx.tx)))
			mem.metrics.TxSizeBytes.Observe(float64(len(memTx.tx)))
			mem.logger.Info("Added good transaction",
				"tx", txID(tx),
				"res", r,
				"height", memTx.height,
				"total", mem.Size(),
			)
			mem.notifyTxsAvailable()
			mem.notifyReactor(memTx)
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
func (mem *basemempool) resCbRecheck(req *abcix.Request, res *abcix.Response) {
	switch r := res.Value.(type) {
	case *abcix.Response_CheckTx:
		tx := req.GetCheckTx().Tx
		memTx := mem.getRecheckCursorTx()
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
			elemRemoved := mem.removeTx(tx)
			if !elemRemoved {
				// TODO: better error handling
				panic("recheck cursor not removed by tx key: " + memTx.tx.String())
			}
			atomic.AddInt64(&mem.txsBytes, int64(-len(tx)))
			mem.cache.Remove(tx)
		}
		mem.updateRecheckCursor()
		if mem.isRecheckCursorNil() {
			// Done!
			mem.logger.Info("Done rechecking txs")

			// in case the recheck removed all txs
			if mem.Size() > 0 {
				mem.notifyTxsAvailable()
			}
		}
	default:
		// ignore other messages
	}
}

// Safe for concurrent use by multiple goroutines.
func (mem *basemempool) TxsAvailable() <-chan struct{} {
	return mem.txsAvailable
}

func (mem *basemempool) notifyTxsAvailable() {
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

func (mem *basemempool) ReapMaxTxs(max int) types.Txs {
	mem.updateMtx.RLock()
	defer mem.updateMtx.RUnlock()

	return mem.reapMaxTxs(max)
}

// Lock() must be held by the caller during execution.
func (mem *basemempool) Update(
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
		if elemRemoved := mem.removeTx(tx); elemRemoved {
			atomic.AddInt64(&mem.txsBytes, int64(-len(tx)))
		}
	}

	// Either recheck non-committed txs to see if they became invalid
	// or just notify there're some txs left.
	if mem.Size() > 0 {
		if mem.config.Recheck {
			mem.logger.Info("Recheck txs", "numtxs", mem.Size(), "height", height)
			mem.recheckTxs(mem.proxyAppConn)
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

// Lock() must be held by the caller during execution.
func (mem *basemempool) RemoveTxs(txs types.Txs) error {
	for _, tx := range txs {
		elemRemoved := mem.removeTx(tx)
		if !elemRemoved {
			return errors.New("fail to load and delete tx from mempool")
		}
		atomic.AddInt64(&mem.txsBytes, int64(-len(tx)))
		mem.cache.Remove(tx)
	}
	return nil
}

// Used by reactor to iterate all tx + new tx for broadcasting, and
// sending nil tx would terminate the listening side
func (mem *basemempool) subscribe() reactorSub {
	ret := reactorSub{
		txCh: make(chan *mempoolTx, 100),
		quit: make(chan struct{}),
	}
	go func() {
		var tx *mempoolTx
		for {
			tx := mem.nextTx(tx)
			if tx == nil {
				return
			}
			select {
			case ret.txCh <- tx:
			case <-ret.quit:
				return
			}
		}
	}()
	mem.reactorSubMtx.Lock()
	defer mem.reactorSubMtx.Unlock()
	mem.reactorSubs = append(mem.reactorSubs, ret)
	return ret
}

func (mem *basemempool) notifyReactor(tx *mempoolTx) {
	mem.reactorSubMtx.Lock()
	defer mem.reactorSubMtx.Unlock()
	// Also garbage collect closed subscriptions
	newSubs := make([]reactorSub, 0, len(mem.reactorSubs))
	for _, sub := range mem.reactorSubs {
		select {
		case sub.txCh <- tx:
			newSubs = append(newSubs, sub)
		case <-sub.quit:
		}
	}
	mem.reactorSubs = newSubs
}

//--------------------------------------------------------------------------------

// mempoolTx is a transaction that successfully ran
type mempoolTx struct {
	height    int64    // height that this tx had been validated in
	gasWanted int64    // amount of gas this tx states it will require
	tx        types.Tx //

	// ids of peers who've sent us this tx (as a map for quick lookups).
	// senders: PeerID -> bool
	senders sync.Map
}

// Height returns the height for this transaction
func (memTx *mempoolTx) Height() int64 {
	return atomic.LoadInt64(&memTx.height)
}

//--------------------------------------------------------------------------------

type txCache interface {
	Reset()
	Push(tx types.Tx) bool
	Remove(tx types.Tx)
}

// mapTxCache maintains a LRU cache of transactions. This only stores the hash
// of the tx, due to memory concerns.
type mapTxCache struct {
	mtx      sync.Mutex
	size     int
	cacheMap map[[TxKeySize]byte]*list.Element
	list     *list.List
}

var _ txCache = (*mapTxCache)(nil)

// newMapTxCache returns a new mapTxCache.
func newMapTxCache(cacheSize int) *mapTxCache {
	return &mapTxCache{
		size:     cacheSize,
		cacheMap: make(map[[TxKeySize]byte]*list.Element, cacheSize),
		list:     list.New(),
	}
}

// Reset resets the cache to an empty state.
func (cache *mapTxCache) Reset() {
	cache.mtx.Lock()
	cache.cacheMap = make(map[[TxKeySize]byte]*list.Element, cache.size)
	cache.list.Init()
	cache.mtx.Unlock()
}

// Push adds the given tx to the cache and returns true. It returns
// false if tx is already in the cache.
func (cache *mapTxCache) Push(tx types.Tx) bool {
	cache.mtx.Lock()
	defer cache.mtx.Unlock()

	// Use the tx hash in the cache
	txHash := TxKey(tx)
	if moved, exists := cache.cacheMap[txHash]; exists {
		cache.list.MoveToBack(moved)
		return false
	}

	if cache.list.Len() >= cache.size {
		popped := cache.list.Front()
		if popped != nil {
			poppedTxHash := popped.Value.([TxKeySize]byte)
			delete(cache.cacheMap, poppedTxHash)
			cache.list.Remove(popped)
		}
	}
	e := cache.list.PushBack(txHash)
	cache.cacheMap[txHash] = e
	return true
}

// Remove removes the given tx from the cache.
func (cache *mapTxCache) Remove(tx types.Tx) {
	cache.mtx.Lock()
	txHash := TxKey(tx)
	popped := cache.cacheMap[txHash]
	delete(cache.cacheMap, txHash)
	if popped != nil {
		cache.list.Remove(popped)
	}

	cache.mtx.Unlock()
}

type nopTxCache struct{}

var _ txCache = (*nopTxCache)(nil)

func (nopTxCache) Reset()             {}
func (nopTxCache) Push(types.Tx) bool { return true }
func (nopTxCache) Remove(types.Tx)    {}

//--------------------------------------------------------------------------------

// TxKey is the fixed length array hash used as the key in maps.
func TxKey(tx types.Tx) [TxKeySize]byte {
	return sha256.Sum256(tx)
}

// txID is the hex encoded hash of the bytes as a types.Tx.
func txID(tx []byte) string {
	return fmt.Sprintf("%X", types.Tx(tx).Hash())
}
