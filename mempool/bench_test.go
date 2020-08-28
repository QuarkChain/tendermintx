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

// BenchmarkClistCheckTx-8   	     768	   1510694 ns/op
func BenchmarkClistCheckTx(b *testing.B) {
	benchmarkCheckTx(b, enumclistmempool)
}

// BenchmarkLLRBCheckTx-8   	     646	   1573239 ns/op
func BenchmarkLLRBCheckTx(b *testing.B) {
	benchmarkCheckTx(b, enumllrbmempool)
}

func benchmarkCheckTx(b *testing.B, enum mpEnum) {
	app := kvstore.NewApplication()
	cc := proxy.NewLocalClientCreator(app)
	mempool, cleanup := newMempoolWithAppAndConfig(cc, cfg.ResetTestRoot(fmt.Sprintf("mempool_test_%d", enum)), enum)
	defer cleanup()
	size := 100
	txs := types.Txs{}
	for i := 0; i < size; i++ {
		txBytes := make([]byte, 20)
		priority := strconv.FormatInt(int64(i)%100, 10)
		tx := "k" + strconv.Itoa(i) + "=v" + strconv.Itoa(i) + ","
		extra := 20 - len(tx) - len(priority) - 1
		tx = tx + strings.Repeat("f", extra) + "," + priority
		copy(txBytes, tx)
		txs = append(txs, txBytes)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < size; j++ {
			mempool.CheckTx(txs[j], nil, TxInfo{})
		}
		b.StopTimer()
		mempool.Flush()
		b.StartTimer()
	}
}

// BenchmarkClistRemoveTx-8   	  731764	      1619 ns/op
func BenchmarkClistRemoveTx(b *testing.B) {
	benchmarkRemoveTx(b, enumclistmempool)
}

// BenchmarkLLRBRemoveTx-8   	  726847	      1642 ns/op
func BenchmarkLLRBRemoveTx(b *testing.B) {
	benchmarkRemoveTx(b, enumllrbmempool)
}

func benchmarkRemoveTx(b *testing.B, enum mpEnum) {
	app := kvstore.NewApplication()
	cc := proxy.NewLocalClientCreator(app)
	mempool, cleanup := newMempoolWithAppAndConfig(cc, cfg.ResetTestRoot(fmt.Sprintf("mempool_test_%d", enum)), enum)
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

// BenchmarkClistMempoolGetNextTxBytes-8   	       2	 604934776 ns/op
func BenchmarkClistMempoolGetNextTxBytes(b *testing.B) {
	benchmarkMempoolGetNextTxBytes(b, enumclistmempool)
}

// BenchmarkLLRBMempoolGetNextTxBytes-8   	     183	   5956796 ns/op
func BenchmarkLLRBMempoolGetNextTxBytes(b *testing.B) {
	benchmarkMempoolGetNextTxBytes(b, enumllrbmempool)
}

func benchmarkMempoolGetNextTxBytes(b *testing.B, enum mpEnum) {
	app := kvstore.NewApplication()
	cc := proxy.NewLocalClientCreator(app)
	mempool, cleanup := newMempoolWithAppAndConfig(cc, cfg.ResetTestRoot(fmt.Sprintf("mempool_test_%d", enum)), enum)
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
			next, _ := mempool.GetNextTxBytes(1000, 1, starter)
			if getPriority(next) > getPriority(starter) {
				b.Error("invalid iteration")
				return
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
