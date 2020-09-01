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

func NewLLRB(speedUp bool) BalancedTree {
	return newLLRB(speedUp)
}

func NewBTree(speedUp bool) BalancedTree {
	return newBTree(speedUp)
}
