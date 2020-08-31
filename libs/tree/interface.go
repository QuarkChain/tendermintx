package tree

import (
	"crypto/sha256"
	"time"
)

type NodeKey struct {
	Priority uint64
	TS       time.Time
	Hash     [sha256.Size]byte
}

type BalancedTree interface {
	Size() int
	GetNext(starter *NodeKey, predicate func(interface{}) bool) (interface{}, NodeKey, error)
	Insert(key NodeKey, data interface{}) error
	Remove(key NodeKey) (interface{}, error)
	UpdateKey(oldKey NodeKey, newKey NodeKey) error
}

func NewLLRB(speedUp bool) BalancedTree {
	return newLLRB(maxSize, speedUp)
}

func NewBTree(speedUp bool) BalancedTree {
	return newBTree(maxSize)
}
