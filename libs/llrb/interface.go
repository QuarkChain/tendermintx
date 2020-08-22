package llrb

import (
	"crypto/sha256"
	"time"
)

const TxKeySize = sha256.Size

type NodeKey struct {
	Priority uint64
	TS       time.Time
	TxHash   [TxKeySize]byte
}

type LLRB interface {
	Size() int
	GetNext(starter *NodeKey, predicate func(interface{}) bool) (interface{}, error)
	Insert(key NodeKey, data interface{}) error
	Remove(key NodeKey) (interface{}, error)
	UpdateKey(oldKey NodeKey, newKey NodeKey) error
}

func New() LLRB {
	return newLLRB(maxSize)
}
