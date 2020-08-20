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
		result, _ := tree.GetNext(starter, func(interface{}) bool { return true })
		if result == nil {
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
	nks := getNodeKeys([]uint64{1})
	txs := getRandomBytes(1)
	tree.Insert(*nks[0], txs[0])
	if tree.Size() != 1 {
		t.Errorf("expecting len 1")
	}
	data, _ := tree.Remove(*nks[0])
	if tree.Size() != 0 {
		t.Errorf("expecting len 0")
	}
	if !bytes.Equal(data.([]byte), txs[0]) {
		t.Errorf("expecting equal bytes")
	}
}

func TestGetNext(t *testing.T) {
	testCases := []struct {
		priorities []uint64
		order      []int64
	}{
		// same priority would present as FIFO
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
			if !bytes.Equal(txs[j], ordered[tc.order[j]]) {
				t.Errorf("expecting equal bytes at %d testcase %d txs", i, j)
			}
		}
	}
}
