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
