package sorted_list

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

// Keys returns all keys in ascending order, including duplicates.
func (sl *SortedList) Keys() []int64 {
	if sl.len == 0 {
		return []int64{}
	}
	result := make([]int64, 0, sl.len)
	sl.appendKeys(sl.root, &result)
	return result
}

func (sl *SortedList) appendKeys(node *sortedListNode, out *[]int64) {
	if node == nil {
		return
	}
	sl.appendKeys(node.left, out)
	for i := 0; i < node.count; i++ {
		*out = append(*out, node.key)
	}
	sl.appendKeys(node.right, out)
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

// Delete removes one occurrence of key if present.
func (sl *SortedList) Delete(key int64) {
	node := sl.find(key)
	if node == nil {
		return
	}

	if node.count > 1 {
		node.count--
		sl.recomputeSizes(node)
		sl.len--
		return
	}

	sl.deleteNode(node)
	sl.len--
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

	sl.updateSize(x)
	sl.updateSize(y)
	sl.recomputeSizes(y.parent)
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

	sl.updateSize(y)
	sl.updateSize(x)
	sl.recomputeSizes(x.parent)
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
	var x, y *sortedListNode

	if z.left == nil || z.right == nil {
		y = z
	} else {
		y = sl.successor(z)
	}

	removed := y.count
	parentBeforeRemoval := y.parent
	sl.adjustSizesOnDelete(y, removed)

	if y.left != nil {
		x = y.left
	} else {
		x = y.right
	}

	if x != nil {
		x.parent = y.parent
	}

	if y.parent == nil {
		sl.root = x
	} else if y == y.parent.left {
		y.parent.left = x
	} else {
		y.parent.right = x
	}

	if y != z {
		z.key = y.key
		z.count = y.count
		sl.recomputeSizes(z)
	} else if parentBeforeRemoval != nil {
		sl.recomputeSizes(parentBeforeRemoval)
	} else if sl.root != nil {
		sl.recomputeSizes(sl.root)
	}

	if parentBeforeRemoval != nil {
		sl.recomputeSizes(parentBeforeRemoval)
	}

	if y.color == colorBlack {
		if x != nil {
			sl.deleteFixup(x)
		}
	}
}

func (sl *SortedList) adjustSizesOnDelete(node *sortedListNode, removed int) {
	for current := parentOfNode(node); current != nil; current = parentOfNode(current) {
		current.size -= removed
	}
}

func (sl *SortedList) deleteFixup(x *sortedListNode) {
	for x != sl.root && (x == nil || x.color == colorBlack) {
		if x == parentOfNode(x).left {
			w := parentOfNode(x).right
			if w != nil && w.color == colorRed {
				w.color = colorBlack
				parentOfNode(x).color = colorRed
				sl.leftRotate(parentOfNode(x))
				w = parentOfNode(x).right
			}
			if w != nil && (w.left == nil || w.left.color == colorBlack) && (w.right == nil || w.right.color == colorBlack) {
				w.color = colorRed
				x = parentOfNode(x)
			} else if w != nil {
				if w.right == nil || w.right.color == colorBlack {
					if w.left != nil {
						w.left.color = colorBlack
					}
					w.color = colorRed
					sl.rightRotate(w)
					w = parentOfNode(x).right
				}
				w.color = parentOfNode(x).color
				parentOfNode(x).color = colorBlack
				if w.right != nil {
					w.right.color = colorBlack
				}
				sl.leftRotate(parentOfNode(x))
				x = sl.root
			} else {
				x = parentOfNode(x)
			}
		} else {
			w := parentOfNode(x).left
			if w != nil && w.color == colorRed {
				w.color = colorBlack
				parentOfNode(x).color = colorRed
				sl.rightRotate(parentOfNode(x))
				w = parentOfNode(x).left
			}
			if w != nil && (w.right == nil || w.right.color == colorBlack) && (w.left == nil || w.left.color == colorBlack) {
				w.color = colorRed
				x = parentOfNode(x)
			} else if w != nil {
				if w.left == nil || w.left.color == colorBlack {
					if w.right != nil {
						w.right.color = colorBlack
					}
					w.color = colorRed
					sl.leftRotate(w)
					w = parentOfNode(x).left
				}
				w.color = parentOfNode(x).color
				parentOfNode(x).color = colorBlack
				if w.left != nil {
					w.left.color = colorBlack
				}
				sl.rightRotate(parentOfNode(x))
				x = sl.root
			} else {
				x = parentOfNode(x)
			}
		}
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
