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

//goos: darwin
//goarch: amd64
//pkg: github.com/tendermint/tendermint/mempool
//BenchmarkClistCheckTx-8                             3585            318668 ns/op
//BenchmarkLLRBCheckTx-8                              3415            320087 ns/op
//BenchmarkBTreeCheckTx-8                             3588            306634 ns/op
//BenchmarkClistRemoveTx-8                          812334              1522 ns/op
//BenchmarkLLRBRemoveTx-8                           803126              1466 ns/op
//BenchmarkBTreeRemoveTx-8                          823885              1445 ns/op
//BenchmarkCacheInsertTime-8                       1690495               671 ns/op
//BenchmarkCacheRemoveTime-8                       2572720               421 ns/op
//BenchmarkClistMempoolGetNextTxBytes-8                  3         429128491 ns/op
//BenchmarkLLRBMempoolGetNextTxBytes-8                 222           5250141 ns/op
//BenchmarkBTreeMempoolGetNextTxBytes-8                142           9214519 ns/op

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
	initTxs(size)

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
	size := 5000
	initTxs(size)
	for j := 0; j < size; j++ {
		mempool.CheckTx(txs[j], nil, TxInfo{})
	}
	if mempool.Size() != size {
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
	size := 5000
	initTxs(size)
	for i := 0; i < size; i++ {
		mempool.CheckTx(txs[i], nil, TxInfo{})
	}
	if mempool.Size() != size {
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
