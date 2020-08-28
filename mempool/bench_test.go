package mempool

import (
	"crypto/rand"
	"encoding/binary"
	"math/big"
	"strconv"
	"testing"

	"github.com/tendermint/tendermint/abci/example/kvstore"
	cfg "github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/proxy"
	"github.com/tendermint/tendermint/types"
)

// BenchmarkClistCheckTx-8   	10842198	        94.9 ns/op
func BenchmarkClistCheckTx(b *testing.B) {
	app := kvstore.NewApplication()
	cc := proxy.NewLegacyLocalClientCreator(app)
	mempool, cleanup := newLegacyMempoolWithAppAndConfig(cc, cfg.ResetTestRoot("mempool_test"), enumclistmempool)
	defer cleanup()
	for i := 0; i < b.N; i++ {
		tx := make([]byte, 8)
		binary.BigEndian.PutUint64(tx, uint64(i))
		mempool.CheckTx(tx, nil, TxInfo{})
	}
}

// BenchmarkLLRBCheckTx-8   	10889796	        99.2 ns/op
func BenchmarkLLRBCheckTx(b *testing.B) {
	app := kvstore.NewApplication()
	cc := proxy.NewLegacyLocalClientCreator(app)
	mempool, cleanup := newLegacyMempoolWithAppAndConfig(cc, cfg.ResetTestRoot("mempool_test"), enumllrbmempool)
	defer cleanup()
	for i := 0; i < b.N; i++ {
		tx := make([]byte, 8)
		binary.BigEndian.PutUint64(tx, uint64(i))
		mempool.CheckTx(tx, nil, TxInfo{})
	}
}

// BenchmarkClistRemoveTx-8   	  749619	      1625 ns/op
func BenchmarkClistRemoveTx(b *testing.B) {
	benchmarkRemoveTx(b, enumclistmempool)
}

// BenchmarkLLRBRemoveTx-8   	  724825	      1609 ns/op
func BenchmarkLLRBRemoveTx(b *testing.B) {
	benchmarkRemoveTx(b, enumllrbmempool)
}

func benchmarkRemoveTx(b *testing.B, enum mpEnum) {
	app := kvstore.NewApplication()
	cc := proxy.NewLegacyLocalClientCreator(app)
	mempool, cleanup := newLegacyMempoolWithAppAndConfig(cc, cfg.ResetTestRoot("mempool_test"), enum)
	defer cleanup()
	size := 100000
	txs := types.Txs{}
	for j := 0; j < size; j++ {
		rand, _ := rand.Int(rand.Reader, big.NewInt(100))
		tx := "k" + strconv.Itoa(j) + "=v" + strconv.Itoa(j) + ",f," + strconv.FormatInt(rand.Int64(), 10)
		txBytes := []byte(tx)
		txs = append(txs, txBytes)
		mempool.CheckTx(txBytes, nil, TxInfo{})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		testmempool := mempool
		testmempool.RemoveTxs(txs)
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

// BenchmarkClistMempoolGetNextTxBytes-8   	       2	 793475198 ns/op
func BenchmarkClistMempoolGetNextTxBytes(b *testing.B) {
	benchmarkMempoolGetNextTxBytes(b, enumclistmempool)
}

// BenchmarkLLRBMempoolGetNextTxBytes-8   	     322	   3565640 ns/op
func BenchmarkLLRBMempoolGetNextTxBytes(b *testing.B) {
	benchmarkMempoolGetNextTxBytes(b, enumllrbmempool)
}

func benchmarkMempoolGetNextTxBytes(b *testing.B, enum mpEnum) {
	app := kvstore.NewApplication()
	cc := proxy.NewLegacyLocalClientCreator(app)
	mempool, cleanup := newLegacyMempoolWithAppAndConfig(cc, cfg.ResetTestRoot("mempool_test"), enum)
	defer cleanup()
	size := 100000
	for i := 0; i < size; i++ {
		rand, _ := rand.Int(rand.Reader, big.NewInt(100))
		tx := "k" + strconv.Itoa(i) + "=v" + strconv.Itoa(i) + ",f," + strconv.FormatInt(rand.Int64(), 10)
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
