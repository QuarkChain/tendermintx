package llrb

import (
	"bytes"
	"math/rand"
	"testing"
	"time"
)

func getNodeKeys(count int) []NodeKey {
	perm := rand.Perm(count)
	var nks []NodeKey
	for i := 0; i < count; i++ {
		nk := NodeKey{
			priority: uint64(perm[i]),
			ts:       time.Now(),
		}
		nks = append(nks, nk)
	}
	return nks
}

func TestCases(t *testing.T) {
	tree := new(llrb)
	nks := getNodeKeys(1)
	txBytes := make([]byte, 20)
	rand.Read(txBytes)
	tree.Insert(nks[0], txBytes)
	tree.Insert(nks[0], txBytes)
	if tree.Size() != 1 {
		t.Errorf("expecting len 1")
	}
	if !bytes.Equal(tree.Get(&nks[0]),txBytes) {
		t.Errorf("expecting same transaction bytes")
	}

	tree.Delete(&nks[0])
	if tree.Size() != 0 {
		t.Errorf("expecting len 0")
	}
	if tree.Get(&nks[0]) != nil {
		t.Errorf("not expecting to find key")
	}

	tree.Delete(&nks[0])
	if tree.Size() != 0 {
		t.Errorf("expecting len 0")
	}
	if tree.Get(&nks[0]) != nil {
		t.Errorf("not expecting to find key")
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
