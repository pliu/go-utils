# `stats` example

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

Values expire as the window advances. The tracker removes expired values on
the next read or update.
