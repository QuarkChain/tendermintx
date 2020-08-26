package balancedtree

import (
	gbt "github.com/google/btree"
)

type btree struct {
	tree *gbt.BTree
}

func (a NodeKey) Less(b NodeKey) bool {
	return a.compare(b) <= 0
}

// newBTREE return btree with given maxSize
func newBTREE(maxSize int) *btree {
	return &btree{tree: gbt.New(maxSize)}
}

// Size returns the number of nodes in the tree
func (t *btree) Size() int {
	return 0
}

// GetNext retrieves a satisfied tx with "largest" nodeKey and "smaller" than starter if provided
func (t *btree) GetNext(starter *NodeKey, predicate func(interface{}) bool) (interface{}, error) {
	return nil, nil
}

func (t *btree) UpdateKey(oldKey NodeKey, newKey NodeKey) error {
	return nil
}

// Insert inserts value into the tree
func (t *btree) Insert(key NodeKey, data interface{}) error {
	return nil
}

// Remove removes a value from the tree with provided key
// The removed data is return if key found, otherwise nil is returned
func (t *btree) Remove(key NodeKey) (interface{}, error) {
	return nil, nil
}
