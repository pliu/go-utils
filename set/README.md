# `set`

Generic set for comparable Go values.

`Set[T]` wraps a `map[T]struct{}`, so it accepts any type Go considers
comparable and inherits the map's O(1) expected costs. Adding a value that is
already present is a no-op, as is removing one that is absent.

## Example

```go
import "github.com/pliu/go-utils/set"

seen := set.NewSet[string]()
seen.Add("alpha")
seen.Add("beta")
seen.Add("alpha") // already present: no-op, Len stays 2

present := seen.Contains("alpha") // true
items := seen.Items()             // []string{"alpha", "beta"} in some order
seen.Remove("beta")
seen.Remove("gamma")              // absent: no-op
```

## Operations

| Operation | Cost |
|---|---|
| `Add`, `Remove`, `Contains` | O(1) expected |
| `Len` | O(1) |
| `Items` | O(n), allocates a new slice |
| `Equals` | O(n) |

## Behavior worth knowing

- **Use `NewSet`; the zero value is not usable.** `var s Set[string]` leaves
  the underlying map nil, and `Add` then panics with "assignment to entry in
  nil map". The read-only methods (`Len`, `Contains`, `Items`, `Remove`,
  `Equals`) happen to tolerate it, so the panic can surface later than the
  mistake.
- **`Items` returns a fresh slice in unspecified order.** Go randomizes map
  iteration, so the order differs between calls on the same set — sort the
  result if you need determinism. The slice is a copy; mutating it does not
  affect the set. An empty set yields an empty non-nil slice, never nil.
- **`Equals` compares membership, not iteration order**, so two sets built by
  different insertion sequences compare equal. It returns `false` for a nil
  argument even when the receiver is empty; two empty non-nil sets are equal.
- **A `Set` is not safe for concurrent use.** Guard it with your own lock if
  several goroutines share one instance, and note that even `Contains` needs
  the lock, since a concurrent `Add` is a map write.
- **There is no capacity hint.** A set built by repeated `Add` grows its map
  incrementally and rehashes along the way, so building a large set of known
  size costs more than a single presized allocation would.
