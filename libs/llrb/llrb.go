package llrb

import (
	"fmt"
	"sync"
	"time"
)

// MaxSize is the max allowed number of node a llrb is
// allowed to contain.
// If more nodes are pushed it will panic.
const MaxSize = int(^uint(0) >> 1)

type nodeKey struct {
	priority uint64
	ts       time.Time
}

func (a nodeKey) compare(b nodeKey) int {
	if a.priority > b.priority {
		return 1
	}
	if a.priority < b.priority {
		return -1
	}
	if a.ts.Before(b.ts) {
		return 1
	}
	if a.ts.After(b.ts) {
		return -1
	}
	return 0
}

func NewNodeKey(priority uint64, ts time.Time) interface{} {
	k := new(nodeKey)
	k.priority = priority
	k.ts = ts
	return k
}

type node struct {
	Key         nodeKey
	Data        interface{}
	Left, Right *node
	black       bool
}

type LLRB interface {
	Size() int
	GetNext(starter *nodeKey, predicate func(interface{}) bool) ([]byte, error)
	Insert(priority uint64, time time.Time, data interface{}) error
	Delete(priority uint64, time time.Time) interface{}
}

func NewLLRB() LLRB {
	return newWithMax(MaxSize)
}

type llrb struct {
	mtx     sync.RWMutex
	wg      *sync.WaitGroup
	waitCh  chan struct{}
	size    int
	maxSize int
	root    *node
}

// Return llrb with given maxLength.
// Will panic if list exceeds given maxLength.
func newWithMax(maxSize int) *llrb {
	t := new(llrb)
	t.mtx.Lock()
	defer t.mtx.Unlock()
	t.wg = waitGroup1()
	t.waitCh = make(chan struct{})
	t.root = nil
	t.size = 0
	t.maxSize = maxSize
	return t
}

// Size returns the number of nodes in the tree.
func (t *llrb) Size() int {
	t.mtx.RLock()
	defer t.mtx.RUnlock()
	return t.size
}

// GetNext retrieves a satisfied tx with "largest" nodeKey and "smaller" than starter if provided
func (t *llrb) GetNext(starter *nodeKey, predicate func(interface{}) bool) ([]byte, error) {
	//var key *nodeKey
	//if starter != nil {
	//	key = starter.(*nodeKey)
	//}
	if starter == nil {
		return nil, fmt.Errorf("not implemented")
	}
	return nil, nil
}

// Insert inserts value into the tree.
func (t *llrb) Insert(priority uint64, time time.Time, data interface{}) error {
	var err error
	key := nodeKey{
		priority: priority,
		ts:       time,
	}
	t.root, err = t.insert(t.root, key, data)
	t.root.black = true
	if err == nil {
		t.size++
	}
	return err
}

func (t *llrb) insert(h *node, key nodeKey, data interface{}) (*node, error) {
	if h == nil {
		return &node{Key: key, Data: data}, nil
	}

	var err error

	switch comp := key.compare(h.Key); comp {
	case -1:
		h.Left, err = t.insert(h.Left, key, data)
	case 1:
		h.Right, err = t.insert(h.Right, key, data)
	default:
		err = fmt.Errorf("key conflict")
	}

	if isRed(h.Right) && !isRed(h.Left) {
		h = t.rotateLeft(h)
	}

	if isRed(h.Left) && isRed(h.Left.Left) {
		h = t.rotateRight(h)
	}

	if isRed(h.Left) && isRed(h.Right) {
		flip(h)
	}

	return h, err
}

func (t *llrb) deleteMin(h *node) (*node, *node) {
	if h == nil {
		return nil, nil
	}
	if h.Left == nil {
		return nil, h
	}

	if !isRed(h.Left) && !isRed(h.Left.Left) {
		h = t.moveRedLeft(h)
	}

	var deleted *node
	h.Left, deleted = t.deleteMin(h.Left)

	return t.fixUp(h), deleted
}

// Delete deletes a value from the tree whose value equals key.
// The deleted data is return, otherwise nil is returned.
func (t *llrb) Delete(priority uint64, time time.Time) interface{} {

	var deleted *node
	key := &nodeKey{
		priority: priority,
		ts:       time,
	}
	t.root, deleted = t.delete(t.root, key)
	if t.root != nil {
		t.root.black = true
	}
	if deleted != nil {
		t.size--
		return deleted.Data
	}
	return nil
}

func (t *llrb) delete(h *node, key *nodeKey) (*node, *node) {
	var deleted *node
	if h == nil {
		return nil, nil
	}
	if key.compare(h.Key) == -1 {
		if h.Left == nil {
			return h, nil
		}
		if !isRed(h.Left) && !isRed(h.Left.Left) {
			h = t.moveRedLeft(h)
		}
		h.Left, deleted = t.delete(h.Left, key)
	} else {
		if isRed(h.Left) {
			h = t.rotateRight(h)
		}
		if key.compare(h.Key) == 0 && h.Right == nil {
			return nil, h
		}
		if h.Right != nil && !isRed(h.Right) && !isRed(h.Right.Left) {
			h = t.moveRedRight(h)
		}
		if key.compare(h.Key) == 0 {

			deleted = h
			r, k := t.deleteMin(h.Right)
			h.Right, h.Key, h.Data = r, k.Key, k.Data
		} else {
			h.Right, deleted = t.delete(h.Right, key)
		}
	}

	return t.fixUp(h), deleted
}

func (t *llrb) rotateLeft(h *node) *node {
	x := h.Right
	if x.black {
		panic("rotating a black link")
	}
	h.Right = x.Left
	x.Left = h
	x.black = h.black
	h.black = false
	return x
}

func (t *llrb) rotateRight(h *node) *node {
	x := h.Left
	if x.black {
		panic("rotating a black link")
	}
	h.Left = x.Right
	x.Right = h
	x.black = h.black
	h.black = false
	return x
}

func (t *llrb) moveRedLeft(h *node) *node {
	flip(h)
	if isRed(h.Right.Left) {
		h.Right = t.rotateRight(h.Right)
		h = t.rotateLeft(h)
		flip(h)
	}
	return h
}

func (t *llrb) moveRedRight(h *node) *node {
	flip(h)
	if isRed(h.Left.Left) {
		h = t.rotateRight(h)
		flip(h)
	}
	return h
}

func (t *llrb) fixUp(h *node) *node {
	if isRed(h.Right) {
		h = t.rotateLeft(h)
	}

	if isRed(h.Left) && isRed(h.Left.Left) {
		h = t.rotateRight(h)
	}

	if isRed(h.Left) && isRed(h.Right) {
		flip(h)
	}

	return h
}

func isRed(h *node) bool {
	if h == nil {
		return false
	}
	return !h.black
}

func flip(h *node) {
	h.black = !h.black
	h.Left.black = !h.Left.black
	h.Right.black = !h.Right.black
}

func waitGroup1() (wg *sync.WaitGroup) {
	wg = &sync.WaitGroup{}
	wg.Add(1)
	return
}
