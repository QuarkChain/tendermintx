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

type NodeKey struct {
	priority uint64
	ts       time.Time
}

func (a NodeKey) NewNodeKey(priority uint64, ts time.Time) *NodeKey {
	key := &NodeKey{
		priority: priority,
		ts:       ts,
	}
	return key
}

func (a NodeKey) Compare(b NodeKey) int {
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

type node struct {
	Key         NodeKey
	Data        []byte
	Left, Right *node
	Black       bool
}

type Llrb struct {
	mtx     sync.RWMutex
	wg      *sync.WaitGroup
	waitCh  chan struct{}
	size    int
	maxSize int
	root    *node
}

func (t *Llrb) Init() *Llrb {
	t.mtx.Lock()
	t.wg = waitGroup1()
	t.waitCh = make(chan struct{})
	t.root = nil
	t.size = 0
	t.mtx.Unlock()
	return t
}

// Return CList with MaxLength. CList will panic if it goes beyond MaxLength.
func New() *Llrb { return newWithMax(MaxSize) }

// Return CList with given maxLength.
// Will panic if list exceeds given maxLength.
func newWithMax(maxSize int) *Llrb {
	t := new(Llrb)
	t.maxSize = maxSize
	return t.Init()
}

// Size returns the number of nodes in the tree.
func (t *Llrb) Size() int {
	t.mtx.RLock()
	s := t.size
	t.mtx.RUnlock()
	return s
}

// GetNext retrieves an "largest" element "smaller" than starter if provided
func (t *Llrb) GetNext(startPriority uint64, startTime time.Time) ([]byte, error) {
	return nil, nil
}

// Insert inserts value into the tree.
func (t *Llrb) Insert(priority uint64, time time.Time, data []byte) error {
	var err error
	key := NodeKey{
		priority: priority,
		ts:       time,
	}
	t.root, err = t.insert(t.root, key, data)
	t.root.Black = true
	if err != nil {
		t.size++
	}
	return err
}

func (t *Llrb) insert(h *node, key NodeKey, data []byte) (*node, error) {
	if h == nil {
		return &node{Key: key, Data: data}, nil
	}

	var err error

	switch comp := key.Compare(h.Key); comp {
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

func (t *Llrb) deleteMin(h *node) (*node, *NodeKey) {
	if h == nil {
		return nil, nil
	}
	if h.Left == nil {
		return nil, &h.Key
	}

	if !isRed(h.Left) && !isRed(h.Left.Left) {
		h = t.moveRedLeft(h)
	}

	var deleted *NodeKey
	h.Left, deleted = t.deleteMin(h.Left)

	return t.fixUp(h), deleted
}

// Delete deletes a value from the tree whose value equals key.
// The deleted data is return, otherwise nil is returned.
func (t *Llrb) Delete(key *NodeKey) []byte {

	var deleted *NodeKey
	t.root, deleted = t.delete(t.root, key)
	if t.root != nil {
		t.root.Black = true
	}
	if deleted != nil {
		t.size--
	}

	return nil
}

func (t *Llrb) delete(h *node, key *NodeKey) (*node, *NodeKey) {
	var deleted *NodeKey
	if h == nil {
		return nil, nil
	}
	if key.Compare(h.Key) == -1 {
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
		if key.Compare(h.Key) == 0 && h.Right == nil {
			return nil, &h.Key
		}
		if h.Right != nil && !isRed(h.Right) && !isRed(h.Right.Left) {
			h = t.moveRedRight(h)
		}
		if key.Compare(h.Key) == 0 {
			deleted = &h.Key
			r, k := t.deleteMin(h.Right)
			h.Right, h.Key = r, *k
		} else {
			h.Right, deleted = t.delete(h.Right, key)
		}
	}

	return t.fixUp(h), deleted
}

func (t *Llrb) rotateLeft(h *node) *node {
	x := h.Right
	if x.Black {
		panic("rotating a black link")
	}
	h.Right = x.Left
	x.Left = h
	x.Black = h.Black
	h.Black = false
	return x
}

func (t *Llrb) rotateRight(h *node) *node {
	x := h.Left
	if x.Black {
		panic("rotating a black link")
	}
	h.Left = x.Right
	x.Right = h
	x.Black = h.Black
	h.Black = false
	return x
}

func (t *Llrb) moveRedLeft(h *node) *node {
	flip(h)
	if isRed(h.Right.Left) {
		h.Right = t.rotateRight(h.Right)
		h = t.rotateLeft(h)
		flip(h)
	}
	return h
}

func (t *Llrb) moveRedRight(h *node) *node {
	flip(h)
	if isRed(h.Left.Left) {
		h = t.rotateRight(h)
		flip(h)
	}
	return h
}

func (t *Llrb) fixUp(h *node) *node {
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
	return !h.Black
}

func flip(h *node) {
	h.Black = !h.Black
	h.Left.Black = !h.Left.Black
	h.Right.Black = !h.Right.Black
}

func waitGroup1() (wg *sync.WaitGroup) {
	wg = &sync.WaitGroup{}
	wg.Add(1)
	return
}
