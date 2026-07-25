# `sorted_list`

Sorted `int64` multiset with indexed rank lookup.

`SortedList` is an `int64` multiset backed by a red-black tree in which every
node also stores the size of its subtree. That augmentation is what makes
`GetByIndex` a rank lookup rather than a scan: it descends the tree comparing
the index against subtree sizes, so retrieving the *k*-th smallest value costs
O(log n) instead of O(k). Repeated occurrences of a key share a single node
with a count, so a multiset with many duplicates stays compact.

## Example

```go
import (
    "fmt"

    "github.com/pliu/go-utils/sorted_list"
)

scores := sorted_list.NewSortedList()
scores.Insert(20)
scores.Insert(10)
scores.Insert(20)

for score := range scores.Keys() { // 10, 20, 20
    fmt.Println(score)
}
second, ok := scores.GetByIndex(1) // 20, true

removed := scores.Delete(20)       // true: removed one of the two 20s
missing := scores.Delete(99)       // false: nothing to remove
```

`Merge` copies every occurrence from another list, leaving the source
unchanged.

## Operations

| Operation | Cost |
|---|---|
| `Insert` | O(log n) |
| `Delete` | O(log n), removes one occurrence and reports whether it found one |
| `GetByIndex` | O(log n) |
| `Len` | O(1) |
| `Keys` | O(n) for a full traversal, does not allocate |
| `Merge` | O(m log n) for a source of m occurrences |

`Delete` on a key that is not present is a no-op returning `false`, and
`GetByIndex` reports `false` for an out-of-range index rather than panicking.
`Keys` can be stopped early and the list must not be modified while its
iterator is running.
A `SortedList` is not safe for concurrent use; guard it with your own lock if
several goroutines share one instance.

## Invariants

The costs above hold only if the red-black invariants actually hold, and
deletion is the easy place to lose them: the node replacing a spliced-out one
is frequently nil, and a missing child still counts as black and still needs
rebalancing above it. The tests check this directly rather than assuming it.
After randomized workloads, ordered insert/delete patterns, and draining the
tree to empty, they assert that

- every node's size equals its own count plus its children's sizes,
- no red node has a red child,
- every root-to-leaf path crosses the same number of black nodes, and
- the resulting height stays within the red-black bound of 2·log2(n+1),

while cross-checking `Len`, `Keys`, and `GetByIndex` against a plain model of
the multiset.
