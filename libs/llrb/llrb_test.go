package llrb

import (
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
	tree := new(Llrb)
	nks := getNodeKeys(1)
	txBytes := make([]byte, 20)
	cr.Read(txBytes)
	tree.Insert(nks[0].priority, nks[0].ts, txBytes)
	if tree.Size() != 1 {
		t.Errorf("expecting len 1")
	}
}

//
//func TestRandomReplace(t *testing.T) {
//	tree := new(llrb)
//	n := 100
//	nks := getNodeKeys(n)
//	for i := 0; i < n; i++ {
//		tree.Insert(nks[i])
//	}
//	perm := rand.Perm(n)
//	for i := 0; i < n; i++ {
//		if replaced := tree.Insert(uint64(perm[i])); replaced == NullValue || replaced != uint64(perm[i]) {
//			t.Errorf("error replacing")
//		}
//	}
//}
//
//func TestRandomInsertSequentialDelete(t *testing.T) {
//	tree := new(llrb)
//	n := 1000
//	perm := rand.Perm(n)
//	for i := 0; i < n; i++ {
//		tree.Insert(uint64(perm[i]))
//	}
//	for i := 0; i < n; i++ {
//		tree.Delete(uint64(i))
//	}
//}
//
//func TestRandomInsertDeleteNonExistent(t *testing.T) {
//	tree := new(llrb)
//	n := 100
//	perm := rand.Perm(n)
//	for i := 0; i < n; i++ {
//		tree.Insert(uint64(perm[i]))
//	}
//	if tree.Delete(uint64(200)) != NullValue {
//		t.Errorf("deleted non-existent item")
//	}
//	if tree.Delete(uint64(2000)) != NullValue {
//		t.Errorf("deleted non-existent item")
//	}
//	for i := 0; i < n; i++ {
//		if u := tree.Delete(uint64(i)); u == NullValue || u != uint64(i) {
//			t.Errorf("delete failed")
//		}
//	}
//	if tree.Delete(uint64(200)) != NullValue {
//		t.Errorf("deleted non-existent item")
//	}
//	if tree.Delete(uint64(2000)) != NullValue {
//		t.Errorf("deleted non-existent item")
//	}
//}
//
//func BenchmarkInsert(b *testing.B) {
//	tree := new(llrb)
//	for i := 0; i < b.N; i++ {
//		tree.Insert(uint64(b.N - i))
//	}
//}
//
//func BenchmarkDelete(b *testing.B) {
//	b.StopTimer()
//	tree := new(llrb)
//	for i := 0; i < b.N; i++ {
//		tree.Insert(uint64(b.N - i))
//	}
//	b.StartTimer()
//	for i := 0; i < b.N; i++ {
//		tree.Delete(uint64(i))
//	}
//}
