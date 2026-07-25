# go-utils

Reusable Go utilities. Each tool lives in its own package so importers only
pull in what they use.

| Package | Description | Documentation |
|---|---|---|
| [`configmanager`](./configmanager) | Generic, type-safe, hot-reloading JSON config loader. | [README](./configmanager/README.md) |
| [`labelmatch`](./labelmatch) | Rule-based label set matching and mutation. | [README](./labelmatch/README.md) |
| [`ring`](./ring) | Growable FIFO queue backed by a ring buffer. | [README](./ring/README.md) |
| [`set`](./set) | Generic set for comparable Go values. | [README](./set/README.md) |
| [`sorted_list`](./sorted_list) | Sorted `int64` multiset with indexed rank lookup. | [README](./sorted_list/README.md) |
| [`stats`](./stats) | Concurrent sliding-window averages and percentiles. | [README](./stats/README.md) |

## License

Licensed under the Apache License 2.0. See [`LICENSE`](./LICENSE).
