package sorted_list

import "iter"

// SortedList maintains keys with multiplicity using a red-black tree. It supports
// inserting and deleting key occurrences while allowing rank lookups without
// storing per-key payload values.

const (
	colorRed   = 1
	colorBlack = 0
)

type sortedListNode struct {
	key                 int64
	count               int
	size                int
	color               int
	left, right, parent *sortedListNode
}

type SortedList struct {
	root *sortedListNode
	len  int
}

func NewSortedList() *SortedList {
	return &SortedList{}
}

func (sl *SortedList) Len() int {
	return sl.len
}

// Keys iterates over keys in ascending order, including duplicates.
// The SortedList must not be modified during iteration.
func (sl *SortedList) Keys() iter.Seq[int64] {
	return func(yield func(int64) bool) {
		sl.yieldKeys(sl.root, yield)
	}
}

func (sl *SortedList) yieldKeys(node *sortedListNode, yield func(int64) bool) bool {
	if node == nil {
		return true
	}
	if !sl.yieldKeys(node.left, yield) {
		return false
	}
	for i := 0; i < node.count; i++ {
		if !yield(node.key) {
			return false
		}
	}
	return sl.yieldKeys(node.right, yield)
}

// Merge inserts all keys from other into this sorted list.
func (sl *SortedList) Merge(other *SortedList) {
	if other == nil || other.root == nil {
		return
	}
	if sl == other {
		return
	}
	sl.mergeNode(other.root)
}

func (sl *SortedList) mergeNode(node *sortedListNode) {
	if node == nil {
		return
	}
	sl.mergeNode(node.left)
	sl.insertCount(node.key, node.count)
	sl.mergeNode(node.right)
}

// Insert adds a key occurrence to the structure.
func (sl *SortedList) Insert(key int64) {
	sl.insertCount(key, 1)
}

func (sl *SortedList) insertCount(key int64, count int) {
	if count <= 0 {
		return
	}
	if sl.root == nil {
		sl.root = &sortedListNode{key: key, count: count, size: count, color: colorBlack}
		sl.len += count
		return
	}

	current := sl.root
	var parent *sortedListNode
	for current != nil {
		parent = current
		if key == current.key {
			current.count += count
			sl.len += count
			sl.recomputeSizes(current)
			return
		}
		if key < current.key {
			current = current.left
		} else {
			current = current.right
		}
	}

	newNode := &sortedListNode{key: key, count: count, size: count, color: colorRed, parent: parent}
	if key < parent.key {
		parent.left = newNode
	} else {
		parent.right = newNode
	}

	sl.recomputeSizes(newNode)
	sl.insertFixup(newNode)
	sl.len += count
}

// Delete removes one occurrence of key, reporting whether one was found.
// Deleting a key that is not present is a no-op and returns false.
func (sl *SortedList) Delete(key int64) bool {
	node := sl.find(key)
	if node == nil {
		return false
	}

	if node.count > 1 {
		node.count--
		sl.recomputeSizes(node)
		sl.len--
		return true
	}

	sl.deleteNode(node)
	sl.len--
	return true
}

// GetByIndex returns the key stored at a 0-based position.
func (sl *SortedList) GetByIndex(index int) (int64, bool) {
	if index < 0 || index >= sl.len {
		return 0, false
	}
	node, _ := sl.selectNode(sl.root, index)
	if node == nil {
		return 0, false
	}
	return node.key, true
}

// --- internal helpers ---

func (sl *SortedList) find(key int64) *sortedListNode {
	current := sl.root
	for current != nil {
		if key == current.key {
			return current
		}
		if key < current.key {
			current = current.left
		} else {
			current = current.right
		}
	}
	return nil
}

func (sl *SortedList) selectNode(node *sortedListNode, index int) (*sortedListNode, int) {
	current := node
	remaining := index
	for current != nil {
		leftSize := nodeSize(current.left)
		if remaining < leftSize {
			current = current.left
			continue
		}
		remaining -= leftSize
		if remaining < current.count {
			return current, remaining
		}
		remaining -= current.count
		current = current.right
	}
	return nil, 0
}

func nodeSize(n *sortedListNode) int {
	if n == nil {
		return 0
	}
	return n.size
}

func (sl *SortedList) updateSize(n *sortedListNode) {
	if n == nil {
		return
	}
	n.size = n.count + nodeSize(n.left) + nodeSize(n.right)
}

func (sl *SortedList) recomputeSizes(n *sortedListNode) {
	for current := n; current != nil; current = current.parent {
		sl.updateSize(current)
	}
}

func parentOfNode(n *sortedListNode) *sortedListNode {
	if n == nil {
		return nil
	}
	return n.parent
}

func grandparentOfNode(n *sortedListNode) *sortedListNode {
	return parentOfNode(parentOfNode(n))
}

func (sl *SortedList) leftRotate(x *sortedListNode) {
	y := x.right
	x.right = y.left
	if y.left != nil {
		y.left.parent = x
	}
	y.parent = x.parent
	if x.parent == nil {
		sl.root = y
	} else if x == x.parent.left {
		x.parent.left = y
	} else {
		x.parent.right = y
	}
	y.left = x
	x.parent = y

	// A rotation only rearranges nodes within one subtree, so the subtree's
	// total is unchanged and no ancestor's size can differ. Fixing the two
	// rotated nodes is enough; walking to the root would be wasted work.
	sl.updateSize(x)
	sl.updateSize(y)
}

