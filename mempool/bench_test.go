package mempool

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"math/big"
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
//BenchmarkClistCheckTx-8                             3847            304484 ns/op
//BenchmarkLLRBCheckTx-8                              3756            312078 ns/op
//BenchmarkBTreeCheckTx-8                             3610            308219 ns/op
//BenchmarkClistRemoveTx-8                          819738              1502 ns/op
//BenchmarkLLRBRemoveTx-8                           806511              1470 ns/op
//BenchmarkBTreeRemoveTx-8                          803672              1459 ns/op
//BenchmarkCacheInsertTime-8                       1694118               706 ns/op
//BenchmarkCacheRemoveTime-8                       2465884               510 ns/op
//BenchmarkClistMempoolGetNextTxBytes-8                  3         419214434 ns/op
//BenchmarkLLRBMempoolGetNextTxBytes-8                 226           5194137 ns/op
//BenchmarkBTreeMempoolGetNextTxBytes-8                141           8484462 ns/op

var txs types.Txs

const txSize = 5000

func init() {
	txs = types.Txs{}
	for i := 0; i < txSize; i++ {
		txBytes := make([]byte, 20)
		randInt, _ := rand.Int(rand.Reader, big.NewInt(100))
		priority := strconv.FormatInt(randInt.Int64(), 10)
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
	mempool, cleanup := newMempoolWithAppAndConfig(cc, cfg.ResetTestRoot(fmt.Sprintf("mempool_test_%d", enum)), enum, false)
	defer cleanup()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < txSize; j++ {
			mempool.CheckTx(txs[j], nil, TxInfo{})
		}
	}
	if mempool.Size() != txSize {
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
	mempool, cleanup := newMempoolWithAppAndConfig(cc, cfg.ResetTestRoot(fmt.Sprintf("mempool_test_%d", enum)), enum, false)
	defer cleanup()
	for j := 0; j < txSize; j++ {
		mempool.CheckTx(txs[j], nil, TxInfo{})
	}
	if mempool.Size() != txSize {
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
	benchmarkMempoolGetNextTxBytes(b, enumclistmempool, false)
}

func BenchmarkLLRBMempoolGetNextTxBytes(b *testing.B) {
	benchmarkMempoolGetNextTxBytes(b, enumllrbmempool, false)
}

func BenchmarkLLRBMempoolIterNextTxBytes(b *testing.B) {
	benchmarkMempoolGetNextTxBytes(b, enumllrbmempool, true)
}

func BenchmarkBTreeMempoolGetNextTxBytes(b *testing.B) {
	benchmarkMempoolGetNextTxBytes(b, enumbtreemempool, false)
}

func benchmarkMempoolGetNextTxBytes(b *testing.B, enum mpEnum, supportIterable bool) {
	app := kvstore.NewApplication()
	cc := proxy.NewLocalClientCreator(app)
	mempool, cleanup := newMempoolWithAppAndConfig(cc, cfg.ResetTestRoot(fmt.Sprintf("mempool_test_%d", enum)), enum, supportIterable)
	defer cleanup()
	for i := 0; i < txSize; i++ {
		mempool.CheckTx(txs[i], nil, TxInfo{})
	}
	if mempool.Size() != txSize {
		b.Fatal("wrong transaction size")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if supportIterable {
			mempool.Register(0)
			starter, _ := mempool.IterNext(0, 1000, 1, nil)
			for starter != nil {
				next, _ := mempool.IterNext(0, 1000, 1, starter)
				if getPriority(next) > getPriority(starter) {
					b.Fatal("invalid iteration")
				}
				starter = next
			}
		} else {
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
}

func getPriority(txBytes []byte) int64 {
	txSlice := strings.Split(string(txBytes), ",")
	value, _ := strconv.ParseInt(txSlice[len(txSlice)-1], 10, 64)
	return value
}
