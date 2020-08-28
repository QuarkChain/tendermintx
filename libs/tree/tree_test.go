package tree

import (
	"bytes"
	cr "crypto/rand"
	"crypto/sha256"
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type treeEnum int

const (
	enumllrb treeEnum = iota
	enumbtree
)

func getNodeKeys(priorities []uint64, txs [][]byte) []*NodeKey {
	var nks []*NodeKey
	for i := 0; i < len(priorities); i++ {
		nk := &NodeKey{
			Priority: priorities[i],
			TS:       time.Now(),
			Hash:     txHash(txs[i]),
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

func getFixedBytes(byteLength []int) [][]byte {
	var txs [][]byte
	for i := 0; i < len(byteLength); i++ {
		tx := make([]byte, byteLength[i])
		cr.Read(tx)
		txs = append(txs, tx)
	}
	return txs
}

func getOrderedTxs(tree BalancedTree, byteLimit int) [][]byte {
	var starter *NodeKey
	var txs [][]byte
	for {
		result, next, err := tree.GetNext(starter, func(v interface{}) bool { return len(v.([]byte)) <= byteLimit })
		if err != nil {
			break
		}
		txs = append(txs, result.([]byte))
		starter = &next
	}
	return txs
}

func txHash(tx []byte) [sha256.Size]byte {
	return sha256.Sum256(tx)
}

func treeGen(enum treeEnum) BalancedTree {
	switch enum {
	case enumllrb:
		return NewLLRB()
	case enumbtree:
		return NewBTree()
	}
	return nil
}

func TestLLRBBasics(t *testing.T) {
	testTreeBasics(t, enumllrb)
}

func TestBTreeBasics(t *testing.T) {
	testTreeBasics(t, enumbtree)
}

func testTreeBasics(t *testing.T, enum treeEnum) {
	tree := treeGen(enum)
	txs := getRandomBytes(2)
	nks := getNodeKeys([]uint64{1, 2}, txs)
	tree.Insert(*nks[0], txs[0])
	require.Equal(t, 1, tree.Size(), "expecting len 1")
	data, err := tree.Remove(*nks[0])
	require.NoError(t, err, "expecting no error when removing existed node")
	require.Equal(t, 0, tree.Size(), "expecting len 0")
	require.Equal(t, txs[0], data.([]byte), "expecting same data")
	_, err = tree.Remove(*nks[1])
	require.Error(t, err, "expecting error when removing nonexistent node")
}

func TestLLRBRandomInsertSequenceDelete(t *testing.T) {
	testRandomInsertSequenceDelete(t, enumllrb)
}

func TestBTreeRandomInsertSequenceDelete(t *testing.T) {
	testRandomInsertSequenceDelete(t, enumbtree)
}

func testRandomInsertSequenceDelete(t *testing.T, enum treeEnum) {
	tree := treeGen(enum)
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

func TestLLRBRandomInsertDeleteNonExistent(t *testing.T) {
	testRandomInsertDeleteNonExistent(t, enumllrb)
}

func TestBTreeRandomInsertDeleteNonExistent(t *testing.T) {
	testRandomInsertDeleteNonExistent(t, enumbtree)
}

func testRandomInsertDeleteNonExistent(t *testing.T, enum treeEnum) {
	tree := treeGen(enum)
	n := 100
	txs := getRandomBytes(n)
	perm := rand.Perm(n)
	var nks []*NodeKey
	for i := 0; i < n; i++ {
		nk := &NodeKey{Priority: uint64(perm[i])}
		nks = append(nks, nk)
		tree.Insert(*nk, txs[perm[i]])
	}
	_, err := tree.Remove(*getNodeKeys([]uint64{200}, txs)[0])
	require.Error(t, err, "expecting error when removing nonexistent node")

	for i := 0; i < n; i++ {
		result, err := tree.Remove(*nks[i])
		require.NoError(t, err, "expecting no error when removing existed node")
		require.True(t, bytes.Equal(result.([]byte), txs[perm[i]]), "expecting same data")
	}
	_, err = tree.Remove(*getNodeKeys([]uint64{200}, txs)[0])
	require.Error(t, err, "expecting error when removing nonexistent node")
}

func TestLLRBUpdatekey(t *testing.T) {
	testUpdateKey(t, enumllrb)
}

func TestBTreeUpdatekey(t *testing.T) {
	testUpdateKey(t, enumbtree)
}

func testUpdateKey(t *testing.T, enum treeEnum) {
	tree := treeGen(enum)
	n := 100
	txs := getRandomBytes(n)
	perm := rand.Perm(n)
	var nks []*NodeKey
	for i := 0; i < n; i++ {
		nk := &NodeKey{Priority: uint64(perm[i])}
		tree.Insert(*nk, txs[perm[i]])
		nks = append(nks, nk)
	}
	for i := 0; i < n; i++ {
		newKey := *nks[i]
		newKey.Priority += 100
		err := tree.UpdateKey(newKey, *nks[i])
		require.Error(t, err, "expecting error when updating nonexistent keys")
		err = tree.UpdateKey(*nks[i], newKey)
		require.NoError(t, err, "expecting no error when updating existed keys")
		data, _ := tree.Remove(newKey)
		require.True(t, bytes.Equal(data.([]byte), txs[perm[i]]), "expecting same tx after updating keys")
	}
}

func TestLLRBGetNext(t *testing.T) {
	testGetNext(t, enumllrb)
}

func TestBTreeGetNext(t *testing.T) {
	testGetNext(t, enumbtree)
}

func testGetNext(t *testing.T, enum treeEnum) {
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
		tree := treeGen(enum)
		txs := getRandomBytes(len(tc.priorities))
		nks := getNodeKeys(tc.priorities, txs)
		limit := 20
		if tc.byteLength != nil {
			txs = getFixedBytes(tc.byteLength)
		}
		if tc.byteLimit != 0 {
			limit = tc.byteLimit
		}
		for j := 0; j < len(nks); j++ {
			tree.Insert(*nks[j], txs[j])
		}
		ordered := getOrderedTxs(tree, limit)
		require.Equal(t, len(tc.expectedTxOrder), len(ordered), "expecting equal tx count at testcase %d", i)
		for j, k := range tc.expectedTxOrder {
			require.True(t, bytes.Equal(txs[k], ordered[j]), "expecting equal bytes at testcase %d", i)
		}
	}
}

// BenchmarkLLRBInsert-8   	 1399514	       770 ns/op
func BenchmarkLLRBInsert(b *testing.B) {
	benchmarkInsert(b, enumllrb)
}

// BenchmarkBTreeInsert-8   	 1000000	      1561 ns/op
func BenchmarkBTreeInsert(b *testing.B) {
	benchmarkInsert(b, enumbtree)
}

func benchmarkInsert(b *testing.B, enum treeEnum) {
	tree := treeGen(enum)
	for i := 0; i < b.N; i++ {
		tree.Insert(NodeKey{Priority: uint64(i)}, getRandomBytes(1)[0])
	}
}

// BenchmarkLLRBRemove-8   	 1000000	      1382 ns/op
func BenchmarkLLRBRemove(b *testing.B) {
	benchmarkRemove(b, enumllrb)
}

// BenchmarkBTreeRemove-8   	 1607142	       810 ns/op
func BenchmarkBTreeRemove(b *testing.B) {
	benchmarkRemove(b, enumbtree)
}

func benchmarkRemove(b *testing.B, enum treeEnum) {
	b.StopTimer()
	tree := treeGen(enum)
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
