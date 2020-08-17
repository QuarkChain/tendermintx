package llrb

import (
	"math/rand"
	"testing"
)

func TestCases(t *testing.T) {
	tree := new(llrb)
	tree.Insert(uint64(1))
	tree.Insert(uint64(1))
	if tree.Size() != 1 {
		t.Errorf("expecting len 1")
	}
	if tree.Get(1) == nil {
		t.Errorf("expecting to find key=1")
	}

	tree.Delete(uint64(1))
	if tree.Size() != 0 {
		t.Errorf("expecting len 0")
	}
	if tree.Get(1) != nil {
		t.Errorf("not expecting to find key=1")
	}

	tree.Delete(uint64(1))
	if tree.Size() != 0 {
		t.Errorf("expecting len 0")
	}
	if tree.Get(1) != nil {
		t.Errorf("not expecting to find key=1")
	}
}

func TestRandomReplace(t *testing.T) {
	tree := new(llrb)
	n := 100
	perm := rand.Perm(n)
	for i := 0; i < n; i++ {
		tree.Insert(uint64(perm[i]))
	}
	perm = rand.Perm(n)
	for i := 0; i < n; i++ {
		if replaced := tree.Insert(uint64(perm[i])); replaced == NullValue || replaced != uint64(perm[i]) {
			t.Errorf("error replacing")
		}
	}
}

func TestRandomInsertSequentialDelete(t *testing.T) {
	tree := new(llrb)
	n := 1000
	perm := rand.Perm(n)
	for i := 0; i < n; i++ {
		tree.Insert(uint64(perm[i]))
	}
	for i := 0; i < n; i++ {
		tree.Delete(uint64(i))
	}
}

func TestRandomInsertDeleteNonExistent(t *testing.T) {
	tree := new(llrb)
	n := 100
	perm := rand.Perm(n)
	for i := 0; i < n; i++ {
		tree.Insert(uint64(perm[i]))
	}
	if tree.Delete(uint64(200)) != NullValue {
		t.Errorf("deleted non-existent item")
	}
	if tree.Delete(uint64(2000)) != NullValue {
		t.Errorf("deleted non-existent item")
	}
	for i := 0; i < n; i++ {
		if u := tree.Delete(uint64(i)); u == NullValue || u != uint64(i) {
			t.Errorf("delete failed")
		}
	}
	if tree.Delete(uint64(200)) != NullValue {
		t.Errorf("deleted non-existent item")
	}
	if tree.Delete(uint64(2000)) != NullValue {
		t.Errorf("deleted non-existent item")
	}
}

func BenchmarkInsert(b *testing.B) {
	tree := new(llrb)
	for i := 0; i < b.N; i++ {
		tree.Insert(uint64(b.N - i))
	}
}

func BenchmarkDelete(b *testing.B) {
	b.StopTimer()
	tree := new(llrb)
	for i := 0; i < b.N; i++ {
		tree.Insert(uint64(b.N - i))
	}
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		tree.Delete(uint64(i))
	}
}
