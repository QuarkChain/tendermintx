package llrb

import "time"

type NodeKey struct {
	Priority uint64
	TS       time.Time
}

type LLRB interface {
	Size() int
	GetNext(starter *NodeKey, predicate func(interface{}) bool) (interface{}, error)
	Insert(key NodeKey, data interface{}) error
	Remove(key NodeKey) (interface{}, error)
	IterInit(starter *NodeKey, predicate func(interface{}) bool) error
	IterNext() error
	IterCurr() (interface{}, error)
	IterHasNext() bool
}

func New() LLRB {
	return newLLRB(maxSize)
}
