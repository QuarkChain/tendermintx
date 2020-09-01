package mempool

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/tendermint/tendermint/abcix/example/kvstore"
	cfg "github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/proxy"
	"github.com/tendermint/tendermint/types"
)

var txs types.Txs

func initTxs(size int) {
	txs = types.Txs{}
	for i := 0; i < size; i++ {
		txBytes := make([]byte, 20)
		priority := strconv.FormatInt(int64(i)%100, 10)
		tx := "k" + strconv.Itoa(i) + "=v" + strconv.Itoa(i) + ","
		extra := 20 - len(tx) - len(priority) - 1
		tx = tx + strings.Repeat("f", extra) + "," + priority
		copy(txBytes, tx)
		txs = append(txs, txBytes)
	}
}

// BenchmarkClistCheckTx-8   	   17149	     71564 ns/op
func BenchmarkClistCheckTx(b *testing.B) {
	benchmarkCheckTx(b, enumclistmempool)
}

// BenchmarkLLRBCheckTx-8   	   17014	     70137 ns/op
func BenchmarkLLRBCheckTx(b *testing.B) {
	benchmarkCheckTx(b, enumllrbmempool)
}

// BenchmarkBTreeCheckTx-8   	   17071	     71053 ns/op
func BenchmarkBTreeCheckTx(b *testing.B) {
	benchmarkCheckTx(b, enumbtreemempool)
}

func benchmarkCheckTx(b *testing.B, enum mpEnum) {
	app := kvstore.NewApplication()
	cc := proxy.NewLocalClientCreator(app)
	mempool, cleanup := newMempoolWithAppAndConfig(cc, cfg.ResetTestRoot(fmt.Sprintf("mempool_test_%d", enum)), enum)
	defer cleanup()
	size := 100
	initTxs(size)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < size; j++ {
			mempool.CheckTx(txs[j], nil, TxInfo{})
		}
	}
	if mempool.Size() != size {
		b.Fatal("wrong checkTx size")
	}
}

// BenchmarkClistRemoveTx-8   	  770739	      1528 ns/op
func BenchmarkClistRemoveTx(b *testing.B) {
	benchmarkRemoveTx(b, enumclistmempool)
}

// BenchmarkLLRBRemoveTx-8   	  767186	      1542 ns/op
func BenchmarkLLRBRemoveTx(b *testing.B) {
	benchmarkRemoveTx(b, enumllrbmempool)
}

// BenchmarkBTreeRemoveTx-8   	  759945	      1597 ns/op
func BenchmarkBTreeRemoveTx(b *testing.B) {
	benchmarkRemoveTx(b, enumbtreemempool)
}

func benchmarkRemoveTx(b *testing.B, enum mpEnum) {
	app := kvstore.NewApplication()
	cc := proxy.NewLocalClientCreator(app)
	mempool, cleanup := newMempoolWithAppAndConfig(cc, cfg.ResetTestRoot(fmt.Sprintf("mempool_test_%d", enum)), enum)
	defer cleanup()
	size := 100000
	initTxs(size)
	for j := 0; j < size; j++ {
		mempool.CheckTx(txs[j], nil, TxInfo{})
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

// BenchmarkClistMempoolGetNextTxBytes-8   	      56	  18551073 ns/op
func BenchmarkClistMempoolGetNextTxBytes(b *testing.B) {
	benchmarkMempoolGetNextTxBytes(b, enumclistmempool)
}

// BenchmarkLLRBMempoolGetNextTxBytes-8   	    1030	    980720 ns/op
func BenchmarkLLRBMempoolGetNextTxBytes(b *testing.B) {
	benchmarkMempoolGetNextTxBytes(b, enumllrbmempool)
}

// BenchmarkBTreeMempoolGetNextTxBytes-8   	     730	   1826076 ns/op
func BenchmarkBTreeMempoolGetNextTxBytes(b *testing.B) {
	benchmarkMempoolGetNextTxBytes(b, enumbtreemempool)
}

func benchmarkMempoolGetNextTxBytes(b *testing.B, enum mpEnum) {
	app := kvstore.NewApplication()
	cc := proxy.NewLocalClientCreator(app)
	mempool, cleanup := newMempoolWithAppAndConfig(cc, cfg.ResetTestRoot(fmt.Sprintf("mempool_test_%d", enum)), enum)
	defer cleanup()
	size := 1000
	initTxs(size)
	for i := 0; i < size; i++ {
		mempool.CheckTx(txs[i], nil, TxInfo{})
	}
	if mempool.Size() != size {
		b.Fatal("wrong checkTx size")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		starter, _ := mempool.GetNextTxBytes(1000, 1, nil)
		for starter != nil {
			next, _ := mempool.GetNextTxBytes(1000, 1, starter)
			if getPriority(next) > getPriority(starter) {
				b.Fatal("invalid iteration")
			}
			starter = next
		}
	}
}

func getPriority(txBytes []byte) int64 {
	txSlice := strings.Split(string(txBytes), ",")
	value, _ := strconv.ParseInt(txSlice[len(txSlice)-1], 10, 64)
	return value
}
