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
//BenchmarkClistCheckTx-8                             3673            318504 ns/op
//BenchmarkLLRBCheckTx-8                              3454            329866 ns/op
//BenchmarkBTreeCheckTx-8                             3525            330461 ns/op
//BenchmarkClistRemoveTx-8                          774331              1530 ns/op
//BenchmarkLLRBRemoveTx-8                           767287              1564 ns/op
//BenchmarkBTreeRemoveTx-8                          774445              1552 ns/op
//BenchmarkCacheInsertTime-8                       1607788               736 ns/op
//BenchmarkCacheRemoveTime-8                       2453636               579 ns/op

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
	mempool, cleanup := newMempoolWithAppAndConfig(cc, cfg.ResetTestRoot(fmt.Sprintf("mempool_test_%d", enum)), enum)
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
	mempool, cleanup := newMempoolWithAppAndConfig(cc, cfg.ResetTestRoot(fmt.Sprintf("mempool_test_%d", enum)), enum)
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

// txSize = 100
//BenchmarkClistMempoolGetNextTxBytes-8               5145            215172 ns/op
//BenchmarkLLRBMempoolGetNextTxBytes-8               15721             75926 ns/op
//BenchmarkLLRBMempoolIterNextTxBytes-8              14548             84590 ns/op
//BenchmarkBTreeMempoolGetNextTxBytes-8               8058            135202 ns/op
// txSize = 500
//BenchmarkClistMempoolGetNextTxBytes-8                238           4598056 ns/op
//BenchmarkLLRBMempoolGetNextTxBytes-8                2491            427522 ns/op
//BenchmarkLLRBMempoolIterNextTxBytes-8               2828            410575 ns/op
//BenchmarkBTreeMempoolGetNextTxBytes-8               1534            682226 ns/op
// txSize = 1000
//BenchmarkClistMempoolGetNextTxBytes-8                 62          19520278 ns/op
//BenchmarkLLRBMempoolGetNextTxBytes-8                1060           1218037 ns/op
//BenchmarkLLRBMempoolIterNextTxBytes-8               1296            858727 ns/op
//BenchmarkBTreeMempoolGetNextTxBytes-8                730           1443280 ns/op
// txSize = 5000
//BenchmarkClistMempoolGetNextTxBytes-8                  3         396747638 ns/op
//BenchmarkLLRBMempoolGetNextTxBytes-8                 225           4939424 ns/op
//BenchmarkLLRBMempoolIterNextTxBytes-8                192           6514984 ns/op
//BenchmarkBTreeMempoolGetNextTxBytes-8                126           9231797 ns/op
//txSize = 5000 again
//BenchmarkClistMempoolGetNextTxBytes-8                  3         399225165 ns/op
//BenchmarkLLRBMempoolGetNextTxBytes-8                 208           5537551 ns/op
//BenchmarkLLRBMempoolIterNextTxBytes-8                252           5572525 ns/op
//BenchmarkBTreeMempoolGetNextTxBytes-8                130           8967672 ns/op
// txSize = 7500
//BenchmarkClistMempoolGetNextTxBytes-8                  1        1183883588 ns/op
//BenchmarkLLRBMempoolGetNextTxBytes-8                 104          11753040 ns/op
//BenchmarkLLRBMempoolIterNextTxBytes-8                118           8695232 ns/op
//BenchmarkBTreeMempoolGetNextTxBytes-8                 85          13243029 ns/op
// txSize = 10000
//BenchmarkClistMempoolGetNextTxBytes-8                  1        1579712404 ns/op
//BenchmarkLLRBMempoolGetNextTxBytes-8                  96          12655440 ns/op
//BenchmarkLLRBMempoolIterNextTxBytes-8                102          11744083 ns/op
//BenchmarkBTreeMempoolGetNextTxBytes-8                 62          18232574 ns/op
// txSize = 15000
//BenchmarkClistMempoolGetNextTxBytes-8                  1        4879638275 ns/op
//BenchmarkLLRBMempoolGetNextTxBytes-8                  50          32104254 ns/op
//BenchmarkLLRBMempoolIterNextTxBytes-8                 62          21118181 ns/op
//BenchmarkBTreeMempoolGetNextTxBytes-8                 34          30635612 ns/op

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
	mempool, cleanup := newMempoolWithAppAndConfig(cc, cfg.ResetTestRoot(fmt.Sprintf("mempool_test_%d", enum)), enum)
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
