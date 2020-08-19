package llrb

import (
	"bytes"
	cr "crypto/rand"
	"math/rand"
	"testing"
	"time"
)

func getNodeKeys(count int) []nodeKey {
	perm := rand.Perm(count)
	var nks []nodeKey
	for i := 0; i < count; i++ {
		nk := NewNodeKey(uint64(perm[i]), time.Now())
		nks = append(nks, *(nk.(*nodeKey)))
	}
	return nks
}

func TestCases(t *testing.T) {
	tree := NewLLRB()
	nks := getNodeKeys(1)
	txBytes := make([]byte, 20)
	cr.Read(txBytes)
	tree.Insert(nks[0].priority, nks[0].ts, txBytes)
	if tree.Size() != 1 {
		t.Errorf("expecting len 1")
	}
	data := tree.Delete(nks[0].priority, nks[0].ts)
	if tree.Size() != 0 {
		t.Errorf("expecting len 0")
	}
	if !bytes.Equal(data.([]byte), txBytes) {
		t.Errorf("expecting equal bytes")
	}
}
