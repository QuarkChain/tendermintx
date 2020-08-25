package mempool

import (
	"encoding/binary"
	"math/rand"
	"strconv"
	"testing"

	cfg "github.com/tendermint/tendermint/config"

	"github.com/tendermint/tendermint/abci/example/kvstore"
	"github.com/tendermint/tendermint/proxy"
)

func BenchmarkClistCheckTx(b *testing.B) {
	app := kvstore.NewApplication()
	cc := proxy.NewLegacyLocalClientCreator(app)
	mempool, cleanup := newLegacyMempoolWithAppAndConfig(cc, cfg.ResetTestRoot("mempool_test"), 0)
	defer cleanup()

	for i := 0; i < b.N; i++ {
		tx := make([]byte, 8)
		binary.BigEndian.PutUint64(tx, uint64(i))
		mempool.CheckTx(tx, nil, TxInfo{})
	}
}

// BenchmarkLlrbCheckTx-8   	 9360813	       114 ns/op
func BenchmarkLlrbCheckTx(b *testing.B) {
	app := kvstore.NewApplication()
	cc := proxy.NewLegacyLocalClientCreator(app)
	mempool, cleanup := newLegacyMempoolWithAppAndConfig(cc, cfg.ResetTestRoot("mempool_test"), 1)
	defer cleanup()

	for i := 0; i < b.N; i++ {
		tx := make([]byte, 8)
		binary.BigEndian.PutUint64(tx, uint64(i))
		mempool.CheckTx(tx, nil, TxInfo{})
	}
}

func BenchmarkCacheInsertTime(b *testing.B) {
	cache := newMapTxCache(b.N)
	txs := make([][]byte, b.N)
	for i := 0; i < b.N; i++ {
		txs[i] = make([]byte, 8)
		binary.BigEndian.PutUint64(txs[i], uint64(i))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Push(txs[i])
	}
}

// This benchmark is probably skewed, since we actually will be removing
// txs in parallel, which may cause some overhead due to mutex locking.
func BenchmarkCacheRemoveTime(b *testing.B) {
	cache := newMapTxCache(b.N)
	txs := make([][]byte, b.N)
	for i := 0; i < b.N; i++ {
		txs[i] = make([]byte, 8)
		binary.BigEndian.PutUint64(txs[i], uint64(i))
		cache.Push(txs[i])
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Remove(txs[i])
	}
}

// BenchmarkCListMempool_GetNextTxBytes-8   	       1	1099547205 ns/op
func BenchmarkCListMempool_GetNextTxBytes(b *testing.B) {
	app := kvstore.NewApplication()
	cc := proxy.NewLegacyLocalClientCreator(app)
	mempool, cleanup := newLegacyMempoolWithAppAndConfig(cc, cfg.ResetTestRoot("mempool_test"), 0)
	defer cleanup()
	size := 100000
	for i := 0; i < size; i++ {
		tx := "k" + strconv.Itoa(i) + "=v" + strconv.Itoa(i) + ",f," + strconv.FormatInt(int64(rand.Int())%100, 10)
		txBytes := []byte(tx)
		mempool.CheckTx(txBytes, nil, TxInfo{})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		starter, _ := mempool.GetNextTxBytes(1000, 1, nil)
		for starter != nil {
			starter, _ = mempool.GetNextTxBytes(1000, 1, starter)
		}
	}
}

// BenchmarkLlrbMempool_GetNextTxBytes-8   	     250	   5048917 ns/op
func BenchmarkLlrbMempool_GetNextTxBytes(b *testing.B) {
	app := kvstore.NewApplication()
	cc := proxy.NewLegacyLocalClientCreator(app)
	mempool, cleanup := newLegacyMempoolWithAppAndConfig(cc, cfg.ResetTestRoot("mempool_test"), 1)
	defer cleanup()
	size := 100000
	for i := 0; i < size; i++ {
		tx := "k" + strconv.Itoa(i) + "=v" + strconv.Itoa(i) + ",f," + strconv.FormatInt(int64(rand.Int())%100, 10)
		txBytes := []byte(tx)
		mempool.CheckTx(txBytes, nil, TxInfo{})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		starter, _ := mempool.GetNextTxBytes(1000, 1, nil)
		for starter != nil {
			starter, _ = mempool.GetNextTxBytes(1000, 1, starter)
		}
	}
}
