# `stats`

Concurrent sliding-window averages and percentiles.

`Stats` is safe for concurrent use. Values older than the configured window
are removed when the tracker is read or updated. `Percentile` accepts values
from `0` through `100`; it returns the element at
`floor((count-1) * percentile/100)`. Use `Values`, `Len`, or `Merge` to inspect
and combine trackers.

## Example

```go
import (
    "time"

    "github.com/pliu/go-utils/stats"
)

latency := stats.NewStats(5 * time.Minute)
latency.Add(12)
latency.Add(20)
latency.Add(35)

average, ok := latency.Average()                         // 22.333..., true
percentiles, ok := latency.Percentile([]float64{50, 95}) // []int64{20, 20}, true
```

## Performance

Values are held twice: in arrival order in a ring buffer, so the oldest can be
expired, and in sorted order in a [`sorted_list`](../sorted_list), so a
percentile is an O(log n) rank lookup rather than a sort. `Average` is O(1)
against a running sum.

The ring buffer stores measurements by value and reuses its slots as the
window slides, so once it has grown to the working set, `Add` allocates only
when the value is one the window does not already contain — and nothing at all
in a steady state of repeating values.

Every method takes the same lock, including the read-only ones: expiry is
driven lazily from whatever call happens to arrive, so reads mutate too and
cannot share an `RWMutex`. Reads therefore serialize against concurrent
`Add`s. `Values` copies the whole window, so prefer `Percentile`, `Average`,
or `Len` unless the individual values are actually needed.
