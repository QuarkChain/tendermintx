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
	Insert(key NodeKey, data interface{})
	Remove(key NodeKey) (interface{}, error)
}

type IterableTree interface {
	Register(uid uint64) error
	IterNext(uid uint64, starter *NodeKey, predicate func(interface{}) bool) (interface{}, NodeKey, error)
}

func NewLLRB() BalancedTree {
	return newLLRB()
}

func NewBTree() BalancedTree {
	return newBTree()
}
