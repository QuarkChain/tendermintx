package tree

import (
	"bytes"
	"errors"
	"math"
	"sync"
)

var ErrorStopIteration = errors.New("STOP ITERATION")
var errorKeyNotFound = errors.New("KEY NOT FOUND")
var errorKeyConflicted = errors.New("KEY CONFLICTED")

func (a NodeKey) compare(b NodeKey) int {
	if a.Priority > b.Priority {
		return 1
	}
	if a.Priority < b.Priority {
		return -1
	}
	if a.TS.Before(b.TS) {
		return 1
	}
	if a.TS.After(b.TS) {
		return -1
	}
	return bytes.Compare(a.Hash[:], b.Hash[:])
}

type node struct {
	key         NodeKey
	data        interface{}
	left, right *node
	black       bool
}

type llrb struct {
	mtx  sync.RWMutex
	size int
	root *node
}

// newLLRB return llrb with given maxSize
func newLLRB() *llrb {
	return &llrb{}
}

// Size returns the number of nodes in the tree
func (t *llrb) Size() int {
	t.mtx.RLock()
	defer t.mtx.RUnlock()
	return t.size
}

// GetNext retrieves a satisfied tx with "largest" nodeKey and "smaller" than starter if provided
func (t *llrb) GetNext(starter *NodeKey, predicate func(interface{}) bool) (interface{}, NodeKey, error) {
	t.mtx.RLock()
	defer t.mtx.RUnlock()
	startKey := NodeKey{
		Priority: uint64(math.MaxUint64),
	}
	if starter != nil {
		startKey = *starter
	}
	var candidate *node
	for h := t.root; h != nil; {
		if h.key.compare(startKey) == -1 {
			if (predicate == nil || predicate(h.data)) && (candidate == nil || candidate.key.compare(h.key) == -1) {
				candidate = h
			}
			h = h.right
		} else {
			h = h.left
		}
	}
	if candidate == nil {
		return nil, NodeKey{}, ErrorStopIteration
	}
	return candidate.data, candidate.key, nil
}

// Insert inserts value into the tree
func (t *llrb) Insert(key NodeKey, data interface{}) {
	t.mtx.Lock()
	defer t.mtx.Unlock()
	var err error
	t.root, err = t.insert(t.root, key, data)
	t.root.black = true
	if err == nil {
		t.size++
	}
}

func (t *llrb) insert(h *node, key NodeKey, data interface{}) (*node, error) {
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
		return h, errorKeyConflicted
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

func (t *llrb) deleteMin(h *node) (*node, node) {
	deleted := node{}
	if h == nil {
		return nil, deleted
	}
	if h.left == nil {
		return nil, *h
	}

	if !isRed(h.left) && !isRed(h.left.left) {
		h = t.moveRedLeft(h)
	}
	h.left, deleted = t.deleteMin(h.left)
	return t.fixUp(h), deleted
}

// Remove removes a value from the tree with provided key
// The removed data is return if key found, otherwise nil is returned
func (t *llrb) Remove(key NodeKey) (interface{}, error) {
	t.mtx.Lock()
	defer t.mtx.Unlock()
	root, deleted := t.delete(t.root, key)
	t.root = root
	if t.root != nil {
		t.root.black = true
	}
	if deleted != (node{}) {
		t.size--
		return deleted.data, nil
	}
	return nil, errorKeyNotFound
}

func (t *llrb) delete(h *node, key NodeKey) (*node, node) {
	deleted := node{}
	if h == nil {
		return nil, deleted
	}
	if key.compare(h.key) == -1 {
		if h.left == nil {
			return h, deleted
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
			return nil, *h
		}
		if h.right != nil && !isRed(h.right) && !isRed(h.right.left) {
			h = t.moveRedRight(h)
		}
		if key.compare(h.key) == 0 {
			deleted = *h
			right, newNode := t.deleteMin(h.right)
			h.right = right
			h.key = newNode.key
			h.data = newNode.data
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
