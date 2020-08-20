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
}

func New() LLRB {
	return newLlrb(MaxSize)
}

type LlrbIter interface {
	Curr() (interface{}, error)
	Next()
	HasNext() bool
}

// TODO The iterator should passed with a tree, or initialized by a tree
func NewIter(starter *NodeKey, predicate func(interface{}) bool) LlrbIter {
	return newIter(starter, predicate)
}
