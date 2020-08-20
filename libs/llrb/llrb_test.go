package llrb

import (
	"bytes"
	cr "crypto/rand"
	"math"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func getNodeKeys(priorities []uint64) []*NodeKey {
	var nks []*NodeKey
	for i := 0; i < len(priorities); i++ {
		nk := &NodeKey{
			Priority: priorities[i],
			TS:       time.Now(),
		}
		nks = append(nks, nk)
	}
	return nks
}

func getRandomBytes(count int) [][]byte {
	var txs [][]byte
	for i := 0; i < count; i++ {
		tx := make([]byte, 20)
		cr.Read(tx)
		txs = append(txs, tx)
	}
	return txs
}

func getOrderedTxs(tree LLRB, txMap *sync.Map) [][]byte {
	var starter *NodeKey
	var txs [][]byte
	for {
		result, err := tree.GetNext(starter, nil)
		if err != nil {
			break
		}
		txs = append(txs, result.([]byte))
		v, _ := txMap.Load(string(result.([]byte)))
		starter = v.(*NodeKey)
	}
	return txs
}

func TestBasics(t *testing.T) {
	tree := New()
	nks := getNodeKeys([]uint64{1, 2})
	txs := getRandomBytes(2)
	tree.Insert(*nks[0], txs[0])
	require.Equal(t, 1, tree.Size(), "expecting len 1")
	data, err := tree.Remove(*nks[0])
	require.NoError(t, err, "expect not error when removing existed node")
	require.Equal(t, 0, tree.Size(), "expecting len 0")
	require.Equal(t, txs[0], data.([]byte), "expecting same data")
	_, err = tree.Remove(*nks[1])
	require.Error(t, err, "expect error when removing nonexistent node")

}

func TestRandomInsertSequenceDelete(t *testing.T) {
	tree := New()
	n := 10
	txs := getRandomBytes(n)
	perm := rand.Perm(n)
	var nks []*NodeKey
	for i := 0; i < n; i++ {
		nk := &NodeKey{Priority: uint64(perm[i])}
		nks = append(nks, nk)
		tree.Insert(*nk, txs[perm[i]])
	}
	for i := 0; i < n; i++ {
		removed, err := tree.Remove(*nks[i])
		require.NoError(t, err, "expect not error when removing existed node")
		require.True(t, bytes.Equal(removed.([]byte), txs[perm[i]]), "expecting same data")
	}
}

func TestRandomInsertDeleteNonExistent(t *testing.T) {
	tree := New()
	n := 100
	txs := getRandomBytes(n)
	perm := rand.Perm(n)
	var nks []*NodeKey
	for i := 0; i < n; i++ {
		nk := &NodeKey{Priority: uint64(perm[i])}
		nks = append(nks, nk)
		tree.Insert(*nk, txs[perm[i]])
	}
	_, err := tree.Remove(*getNodeKeys([]uint64{200})[0])
	require.Error(t, err, "expect error when removing nonexistent node")
	_, err = tree.Remove(*getNodeKeys([]uint64{2000})[0])
	require.Error(t, err, "expect error when removing nonexistent node")

	for i := 0; i < n; i++ {
		result, err := tree.Remove(*nks[i])
		require.NoError(t, err, "expect not error when removing existed node")
		require.True(t, bytes.Equal(result.([]byte), txs[perm[i]]), "expecting same data")
	}
	_, err = tree.Remove(*getNodeKeys([]uint64{200})[0])
	require.Error(t, err, "expect error when removing nonexistent node")
	_, err = tree.Remove(*getNodeKeys([]uint64{2000})[0])
	require.Error(t, err, "expect error when removing nonexistent node")
}

func TestGetNext(t *testing.T) {
	testCases := []struct {
		priorities []uint64
		order      []int64
	}{
		{
			priorities: []uint64{0, 0, 0, 0, 0},
			order:      []int64{0, 1, 2, 3, 4},
		},
		{
			priorities: []uint64{1, 0, 1, 0, 1},
			order:      []int64{0, 3, 1, 4, 2},
		},
		{
			priorities: []uint64{1, 2, 3, 4, 5},
			order:      []int64{4, 3, 2, 1, 0},
		},
		{
			priorities: []uint64{5, 4, 3, 2, 1},
			order:      []int64{0, 1, 2, 3, 4},
		},
		{
			priorities: []uint64{1, 3, 5, 4, 2},
			order:      []int64{4, 2, 0, 1, 3},
		},
		{
			priorities: []uint64{math.MaxUint64, math.MaxUint64, math.MaxUint64, 1},
			order:      []int64{0, 1, 2, 3},
		},
	}

	for i, tc := range testCases {
		tree := New()
		nks := getNodeKeys(tc.priorities)
		txs := getRandomBytes(len(tc.priorities))
		var txsMap sync.Map
		for j := 0; j < len(nks); j++ {
			txsMap.Store(string(txs[j]), nks[j])
			tree.Insert(*nks[j], txs[j])
		}
		ordered := getOrderedTxs(tree, &txsMap)
		for j := 0; j < len(nks); j++ {
			require.True(t, bytes.Equal(txs[j], ordered[tc.order[j]]), "expecting equal bytes at testcase %d txs %d", i, j)
		}
	}
}

func BenchmarkInsert(b *testing.B) {
	tree := new(llrb)
	for i := 0; i < b.N; i++ {
		tree.Insert(NodeKey{Priority: uint64(i)}, getRandomBytes(1)[0])
	}
}

func BenchmarkDelete(b *testing.B) {
	b.StopTimer()
	tree := new(llrb)
	var nks []*NodeKey
	for i := 0; i < b.N; i++ {
		nk := &NodeKey{Priority: uint64(i)}
		nks = append(nks, nk)
	}
	txs := getRandomBytes(b.N)
	for i := 0; i < b.N; i++ {
		tree.Insert(*nks[i], txs[i])
	}
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		tree.Remove(*nks[i])
	}
}
