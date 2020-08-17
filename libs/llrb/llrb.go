package llrb

import "time"

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
func (t *llrb) Get(key uint64) *Node {
	h := t.root
	for h != nil {
		switch {
		case key < h.Value:
			h = h.Left
		case key > h.Value:
			h = h.Right
		default:
			return h
		}
	}

	return nil
}

// Insert inserts value into the tree.
func (t *llrb) Insert(value uint64) uint64 {
	var replaced uint64
	t.root, replaced = t.insert(t.root, value)
	t.root.Black = true
	if replaced == NullValue {
		t.size++
	}
	return replaced
}

func (t *llrb) insert(h *Node, value uint64) (*Node, uint64) {
	if h == nil {
		return &Node{Value: value}, NullValue
	}

	var replaced uint64

	switch {
	case value < h.Value:
		h.Left, replaced = t.insert(h.Left, value)
	case value > h.Value:
		h.Right, replaced = t.insert(h.Right, value)
	default:
		replaced, h.Value = h.Value, value
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

	return h, replaced
}

func (t *llrb) deleteMin(h *Node) (*Node, uint64) {
	if h == nil {
		return nil, NullValue
	}
	if h.Left == nil {
		return nil, h.Value
	}

	if !isRed(h.Left) && !isRed(h.Left.Left) {
		h = t.moveRedLeft(h)
	}

	var deleted uint64
	h.Left, deleted = t.deleteMin(h.Left)

	return t.fixUp(h), deleted
}

// Delete deletes a value from the tree whose value equals key.
// The deleted value is return, otherwise NullValue is returned.
func (t *llrb) Delete(key uint64) uint64 {
	var deleted uint64
	t.root, deleted = t.delete(t.root, key)
	if t.root != nil {
		t.root.Black = true
	}
	if deleted != NullValue {
		t.size--
	}

	return deleted
}

func (t *llrb) delete(h *Node, value uint64) (*Node, uint64) {
	var deleted uint64
	if h == nil {
		return nil, NullValue
	}
	if value < h.Value {
		if h.Left == nil {
			return h, NullValue
		}
		if !isRed(h.Left) && !isRed(h.Left.Left) {
			h = t.moveRedLeft(h)
		}
		h.Left, deleted = t.delete(h.Left, value)
	} else {
		if isRed(h.Left) {
			h = t.rotateRight(h)
		}
		if h.Value == value && h.Right == nil {
			return nil, h.Value
		}
		if h.Right != nil && !isRed(h.Right) && !isRed(h.Right.Left) {
			h = t.moveRedRight(h)
		}
		if h.Value == value {
			deleted = h.Value
			h.Right, h.Value = t.deleteMin(h.Right)
		} else {
			h.Right, deleted = t.delete(h.Right, value)
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
