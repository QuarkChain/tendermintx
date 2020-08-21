package llrb

import "time"

type NodeKey struct {
	Priority uint64
	TS       time.Time
}

type LLRB interface {
	Size() int
	GetNext(starter *NodeKey, predicate func(interface{}) bool) (interface{}, error)
	Insert(starter NodeKey, data interface{}) error
	Remove(starter NodeKey) (interface{}, error)
}

func New() LLRB {
	return newLLRB(maxSize)
}
