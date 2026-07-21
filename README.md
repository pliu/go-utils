# go-utils

Reusable Go utilities. Each tool lives in its own package so importers only
pull in what they use.

| Package | Description | Example |
|---|---|---|
| [`configmanager`](./configmanager) | Generic, type-safe, hot-reloading JSON config loader. | [Usage](./configmanager/EXAMPLE.md) |
| [`set`](./set) | Generic set for comparable Go values. | [Usage](./set/EXAMPLE.md) |
| [`sorted_list`](./sorted_list) | Sorted `int64` multiset with indexed rank lookup. | [Usage](./sorted_list/EXAMPLE.md) |
| [`stats`](./stats) | Concurrent sliding-window averages and percentiles. | [Usage](./stats/EXAMPLE.md) |

## set

`Set` ignores duplicate additions and also provides `Len` and `Equals`.
See the [`set` example](./set/EXAMPLE.md) for usage.

## sorted_list

`SortedList` is an `int64` multiset backed by a rank-aware red-black tree.
`Merge` copies every occurrence from another list, while leaving the source
unchanged. See the [`sorted_list` example](./sorted_list/EXAMPLE.md) for usage.

## stats

`Stats` is safe for concurrent use. Values older than the configured window
are removed when the tracker is read or updated. `Percentile` accepts values
from `0` through `100`; it returns the element at
`floor((count-1) * percentile/100)`. Use `Values`, `Len`, or `Merge` to inspect
and combine trackers. See the [`stats` example](./stats/EXAMPLE.md) for usage.

## configmanager

See the [`configmanager` example](./configmanager/EXAMPLE.md) for setup and
usage.

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
registry; see the
[Prometheus example](./configmanager/EXAMPLE.md#prometheus-metrics).

| Metric | Type | Description |
|---|---|---|
| `configmanager_load_duration_seconds` | Histogram | Duration of successful initial loads and reloads, including valid unchanged content. Failed loads are excluded. |
| `configmanager_last_reload_successful` | Gauge | `1` when the most recent load/reload succeeded, otherwise `0`; initialized to `1` after `New` succeeds. |

When registering collectors from multiple managers in the same registry, add
a const label to distinguish them, as shown in the
[Prometheus example](./configmanager/EXAMPLE.md#prometheus-metrics).

## License

Licensed under the Apache License 2.0. See [`LICENSE`](./LICENSE).
