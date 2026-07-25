package sorted_list

import (
	"math"
	"math/rand"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSortedListInsertAndGetByIndex(t *testing.T) {
	sl := NewSortedList()
	sl.Insert(10)
	sl.Insert(20)
	sl.Insert(5)
	sl.Insert(10) // duplicate
	sl.Insert(15)

	require.Equal(t, 5, sl.Len())

	expected := []int64{5, 10, 10, 15, 20}
	for i, key := range expected {
		value, ok := sl.GetByIndex(i)
		require.True(t, ok)
		require.Equal(t, key, value)
	}

	_, ok := sl.GetByIndex(len(expected))
	require.False(t, ok)
}

func TestSortedListDelete(t *testing.T) {
	sl := NewSortedList()
	sl.Insert(10)
	sl.Insert(10)
	sl.Insert(5)
	sl.Insert(20)

	// One of the two occurrences of 10 goes; the key itself remains.
	sl.Delete(10)
	require.Equal(t, 3, sl.Len())

	value, ok := sl.GetByIndex(0)
	require.True(t, ok)
	require.Equal(t, int64(5), value)

	value, ok = sl.GetByIndex(1)
	require.True(t, ok)
	require.Equal(t, int64(10), value)

	// The last occurrence goes, removing the key.
	sl.Delete(10)
	require.Equal(t, 2, sl.Len())
	value, ok = sl.GetByIndex(1)
	require.True(t, ok)
	require.Equal(t, int64(20), value)

	sl.Delete(42) // no-op
	require.Equal(t, 2, sl.Len())
}

func TestSortedListMerge(t *testing.T) {
	left := NewSortedList()
	left.Insert(10)
	left.Insert(5)
	left.Insert(10)

	right := NewSortedList()
	right.Insert(7)
	right.Insert(10)
	right.Insert(15)

	left.Merge(right)
	require.Equal(t, 6, left.Len())

	expected := []int64{5, 7, 10, 10, 10, 15}
	for i, key := range expected {
		value, ok := left.GetByIndex(i)
		require.True(t, ok)
		require.Equal(t, key, value)
	}

	require.Equal(t, 3, right.Len())
}

func TestSortedListKeys(t *testing.T) {
	sl := NewSortedList()
	require.Empty(t, sl.Keys())

	values := []int64{10, 5, 10, 7, 7, 20}
	for _, v := range values {
		sl.Insert(v)
	}

	require.Equal(t, []int64{5, 7, 7, 10, 10, 20}, sl.Keys())
}

// --- structural invariants ---
//
// These guard the properties the public API silently depends on: the size
// augmentation that makes GetByIndex correct, and the red-black invariants
// that keep every operation O(log n). Deletion is the easy part to get
// wrong, because the node replacing a spliced-out one is often nil.

func treeHeight(n *sortedListNode) int {
	if n == nil {
		return 0
	}
	return 1 + max(treeHeight(n.left), treeHeight(n.right))
}

// checkSizes verifies that every node's size equals its own count plus its
// children's sizes, and returns the subtree total.
func checkSizes(t *testing.T, n *sortedListNode) int {
	t.Helper()
	if n == nil {
		return 0
	}
	total := n.count + checkSizes(t, n.left) + checkSizes(t, n.right)
	require.Equalf(t, total, n.size, "size augmentation broken at key %d", n.key)
	return total
}

// checkRB verifies that no red node has a red child and that every
// root-to-leaf path crosses the same number of black nodes, returning that
// black-height. Together these bound the tree height at 2*log2(n+1).
func checkRB(t *testing.T, n *sortedListNode) int {
	t.Helper()
	if n == nil {
		return 1
	}
	if n.color == colorRed {
		require.Falsef(t, isRed(n.left) || isRed(n.right),
			"red node with a red child at key %d", n.key)
	}
	left, right := checkRB(t, n.left), checkRB(t, n.right)
	require.Equalf(t, left, right, "black-height mismatch at key %d", n.key)
	if n.color == colorBlack {
		return left + 1
	}
	return left
}

func checkInvariants(t *testing.T, sl *SortedList) {
	t.Helper()
	if sl.root != nil {
		require.Equal(t, colorBlack, sl.root.color, "root must be black")
	}
	require.Equal(t, sl.len, nodeSize(sl.root), "len disagrees with root size")
	checkSizes(t, sl.root)
	checkRB(t, sl.root)

	// The red-black invariants exist to bound the height; assert the bound
	// itself so a future regression shows up as a performance failure too.
	if n := sl.Len(); n > 0 {
		bound := 2 * int(math.Log2(float64(n+1))+1)
		require.LessOrEqualf(t, treeHeight(sl.root), bound,
			"height %d exceeds the red-black bound for n=%d", treeHeight(sl.root), n)
	}
}

func TestInvariantsAfterRandomizedOps(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	sl := NewSortedList()
	counts := make(map[int64]int)

	for i := range 20000 {
		// Skew toward a small key space so duplicates and multi-count nodes
		// are exercised alongside unique keys.
		key := rng.Int63n(500)
		if i%3 == 0 {
			key = rng.Int63n(20000)
		}
		if rng.Intn(100) < 60 || len(counts) == 0 {
			sl.Insert(key)
			counts[key]++
		} else {
			sl.Delete(key)
			if counts[key] > 0 {
				counts[key]--
				if counts[key] == 0 {
					delete(counts, key)
				}
			}
		}
		if i%1000 == 0 {
			checkInvariants(t, sl)
		}
	}
	checkInvariants(t, sl)

	// Cross-check contents against the model.
	want := 0
	for _, c := range counts {
		want += c
	}
	require.Equal(t, want, sl.Len())

	expected := make([]int64, 0, want)
	keys := make([]int64, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		for range counts[k] {
			expected = append(expected, k)
		}
	}
	require.Equal(t, expected, sl.Keys())
	for i, k := range expected {
		got, ok := sl.GetByIndex(i)
		require.True(t, ok)
		require.Equalf(t, k, got, "GetByIndex(%d)", i)
	}
}

// Deleting every node must leave a consistent, empty tree rather than a
// structure that merely reports len 0.
func TestInvariantsDrainToEmpty(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	sl := NewSortedList()
	keys := make([]int64, 0, 5000)
	for range 5000 {
		k := rng.Int63n(2000)
		sl.Insert(k)
		keys = append(keys, k)
	}
	checkInvariants(t, sl)

	rng.Shuffle(len(keys), func(i, j int) { keys[i], keys[j] = keys[j], keys[i] })
	for i, k := range keys {
		sl.Delete(k)
		if i%500 == 0 {
			checkInvariants(t, sl)
		}
	}
	checkInvariants(t, sl)
	require.Equal(t, 0, sl.Len())
	require.Nil(t, sl.root)
}

// Ordered workloads are the ones most likely to unbalance a tree.
func TestInvariantsOrderedWorkloads(t *testing.T) {
	const n = 20000
	tests := []struct {
		name string
		run  func(sl *SortedList)
	}{
		{"ascending insert, ascending delete", func(sl *SortedList) {
			for i := range n {
				sl.Insert(int64(i))
			}
			for i := range n / 2 {
				sl.Delete(int64(i))
			}
		}},
		{"descending insert, ascending delete", func(sl *SortedList) {
			for i := n - 1; i >= 0; i-- {
				sl.Insert(int64(i))
			}
			for i := range n / 2 {
				sl.Delete(int64(i))
			}
		}},
		{"ascending insert, delete evens", func(sl *SortedList) {
			for i := range n {
				sl.Insert(int64(i))
			}
			for i := 0; i < n; i += 2 {
				sl.Delete(int64(i))
			}
		}},
		{"sliding window churn", func(sl *SortedList) {
			for i := range n {
				sl.Insert(int64(i))
				if i >= 1000 {
					sl.Delete(int64(i - 1000))
				}
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sl := NewSortedList()
			tc.run(sl)
			checkInvariants(t, sl)
			t.Logf("n=%d height=%d", sl.Len(), treeHeight(sl.root))
		})
	}
}
