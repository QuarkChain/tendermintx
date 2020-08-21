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

func getFixedBytes(bytelen []int) [][]byte {
	var txs [][]byte
	for i := 0; i < len(bytelen); i++ {
		tx := make([]byte, bytelen[i])
		cr.Read(tx)
		txs = append(txs, tx)
	}
	return txs
}

func getOrderedTxs(tree LLRB, byteLimit int, txMap *sync.Map) [][]byte {
	var starter *NodeKey
	var txs [][]byte
	for {
		result, err := tree.GetNext(starter, func(v interface{}) bool { return len(v.([]byte)) <= byteLimit })
		if err != nil {
			break
		}
		txs = append(txs, result.([]byte))
		v, _ := txMap.Load(string(result.([]byte)))
		starter = v.(*NodeKey)
	}
	return txs
}

func iterateOrderedTxs(tree LLRB, limit int, txMap *sync.Map) [][]byte {
	var starter *NodeKey
	var txs [][]byte
	tree.IterInit(starter, func(v interface{}) bool { return len(v.([]byte)) <= limit })
	for ; tree.IterHasNext(); tree.IterNext() {
		result, _ := tree.IterCurr()
		txs = append(txs, result.([]byte))
	}
	return txs
}

func TestBasics(t *testing.T) {
	tree := New()
	nks := getNodeKeys([]uint64{1})
	txs := getRandomBytes(1)
	tree.Insert(*nks[0], txs[0])
	if tree.Size() != 1 {
		t.Errorf("expecting len 1")
	}
	data, err := tree.Remove(*nks[0])
	if err != nil {
		t.Errorf("error when removing element")
	}
	if tree.Size() != 0 {
		t.Errorf("expecting len 0")
	}
	if !bytes.Equal(data.([]byte), txs[0]) {
		t.Errorf("expecting equal bytes")
	}
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
		require.NoError(t, err, "expecting no error when removing existed node")
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
	require.Error(t, err, "expecting error when removing nonexistent node")

	for i := 0; i < n; i++ {
		result, err := tree.Remove(*nks[i])
		require.NoError(t, err, "expecting no error when removing existed node")
		require.True(t, bytes.Equal(result.([]byte), txs[perm[i]]), "expecting same data")
	}
	_, err = tree.Remove(*getNodeKeys([]uint64{200})[0])
	require.Error(t, err, "expecting error when removing nonexistent node")
}

func testGetNext(target func(tree LLRB, limit int, txMap *sync.Map) [][]byte, t *testing.T) {
	testCases := []struct {
		priorities      []uint64 // Priority of each tx
		byteLength      []int    // Byte length of each tx
		byteLimit       int      // Byte limit provided by user
		expectedTxOrder []int    // How original txs ordered in retrieved txs
	}{
		{
			priorities:      []uint64{0, 0, 0, 0, 0},
			expectedTxOrder: []int{0, 1, 2, 3, 4},
		},
		{
			priorities:      []uint64{1, 0, 1, 0, 1},
			expectedTxOrder: []int{0, 2, 4, 1, 3},
		},
		{
			priorities:      []uint64{1, 2, 3, 4, 5},
			expectedTxOrder: []int{4, 3, 2, 1, 0},
		},
		{
			priorities:      []uint64{5, 4, 3, 2, 1},
			expectedTxOrder: []int{0, 1, 2, 3, 4},
		},
		{
			priorities:      []uint64{1, 3, 5, 4, 2},
			expectedTxOrder: []int{2, 3, 1, 4, 0},
		},
		{
			priorities:      []uint64{math.MaxUint64, math.MaxUint64, math.MaxUint64, 1},
			expectedTxOrder: []int{0, 1, 2, 3},
		},
		// Byte limitation test
		{
			priorities:      []uint64{0, 0, 0, 0, 0},
			byteLimit:       1,
			expectedTxOrder: []int{},
		},
		{
			priorities:      []uint64{0, 0, 0, 0, 0},
			byteLength:      []int{1, 2, 3, 4, 5},
			byteLimit:       1,
			expectedTxOrder: []int{0},
		},
		{
			priorities:      []uint64{0, 0, 0, 0, 0},
			byteLength:      []int{1, 2, 3, 4, 5},
			byteLimit:       3,
			expectedTxOrder: []int{0, 1, 2},
		},
		{
			priorities:      []uint64{1, 0, 1, 0, 1},
			byteLength:      []int{1, 2, 3, 4, 5},
			byteLimit:       3,
			expectedTxOrder: []int{0, 2, 1},
		},
		{
			priorities:      []uint64{1, 3, 5, 4, 2},
			byteLength:      []int{1, 3, 5, 4, 2},
			byteLimit:       3,
			expectedTxOrder: []int{1, 4, 0},
		},
	}

	for i, tc := range testCases {
		tree := New()
		nks := getNodeKeys(tc.priorities)
		txs := getRandomBytes(len(tc.priorities))
		limit := 20
		if tc.byteLength != nil {
			txs = getFixedBytes(tc.byteLength)
		}
		if tc.byteLimit != 0 {
			limit = tc.byteLimit
		}
		var txsMap sync.Map
		for j := 0; j < len(nks); j++ {
			txsMap.Store(string(txs[j]), nks[j])
			tree.Insert(*nks[j], txs[j])
		}
		ordered := target(tree, limit, &txsMap)
		require.Equal(t, len(tc.expectedTxOrder), len(ordered), "expecting equal tx count at testcase %d", i)
		for j, k := range tc.expectedTxOrder {
			require.True(t, bytes.Equal(txs[k], ordered[j]), "expecting equal bytes at testcase %d", i)
		}
	}
}

func TestLlrb_GetNext(t *testing.T) {
	testGetNext(getOrderedTxs, t)
}

// Test GetNext usting iterator, cost O(log(1)) for each query
func TestLlrb_IterNext(t *testing.T) {
	testGetNext(iterateOrderedTxs, t)
}

func BenchmarkInsert(b *testing.B) {
	tree := new(llrb)
	for i := 0; i < b.N; i++ {
		tree.Insert(NodeKey{Priority: uint64(i)}, getRandomBytes(1)[0])
	}
}

func BenchmarkRemove(b *testing.B) {
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
