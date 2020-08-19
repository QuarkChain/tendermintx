package llrb

import (
	"bytes"
	cr "crypto/rand"
	"testing"
	"time"
)

func getNodeKeys(priorities []uint64) []nodeKey {
	var nks []nodeKey
	for i := 0; i < len(priorities); i++ {
		nk := NewNodeKey(priorities[i], time.Now())
		nks = append(nks, *(nk.(*nodeKey)))
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

func getOrderedTxs(tree *llrb) [][]byte {
	var starter *nodeKey
	var ordered
	for result, err := tree.GetNext(starter, func(interface{}) bool { return true }); result != nil && err == nil; {

	}
}

func TestBasics(t *testing.T) {
	tree := NewLLRB()
	nks := getNodeKeys([]uint64{1})
	txs := getRandomBytes(1)
	tree.Insert(nks[0].priority, nks[0].ts, txs[0])
	if tree.Size() != 1 {
		t.Errorf("expecting len 1")
	}
	data := tree.Delete(nks[0].priority, nks[0].ts)
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
		// error case by wrong gas/bytes limit
		{
			priorities: []uint64{0, 0, 0, 0, 0},
			order:      []int64{0, 1, 2, 3, 4},
		},
	}
	for i, tc := range testCases {
		tree := NewLLRB()
		nks := getNodeKeys(tc.priorities)
		txs := getRandomBytes(len(tc.priorities))
		for j := 0; j < len(nks); j++ {
			tree.Insert(nks[j].priority, nks[j].ts, txs[j])
		}
	}
}
