package llrb

import (
	"bytes"
	cr "crypto/rand"
	"math"
	"sync"
	"testing"
	"time"
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
		if result == nil || err != nil {
			break
		}
		txs = append(txs, result.([]byte))
		v, _ := txMap.Load(string(result.([]byte)))
		starter = v.(*NodeKey)
	}
	return txs
}

func iterateOrderedTxs(tree LLRB, txMap *sync.Map) [][]byte {
	var starter *NodeKey
	var txs [][]byte
	tree.IterInit(starter, func(interface{}) bool { return true })
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
		t.Errorf("Error when removing element")
	}
	if tree.Size() != 0 {
		t.Errorf("expecting len 0")
	}
	if !bytes.Equal(data.([]byte), txs[0]) {
		t.Errorf("expecting equal bytes")
	}
}

func testGetNext(target func(tree LLRB, txMap *sync.Map) [][]byte, t *testing.T) {
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
		ordered := target(tree, &txsMap)
		for j := 0; j < len(nks); j++ {
			if !bytes.Equal(txs[j], ordered[tc.order[j]]) {
				t.Errorf("expecting equal bytes at testcase %d txs %d", i, j)
			}
		}
	}
}

// Test GetNext by searching through the tree, cost O(log(N)) for each query
func TestLlrb_GetNext(t *testing.T) {
	testGetNext(getOrderedTxs, t)
}

// Test GetNext usting iterator, cost O(log(1)) for each query
func TestLlrb_IterNext(t *testing.T) {
	testGetNext(iterateOrderedTxs, t)
}