func (sl *SortedList) rightRotate(y *sortedListNode) {
	x := y.left
	y.left = x.right
	if x.right != nil {
		x.right.parent = y
	}
	x.parent = y.parent
	if y.parent == nil {
		sl.root = x
	} else if y == y.parent.left {
		y.parent.left = x
	} else {
		y.parent.right = x
	}
	x.right = y
	y.parent = x

	// See leftRotate: ancestors' sizes are unaffected by a rotation.
	sl.updateSize(y)
	sl.updateSize(x)
}

func (sl *SortedList) insertFixup(z *sortedListNode) {
	for z.parent != nil && z.parent.color == colorRed {
		gp := grandparentOfNode(z)
		if z.parent == gp.left {
			y := gp.right
			if y != nil && y.color == colorRed {
				z.parent.color = colorBlack
				y.color = colorBlack
				gp.color = colorRed
				z = gp
			} else {
				if z == z.parent.right {
					z = z.parent
					sl.leftRotate(z)
				}
				z.parent.color = colorBlack
				sl.rightRotate(gp)
				gp.color = colorRed
			}
		} else {
			y := gp.left
			if y != nil && y.color == colorRed {
				z.parent.color = colorBlack
				y.color = colorBlack
				gp.color = colorRed
				z = gp
			} else {
				if z == z.parent.left {
					z = z.parent
					sl.rightRotate(z)
				}
				z.parent.color = colorBlack
				sl.leftRotate(gp)
				gp.color = colorRed
			}
		}
	}
	sl.root.color = colorBlack
}

func (sl *SortedList) deleteNode(z *sortedListNode) {
	// y is the node physically spliced out of the tree: z itself when it has
	// at most one child, otherwise its successor, whose key and count move
	// into z.
	y := z
	if z.left != nil && z.right != nil {
		y = sl.successor(z)
	}

	// x takes y's place and may be nil, so y's parent is captured separately:
	// the rebalance below still needs it after the splice.
	x := y.left
	if x == nil {
		x = y.right
	}
	parent := y.parent

	if x != nil {
		x.parent = parent
	}
	switch {
	case parent == nil:
		sl.root = x
	case y == parent.left:
		parent.left = x
	default:
		parent.right = x
	}

	if y != z {
		z.key = y.key
		z.count = y.count
	}

	// Only the path from the splice point to the root changes size, and
	// recomputeSizes rebuilds each node from its children, so a single walk
	// from the deepest affected node is enough. When y came from under z,
	// that path runs through z and so also picks up z's new count.
	if parent != nil {
		sl.recomputeSizes(parent)
	}

	// Removing a black node shortens every path through it by one, which
	// must be repaired even when its replacement is nil — a missing child
	// counts as black and still needs rebalancing above it.
	if y.color == colorBlack {
		sl.deleteFixup(x, parent)
	}
}

// isRed and isBlack treat a missing child as black, which is what lets the
// fixup below reason about nil nodes without a sentinel.
func isRed(n *sortedListNode) bool   { return n != nil && n.color == colorRed }
func isBlack(n *sortedListNode) bool { return n == nil || n.color == colorBlack }

// deleteFixup restores the red-black invariants after a black node was
// spliced out. x is the node that took its place and may be nil, so its
// parent is passed explicitly rather than read from x — that is the whole
// reason this takes two arguments.
func (sl *SortedList) deleteFixup(x, parent *sortedListNode) {
	for x != sl.root && isBlack(x) {
		if parent == nil {
			break
		}
		if x == parent.left {
			w := parent.right
			if isRed(w) {
				w.color = colorBlack
				parent.color = colorRed
				sl.leftRotate(parent)
				w = parent.right
			}
			if w == nil {
				x, parent = parent, parent.parent
				continue
			}
			if isBlack(w.left) && isBlack(w.right) {
				w.color = colorRed
				x, parent = parent, parent.parent
				continue
			}
			if isBlack(w.right) {
				if w.left != nil {
					w.left.color = colorBlack
				}
				w.color = colorRed
				sl.rightRotate(w)
				w = parent.right
			}
			w.color = parent.color
			parent.color = colorBlack
			if w.right != nil {
				w.right.color = colorBlack
			}
			sl.leftRotate(parent)
			x, parent = sl.root, nil
			continue
		}

		w := parent.left
		if isRed(w) {
			w.color = colorBlack
			parent.color = colorRed
			sl.rightRotate(parent)
			w = parent.left
		}
		if w == nil {
			x, parent = parent, parent.parent
			continue
		}
		if isBlack(w.right) && isBlack(w.left) {
			w.color = colorRed
			x, parent = parent, parent.parent
			continue
		}
		if isBlack(w.left) {
			if w.right != nil {
				w.right.color = colorBlack
			}
			w.color = colorRed
			sl.leftRotate(w)
			w = parent.left
		}
		w.color = parent.color
		parent.color = colorBlack
		if w.left != nil {
			w.left.color = colorBlack
		}
		sl.rightRotate(parent)
		x, parent = sl.root, nil
	}
	if x != nil {
		x.color = colorBlack
	}
}

func (sl *SortedList) successor(x *sortedListNode) *sortedListNode {
	if x.right != nil {
		return sl.minimum(x.right)
	}
	y := x.parent
	for y != nil && x == y.right {
		x = y
		y = y.parent
	}
	return y
}

func (sl *SortedList) minimum(x *sortedListNode) *sortedListNode {
	for x.left != nil {
		x = x.left
	}
	return x
}
