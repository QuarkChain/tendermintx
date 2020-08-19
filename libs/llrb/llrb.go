package llrb

import (
	"fmt"
	"math"
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
	key         nodeKey
	data        interface{}
	left, right *node
	black       bool
}

type LLRB interface {
	Size() int
	GetNext(starter *nodeKey, predicate func(interface{}) bool) (interface{}, error)
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
func (t *llrb) GetNext(starter *nodeKey, predicate func(interface{}) bool) (interface{}, error) {
	startKey := nodeKey{
		priority: uint64(math.MaxUint64),
		ts:       time.Now(),
	}
	if starter != nil {
		startKey.priority = starter.priority
		startKey.ts = starter.ts
	}

	var cdd *node
	for h := t.root; h != nil; {
		if h.key.compare(startKey) == -1 {
			if cdd == nil || cdd.key.compare(h.key) == -1 {
				cdd = h
			}
		}
		continue
	}
	if cdd == nil {
		return nil, nil
	}

	return cdd.data, nil
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
		return &node{key: key, data: data}, nil
	}

	var err error

	switch key.compare(h.key) {
	case -1:
		h.left, err = t.insert(h.left, key, data)
	case 1:
		h.right, err = t.insert(h.right, key, data)
	default:
		err = fmt.Errorf("key conflict")
	}

	if isRed(h.right) && !isRed(h.left) {
		h = t.rotateLeft(h)
	}

	if isRed(h.left) && isRed(h.left.left) {
		h = t.rotateRight(h)
	}

	if isRed(h.left) && isRed(h.right) {
		flip(h)
	}

	return h, err
}

func (t *llrb) deleteMin(h *node) (*node, *node) {
	if h == nil {
		return nil, nil
	}
	if h.left == nil {
		return nil, h
	}

	if !isRed(h.left) && !isRed(h.left.left) {
		h = t.moveRedLeft(h)
	}

	var deleted *node
	h.left, deleted = t.deleteMin(h.left)

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
		return deleted.data
	}
	return nil
}

func (t *llrb) delete(h *node, key *nodeKey) (*node, *node) {
	var deleted *node
	if h == nil {
		return nil, nil
	}
	if key.compare(h.key) == -1 {
		if h.left == nil {
			return h, nil
		}
		if !isRed(h.left) && !isRed(h.left.left) {
			h = t.moveRedLeft(h)
		}
		h.left, deleted = t.delete(h.left, key)
	} else {
		if isRed(h.left) {
			h = t.rotateRight(h)
		}
		if key.compare(h.key) == 0 && h.right == nil {
			return nil, h
		}
		if h.right != nil && !isRed(h.right) && !isRed(h.right.left) {
			h = t.moveRedRight(h)
		}
		if key.compare(h.key) == 0 {

			deleted = h
			r, k := t.deleteMin(h.right)
			h.right, h.key, h.data = r, k.key, k.data
		} else {
			h.right, deleted = t.delete(h.right, key)
		}
	}

	return t.fixUp(h), deleted
}

func (t *llrb) rotateLeft(h *node) *node {
	x := h.right
	if x.black {
		panic("rotating a black link")
	}
	h.right = x.left
	x.left = h
	x.black = h.black
	h.black = false
	return x
}

func (t *llrb) rotateRight(h *node) *node {
	x := h.left
	if x.black {
		panic("rotating a black link")
	}
	h.left = x.right
	x.right = h
	x.black = h.black
	h.black = false
	return x
}

func (t *llrb) moveRedLeft(h *node) *node {
	flip(h)
	if isRed(h.right.left) {
		h.right = t.rotateRight(h.right)
		h = t.rotateLeft(h)
		flip(h)
	}
	return h
}

func (t *llrb) moveRedRight(h *node) *node {
	flip(h)
	if isRed(h.left.left) {
		h = t.rotateRight(h)
		flip(h)
	}
	return h
}

func (t *llrb) fixUp(h *node) *node {
	if isRed(h.right) {
		h = t.rotateLeft(h)
	}

	if isRed(h.left) && isRed(h.left.left) {
		h = t.rotateRight(h)
	}

	if isRed(h.left) && isRed(h.right) {
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
	h.left.black = !h.left.black
	h.right.black = !h.right.black
}

func waitGroup1() (wg *sync.WaitGroup) {
	wg = &sync.WaitGroup{}
	wg.Add(1)
	return
}
