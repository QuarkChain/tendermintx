package tree

import (
	"flag"
	"sync"

	gbt "github.com/google/btree"
)

// Degree of tree
var btreeDegree = flag.Int("degree", 32, "B-Tree degree")

type btree struct {
	mtx  sync.RWMutex
	tree *gbt.BTree
}

type bnode struct {
	key  NodeKey
	data interface{}
}

var _ gbt.Item = &bnode{}

func (a bnode) Less(b gbt.Item) bool {
	return a.key.compare(b.(bnode).key) < 0
}

// newBTree return btree with given maxSize
func newBTree() *btree {
	return &btree{tree: gbt.New(*btreeDegree)}
}

// Size returns the number of nodes in the tree
func (t *btree) Size() int {
	t.mtx.RLock()
	defer t.mtx.RUnlock()
	return t.tree.Len()
}

// GetNext retrieves a satisfied tx with "largest" nodeKey and "smaller" than starter if provided
func (t *btree) GetNext(starter *NodeKey, predicate func(interface{}) bool) (interface{}, NodeKey, error) {
	t.mtx.RLock()
	defer t.mtx.RUnlock()

	var candidate *bnode
	var descendFunc func(gbt.ItemIterator)
	if starter == nil {
		descendFunc = t.tree.Descend
	} else {
		descendFunc = func(iter gbt.ItemIterator) { t.tree.DescendLessOrEqual(bnode{key: *starter}, iter) }
	}
	descendFunc(func(current gbt.Item) bool {
		cbn := current.(bnode)
		if starter != nil && cbn.key == *starter {
			// Ignore starter itself
			return true
		}
		if predicate == nil || predicate(cbn.data) {
			candidate = &cbn
			// Target found. Stop descending
			return false
		}
		return true
	})
	if candidate == nil {
		return nil, NodeKey{}, ErrorStopIteration
	}
	return candidate.data, candidate.key, nil
}

// Insert inserts value into the tree
func (t *btree) Insert(key NodeKey, data interface{}) {
	t.mtx.Lock()
	defer t.mtx.Unlock()
	item := bnode{
		key:  key,
		data: data,
	}
	t.tree.ReplaceOrInsert(item)
}

// Remove removes a value from the tree with provided key,
// removed data is returned if key found, otherwise nil is returned
func (t *btree) Remove(key NodeKey) (interface{}, error) {
	t.mtx.Lock()
	defer t.mtx.Unlock()
	item := bnode{
		key: key,
	}
	deleted := t.tree.Delete(item)
	if deleted == nil {
		return deleted, errorKeyNotFound
	}
	return deleted.(bnode).data, nil
}
