# `sorted_list`

Sorted `int64` multiset with indexed rank lookup.

`SortedList` is an `int64` multiset backed by a rank-aware red-black tree.
`Merge` copies every occurrence from another list, while leaving the source
unchanged.

## Example

```go
import "github.com/pliu/go-utils/sorted_list"

scores := sorted_list.NewSortedList()
scores.Insert(20)
scores.Insert(10)
scores.Insert(20)

ordered := scores.Keys()           // []int64{10, 20, 20}
second, ok := scores.GetByIndex(1) // 20, true
scores.Delete(20)                  // removes one occurrence
```
