package tree

import (
	"flag"

	gbt "github.com/google/btree"
)

// Degree of tree
var btreeDegree = flag.Int("degree", 32, "B-Tree degree")

type btree struct {
	tree    *gbt.BTree
	maxSize int
}

type bnode struct {
	key  NodeKey
	data interface{}
}

var _ gbt.Item = &bnode{}

func (a bnode) Less(b gbt.Item) bool {
	return a.key.compare(b.(bnode).key) < 0
}

// newBTREE return btree with given maxSize
func newBTREE(maxSize int) *btree {
	return &btree{tree: gbt.New(*btreeDegree), maxSize: maxSize}
}

// Size returns the number of nodes in the tree
func (t *btree) Size() int {
	return t.tree.Len()
}

// GetNext retrieves a satisfied tx with "largest" nodeKey and "smaller" than starter if provided
func (t *btree) GetNext(starter *NodeKey, predicate func(interface{}) bool) (interface{}, NodeKey, error) {
	var candidate bnode
	if starter == nil {
		t.tree.Descend(func(current gbt.Item) bool {
			if (predicate == nil || predicate(current.(bnode).data)) && // the starter should be exclusive
				(candidate == (bnode{}) || candidate.Less(current)) {
				candidate = current.(bnode)
			}
			return true
		})
	} else {
		t.tree.DescendLessOrEqual(bnode{
			key: *starter,
		}, func(current gbt.Item) bool {
			if current.Less(bnode{key: *starter}) && // starter should be exclusive
				(predicate == nil || predicate(current.(bnode).data)) && // should satisfy user requirements
				(candidate == (bnode{}) || candidate.Less(current)) {
				candidate = current.(bnode)
			}
			return true
		})
	}
	if candidate == (bnode{}) {
		return nil, NodeKey{}, ErrorStopIteration
	}
	return candidate.data, candidate.key, nil
}

func (t *btree) UpdateKey(oldKey NodeKey, newKey NodeKey) error {
	data, err := t.Remove(oldKey)
	if err != nil {
		return err
	}
	t.tree.ReplaceOrInsert(bnode{
		key:  newKey,
		data: data,
	})
	return nil
}

// Insert inserts value into the tree
func (t *btree) Insert(key NodeKey, data interface{}) error {
	item := bnode{
		key:  key,
		data: data,
	}
	if t.tree.Has(item) {
		return ErrorKeyConflicted
	}
	t.tree.ReplaceOrInsert(item)
	return nil
}

// Remove removes a value from the tree with provided key,
// removed data is returned if key found, otherwise nil is returned
func (t *btree) Remove(key NodeKey) (interface{}, error) {
	item := bnode{
		key: key,
	}
	deleted := t.tree.Delete(item)
	if deleted == nil {
		return deleted, ErrorKeyNotFound
	}
	return deleted.(bnode).data, nil
}
