# go-utils

Reusable Go utilities. Each tool lives in its own package so importers only
pull in what they use.

| Package | Description |
|---|---|
| [`configmanager`](./configmanager) | Generic, type-safe, hot-reloading JSON config loader. |
| [`set`](./set) | Generic set for comparable Go values. |
| [`sorted_list`](./sorted_list) | Sorted `int64` multiset with indexed rank lookup. |
| [`stats`](./stats) | Concurrent sliding-window averages and percentiles. |

## set

```go
import "github.com/pliu/go-utils/set"

seen := set.NewSet[string]()
seen.Add("alpha")
seen.Add("beta")

present := seen.Contains("alpha") // true
items := seen.Items()             // iteration order is unspecified
seen.Remove("beta")
```

`Set` ignores duplicate additions and also provides `Len` and `Equals`.

## sorted_list

```go
import "github.com/pliu/go-utils/sorted_list"

scores := sorted_list.NewSortedList()
scores.Insert(20)
scores.Insert(10)
scores.Insert(20)

ordered := scores.Keys()          // []int64{10, 20, 20}
second, ok := scores.GetByIndex(1) // 20, true
scores.Delete(20)                  // removes one occurrence
```

`SortedList` is an `int64` multiset backed by a rank-aware red-black tree.
`Merge` copies every occurrence from another list, while leaving the source
unchanged.

## stats

```go
import (
    "time"

    "github.com/pliu/go-utils/stats"
)

latency := stats.NewStats(5 * time.Minute)
latency.Add(12)
latency.Add(20)
latency.Add(35)

average, ok := latency.Average()                  // 22.333..., true
percentiles, ok := latency.Percentile([]float64{50, 95}) // []int64{20, 20}, true
```

`Stats` is safe for concurrent use. Values older than the configured window
are removed when the tracker is read or updated. `Percentile` accepts values
from `0` through `100`; it returns the element at
`floor((count-1) * percentile/100)`. Use `Values`, `Len`, or `Merge` to inspect
and combine trackers.

## configmanager

```go
import "github.com/pliu/go-utils/configmanager"

type AppConfig struct {
    ListenAddr string `json:"listen_addr" validate:"required"`
    MaxConns   int    `json:"max_conns"`
}

mgr, err := configmanager.New[AppConfig]("/etc/app/config.json")
if err != nil {
    log.Fatal(err) // invalid initial config: fail fast, never start on a bad config
}
defer mgr.Close()

cfg := mgr.Get()          // shared read-only snapshot (do not mutate)
mine := mgr.GetDeepCopy() // fully isolated copy (safe to mutate)
```

- The file is polled in the background (default every 3s, see
  `WithPollInterval`). A changed file is decoded and validated into a fresh
  shadow instance and atomically promoted only if valid; an invalid file
  never disturbs the config being served (inspect via `Err()` /
  `WithOnError`, observe swaps via `WithOnSwap`).
- Decoding tolerates unknown fields, and rejects type mismatches on defined
  fields, duplicate JSON keys, and trailing data. Required fields are
  declared with `validate:"required"` tags
  ([go-playground/validator](https://github.com/go-playground/validator));
  custom rules go in a `Validate() error` method or `WithValidator`.
- Snapshots are stable: a reload swaps in a new instance, so values already
  returned by `Get`/`GetDeepCopy` never change under the caller.

### Prometheus metrics

Each manager exposes an opt-in collector. It is never registered globally and
does not start a metrics server, so it can be added to an application's own
registry:

```go
registry := prometheus.NewRegistry()
registry.MustRegister(mgr.PrometheusCollector())

metricsHandler := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
```

| Metric | Type | Description |
|---|---|---|
| `configmanager_load_duration_seconds` | Histogram | Duration of successful initial loads and reloads, including valid unchanged content. Failed loads are excluded. |
| `configmanager_last_reload_successful` | Gauge | `1` when the most recent load/reload succeeded, otherwise `0`; initialized to `1` after `New` succeeds. |

When registering collectors from multiple managers in the same registry, add
a const label to distinguish them:

```go
registerer := prometheus.WrapRegistererWith(
    prometheus.Labels{"config": "application"},
    registry,
)
registerer.MustRegister(mgr.PrometheusCollector())
```

## License

Licensed under the Apache License 2.0. See [`LICENSE`](./LICENSE).
