# `sorted_list` example

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

`Merge` inserts every occurrence from another list without changing the
source list.
