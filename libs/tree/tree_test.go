package tree

import (
	"bytes"
	cr "crypto/rand"
	"crypto/sha256"
	"math"
	"math/big"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var testSize = 1000000

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

func getNextOrderedTxs(t interface{}, byteLimit int) [][]byte {
	var starter *NodeKey
	var txs [][]byte
	var tree = t.(BalancedTree)
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

func iterNextOrderedTxs(t interface{}, byteLimit int) [][]byte {
	var starter *NodeKey
	var txs [][]byte
	var tree = t.(IterableTree)
	tree.Register(0)
	for {
		result, next, err := tree.IterNext(0, starter, func(v interface{}) bool { return len(v.([]byte)) <= byteLimit })
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

func TestLLRBBasics(t *testing.T) {
	testTreeBasics(t, NewLLRB)
}

func TestBTreeBasics(t *testing.T) {
	testTreeBasics(t, NewBTree)
}

func testTreeBasics(t *testing.T, treeGen func() BalancedTree) {
	tree := treeGen()
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
	testRandomInsertSequenceDelete(t, NewLLRB)
}

func TestBTreeRandomInsertSequenceDelete(t *testing.T) {
	testRandomInsertSequenceDelete(t, NewBTree)
}

func testRandomInsertSequenceDelete(t *testing.T, treeGen func() BalancedTree) {
	tree := treeGen()
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
	testRandomInsertDeleteNonExistent(t, NewLLRB)
}

func TestBTreeRandomInsertDeleteNonExistent(t *testing.T) {
	testRandomInsertDeleteNonExistent(t, NewBTree)
}

func testRandomInsertDeleteNonExistent(t *testing.T, treeGen func() BalancedTree) {
	tree := treeGen()
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
func TestLLRBGetNext(t *testing.T) {
	testGetNext(t, NewLLRB, false)
}

func TestLLRBIterNext(t *testing.T) {
	testGetNext(t, NewLLRB, true)
}

func TestBTreeGetNext(t *testing.T) {
	testGetNext(t, NewBTree, false)
}

func testGetNext(t *testing.T, treeGen func() BalancedTree, useIterator bool) {
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
		tree := treeGen()
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
		var ordered [][]byte
		if useIterator {
			ordered = iterNextOrderedTxs(tree, limit)
		} else {
			ordered = getNextOrderedTxs(tree, limit)
		}
		require.Equal(t, len(tc.expectedTxOrder), len(ordered), "expecting equal tx count at testcase %d", i)
		for j, k := range tc.expectedTxOrder {
			require.True(t, bytes.Equal(txs[k], ordered[j]), "expecting equal bytes at testcase %d", i)
		}
	}
}

//goos: darwin
//goarch: amd64
//pkg: github.com/tendermint/tendermint/libs/tree
//BenchmarkLLRBInsert-8            1724072               663 ns/op
//BenchmarkBTreeInsert-8           1675202               769 ns/op
//BenchmarkLLRBRemove-8            1211070              1019 ns/op
//BenchmarkBTreeRemove-8           2185234               616 ns/op

func BenchmarkLLRBInsert(b *testing.B) {
	benchmarkInsert(b, NewLLRB)
}

func BenchmarkBTreeInsert(b *testing.B) {
	benchmarkInsert(b, NewBTree)
}

func benchmarkInsert(b *testing.B, treeGen func() BalancedTree) {
	tree := treeGen()
	for i := 0; i < b.N; i++ {
		tree.Insert(NodeKey{Priority: uint64(i)}, getRandomBytes(1)[0])
	}
}

func BenchmarkLLRBRemove(b *testing.B) {
	benchmarkRemove(b, NewLLRB)
}

func BenchmarkBTreeRemove(b *testing.B) {
	benchmarkRemove(b, NewBTree)
}

func benchmarkRemove(b *testing.B, treeGen func() BalancedTree) {
	b.StopTimer()
	tree := treeGen()
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

// GetNext Benchmarks
// testsize = 100
//BenchmarkLLRBGetNext-8            108014             10832 ns/op
//BenchmarkLLRBIterNext-8            63244             20879 ns/op
//BenchmarkBTreeGetNext-8            16766             77508 ns/op
//BenchmarkGetAll-8                  54942             18655 ns/op
//BenchmarkIterAll-8                195439              6608 ns/op
// testsize = 1000
//BenchmarkLLRBGetNext-8              5124            228192 ns/op
//BenchmarkLLRBIterNext-8             4693            235503 ns/op
//BenchmarkBTreeGetNext-8             1441            792664 ns/op
//BenchmarkGetAll-8                   4402            237955 ns/op
//BenchmarkIterAll-8                 23947             52018 ns/op
// testsize = 10000
//BenchmarkLLRBGetNext-8               424           2960079 ns/op
//BenchmarkLLRBIterNext-8              501           2226079 ns/op
//BenchmarkBTreeGetNext-8               99          10119141 ns/op
//BenchmarkGetAll-8                    175           6181894 ns/op
//BenchmarkIterAll-8                   538           2051830 ns/op
// testsize = 100000
//BenchmarkLLRBGetNext-8                20          51832558 ns/op
//BenchmarkLLRBIterNext-8               32          35636880 ns/op
//BenchmarkBTreeGetNext-8                9         120034507 ns/op
//BenchmarkGetAll-8                     12          97441124 ns/op
//BenchmarkIterAll-8                    36          33734100 ns/op
// testsize = 1000000
//BenchmarkLLRBGetNext-8                 2         592568886 ns/op
//BenchmarkLLRBIterNext-8                3         385438734 ns/op
//BenchmarkBTreeGetNext-8                1        1456426722 ns/op
//BenchmarkGetAll-8                      1        1826249765 ns/op
//BenchmarkIterALl-8                     3         480941656 ns/op

func BenchmarkLLRBGetNext(b *testing.B) {
	benchmarkGetNext(b, NewLLRB)
}

func BenchmarkLLRBIterNext(b *testing.B) {
	tree := newLLRB()
	for i := 0; i < testSize; i++ {
		randNum, _ := cr.Int(cr.Reader, big.NewInt(1000))
		data := getRandomBytes(1)[0]
		tree.Insert(NodeKey{Priority: randNum.Uint64(), Hash: txHash(data)}, data)
	}
	if tree.Size() != testSize {
		b.Fatal("invalid tree size", tree.Size())
	}
	var nextKey NodeKey
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tree.Register(0)
		_, startKey, err := tree.IterNext(0, nil, func(interface{}) bool { return true })
		for err == nil {
			_, nextKey, err = tree.IterNext(0, &startKey, func(interface{}) bool { return true })
			if nextKey.Priority > startKey.Priority {
				b.Fatal("invalid iteration")
			}
			startKey = nextKey
		}
	}
}

func BenchmarkBTreeGetNext(b *testing.B) {
	benchmarkGetNext(b, NewBTree)
}

func benchmarkGetNext(b *testing.B, treeGen func() BalancedTree) {
	tree := treeGen()
	for i := 0; i < testSize; i++ {
		randNum, _ := cr.Int(cr.Reader, big.NewInt(1000))
		data := getRandomBytes(1)[0]
		tree.Insert(NodeKey{Priority: randNum.Uint64(), Hash: txHash(data)}, data)
	}
	if tree.Size() != testSize {
		b.Fatal("invalid tree size", tree.Size())
	}
	var nextKey NodeKey
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, startKey, err := tree.GetNext(nil, func(interface{}) bool { return true })
		for err == nil {
			_, nextKey, err = tree.GetNext(&startKey, func(interface{}) bool { return true })
			if nextKey.Priority > startKey.Priority {
				b.Fatal("invalid iteration")
			}
			startKey = nextKey
		}
	}
}

func BenchmarkGetAll(b *testing.B) {
	tree := newLLRB()
	for i := 0; i < testSize; i++ {
		randNum, _ := cr.Int(cr.Reader, big.NewInt(1000))
		data := getRandomBytes(1)[0]
		tree.Insert(NodeKey{Priority: randNum.Uint64(), Hash: txHash(data)}, data)
	}
	if tree.Size() != testSize {
		b.Fatal("invalid tree size", tree.Size())
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := tree.getAll(func(interface{}) bool { return true }, nil, []NodeKey{})
		if len(result) != testSize {
			b.Fatal("invalid result size", tree.Size())
		}
	}
}

func BenchmarkIterAll(b *testing.B) {
	tree := newLLRB()
	for i := 0; i < testSize; i++ {
		randNum, _ := cr.Int(cr.Reader, big.NewInt(1000))
		data := getRandomBytes(1)[0]
		tree.Insert(NodeKey{Priority: randNum.Uint64(), Hash: txHash(data)}, data)
	}
	if tree.Size() != testSize {
		b.Fatal("invalid tree size", tree.Size())
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := tree.iterAll(func(interface{}) bool { return true })
		if len(result) != testSize {
			b.Fatal("invalid result size", tree.Size())
		}
	}
}
