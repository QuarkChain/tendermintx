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

//goos: darwin
//goarch: amd64
//pkg: github.com/tendermint/tendermint/mempool
//BenchmarkClistCheckTx-8                             3825            296734 ns/op
//BenchmarkLLRBCheckTx-8                              3690            320052 ns/op
//BenchmarkBTreeCheckTx-8                             3278            320964 ns/op
//BenchmarkClistRemoveTx-8                          824952              1461 ns/op
//BenchmarkLLRBRemoveTx-8                           801897              1463 ns/op
//BenchmarkBTreeRemoveTx-8                          842322              1422 ns/op
//BenchmarkCacheInsertTime-8                       1657232               705 ns/op
//BenchmarkCacheRemoveTime-8                       2419852               508 ns/op
//BenchmarkClistMempoolGetNextTxBytes-8                  3         441360573 ns/op
//BenchmarkLLRBMempoolGetNextTxBytes-8                 194           5452566 ns/op
//BenchmarkBTreeMempoolGetNextTxBytes-8                130           8596139 ns/op

var txs types.Txs

const TXSIZE = 5000

func init() {
	txs = types.Txs{}
	for i := 0; i < TXSIZE; i++ {
		txBytes := make([]byte, 20)
		priority := strconv.FormatInt(int64(i)%100, 10)
		tx := "k" + strconv.Itoa(i) + "=v" + strconv.Itoa(i) + ","
		extra := 20 - len(tx) - len(priority) - 1
		tx = tx + strings.Repeat("f", extra) + "," + priority
		copy(txBytes, tx)
		txs = append(txs, txBytes)
	}
}

func BenchmarkClistCheckTx(b *testing.B) {
	benchmarkCheckTx(b, enumclistmempool)
}

func BenchmarkLLRBCheckTx(b *testing.B) {
	benchmarkCheckTx(b, enumllrbmempool)
}

func BenchmarkBTreeCheckTx(b *testing.B) {
	benchmarkCheckTx(b, enumbtreemempool)
}

func benchmarkCheckTx(b *testing.B, enum mpEnum) {
	app := kvstore.NewApplication()
	cc := proxy.NewLocalClientCreator(app)
	mempool, cleanup := newMempoolWithAppAndConfig(cc, cfg.ResetTestRoot(fmt.Sprintf("mempool_test_%d", enum)), enum)
	defer cleanup()
	size := 5000

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < size; j++ {
			mempool.CheckTx(txs[j], nil, TxInfo{})
		}
	}
	if mempool.Size() != size {
		b.Fatal("wrong transaction size")
	}
}

func BenchmarkClistRemoveTx(b *testing.B) {
	benchmarkRemoveTx(b, enumclistmempool)
}

func BenchmarkLLRBRemoveTx(b *testing.B) {
	benchmarkRemoveTx(b, enumllrbmempool)
}

func BenchmarkBTreeRemoveTx(b *testing.B) {
	benchmarkRemoveTx(b, enumbtreemempool)
}

func benchmarkRemoveTx(b *testing.B, enum mpEnum) {
	app := kvstore.NewApplication()
	cc := proxy.NewLocalClientCreator(app)
	mempool, cleanup := newMempoolWithAppAndConfig(cc, cfg.ResetTestRoot(fmt.Sprintf("mempool_test_%d", enum)), enum)
	defer cleanup()
	for j := 0; j < TXSIZE; j++ {
		mempool.CheckTx(txs[j], nil, TxInfo{})
	}
	if mempool.Size() != TXSIZE {
		b.Fatal("wrong transaction size")
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

func BenchmarkClistMempoolGetNextTxBytes(b *testing.B) {
	benchmarkMempoolGetNextTxBytes(b, enumclistmempool)
}

func BenchmarkLLRBMempoolGetNextTxBytes(b *testing.B) {
	benchmarkMempoolGetNextTxBytes(b, enumllrbmempool)
}

func BenchmarkBTreeMempoolGetNextTxBytes(b *testing.B) {
	benchmarkMempoolGetNextTxBytes(b, enumbtreemempool)
}

func benchmarkMempoolGetNextTxBytes(b *testing.B, enum mpEnum) {
	app := kvstore.NewApplication()
	cc := proxy.NewLocalClientCreator(app)
	mempool, cleanup := newMempoolWithAppAndConfig(cc, cfg.ResetTestRoot(fmt.Sprintf("mempool_test_%d", enum)), enum)
	defer cleanup()
	for i := 0; i < TXSIZE; i++ {
		mempool.CheckTx(txs[i], nil, TxInfo{})
	}
	if mempool.Size() != TXSIZE {
		b.Fatal("wrong transaction size")
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
