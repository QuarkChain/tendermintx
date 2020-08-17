package llrb

import (
	"fmt"
	"time"
)

const (
	NullValue = uint64(0xFFFFFFFFFFFFFFFF)
)

type llrb struct {
	size int
	root *Node
}

type NodeKey struct {
	priority uint64
	ts       time.Time
}

type Node struct {
	Key         NodeKey
	Left, Right *Node
	Black       bool
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

// Size returns the number of nodes in the tree.
func (t *llrb) Size() int { return t.size }

// Get retrieves an element from the tree whose value is the same as key.
func (t *llrb) Get(key NodeKey) *Node {
	h := t.root
	for h != nil {
		switch comp := key.Compare(h.Key); comp {
		case -1:
			h = h.Left
		case 1:
			h = h.Right
		default:
			return h
		}
	}

	return nil
}

// Insert inserts value into the tree.
func (t *llrb) Insert(key NodeKey) error {
	var err error
	t.root, err = t.insert(t.root, key)
	t.root.Black = true
	if err != nil {
		t.size++
	}
	return err
}

func (t *llrb) insert(h *Node, key NodeKey) (*Node, error) {
	if h == nil {
		return &Node{Key: key}, nil
	}

	var err error

	switch comp := key.Compare(h.Key); comp {
	case -1:
		h.Left, err = t.insert(h.Left, key)
	case 1:
		h.Right, err = t.insert(h.Right, key)
	default:
		err = fmt.Errorf("Key conflict!")
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

func (t *llrb) deleteMin(h *Node) (*Node, *NodeKey) {
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
// The deleted value is return, otherwise nil is returned.
func (t *llrb) Delete(key *NodeKey) *NodeKey {
	var deleted *NodeKey
	t.root, deleted = t.delete(t.root, key)
	if t.root != nil {
		t.root.Black = true
	}
	if deleted != nil {
		t.size--
	}

	return deleted
}

func (t *llrb) delete(h *Node, key *NodeKey) (*Node, *NodeKey) {
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
			r,k := t.deleteMin(h.Right)
			h.Right, h.Key = r, *k
		} else {
			h.Right, deleted = t.delete(h.Right, key)
		}
	}

	return t.fixUp(h), deleted
}

func (t *llrb) rotateLeft(h *Node) *Node {
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

func (t *llrb) rotateRight(h *Node) *Node {
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

func (t *llrb) moveRedLeft(h *Node) *Node {
	flip(h)
	if isRed(h.Right.Left) {
		h.Right = t.rotateRight(h.Right)
		h = t.rotateLeft(h)
		flip(h)
	}
	return h
}

func (t *llrb) moveRedRight(h *Node) *Node {
	flip(h)
	if isRed(h.Left.Left) {
		h = t.rotateRight(h)
		flip(h)
	}
	return h
}

func (t *llrb) fixUp(h *Node) *Node {
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

func isRed(h *Node) bool {
	if h == nil {
		return false
	}
	return !h.Black
}

func flip(h *Node) {
	h.Black = !h.Black
	h.Left.Black = !h.Left.Black
	h.Right.Black = !h.Right.Black
}
