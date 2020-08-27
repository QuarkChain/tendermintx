package balancedtree

import (
	"math"

	gbt "github.com/google/btree"
)

type btree struct {
	tree *gbt.BTree
	//iter *gbt.ItemIterator
}

type bnode struct {
	key  NodeKey
	data interface{}
}

var _ gbt.Item = &bnode{}

func (a bnode) Less(b gbt.Item) bool {
	return a.key.compare(b.(*bnode).key) <= 0
}

// newBTREE return btree with given maxSize
func newBTREE(maxSize int) *btree {
	return &btree{tree: gbt.New(int(math.Sqrt(float64(maxSize))))}
}

// Size returns the number of nodes in the tree
func (t *btree) Size() int {
	return t.tree.Len()
}

// GetNext retrieves a satisfied tx with "largest" nodeKey and "smaller" than starter if provided
func (t *btree) GetNext(starter *NodeKey, predicate func(interface{}) bool) (interface{}, error) {
	return nil, nil
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

// Remove removes a value from the tree with provided key
// The removed data is return if key found, otherwise nil is returned
func (t *btree) Remove(key NodeKey) (interface{}, error) {
	item := bnode{
		key: key,
	}
	deleted := t.tree.Delete(item)
	return deleted.(*bnode).data, nil
}
