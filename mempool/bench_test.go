package mempool

import (
	"encoding/binary"
	"testing"

	cfg "github.com/tendermint/tendermint/config"

	"github.com/tendermint/tendermint/abci/example/kvstore"
	"github.com/tendermint/tendermint/proxy"
)

func BenchmarkReap(b *testing.B) {
	app := kvstore.NewApplication()
	cc := proxy.NewLegacyLocalClientCreator(app)
	for i := 0; i < 2; i++ {
		mempool, cleanup := newLegacyMempoolWithAppAndConfig(cc, cfg.ResetTestRoot("mempool_test"), i)
		defer cleanup()

		size := 10000
		for j := 0; j < size; j++ {
			tx := make([]byte, 8)
			binary.BigEndian.PutUint64(tx, uint64(i))
			mempool.CheckTx(tx, nil, TxInfo{})
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			mempool.ReapMaxBytesMaxGas(100000000, 10000000)
		}
	}
}

func BenchmarkCheckTx(b *testing.B) {
	app := kvstore.NewApplication()
	cc := proxy.NewLegacyLocalClientCreator(app)
	for i := 0; i < 2; i++ {
		mempool, cleanup := newLegacyMempoolWithAppAndConfig(cc, cfg.ResetTestRoot("mempool_test"), i)
		defer cleanup()

		for i := 0; i < b.N; i++ {
			tx := make([]byte, 8)
			binary.BigEndian.PutUint64(tx, uint64(i))
			mempool.CheckTx(tx, nil, TxInfo{})
		}
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
