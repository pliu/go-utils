# `ring`

Growable FIFO queue backed by a ring buffer.

`Ring[T]` holds its elements by value in a single slice and wraps around it,
so once the buffer has reached the working set, pushing and popping never
allocate — vacated slots are reused as the queue advances. That is the
difference from a linked list, which allocates a node per push and leaves it
for the collector on every pop.

This is not `container/ring`, which is a circular doubly-linked list with no
queue semantics.

## Example

```go
import "github.com/pliu/go-utils/ring"

var window ring.Ring[int]   // the zero value is ready to use
window.Push(10)
window.Push(20)

oldest, ok := window.Front() // 10, true — peek without removing
first, ok := window.Pop()    // 10, true — remove and return

for v := range window.All() { // oldest to newest
    fmt.Println(v)
}
```

Use `NewRingWithCapacity[T](n)` when the working set is known, to skip the
regrowth of filling one from empty. Capacity is rounded up to a power of two
with a floor of 8, so wrapping an index is a mask rather than a division.

## Operations

| Operation | Cost |
|---|---|
| `Push` | O(1) amortized; grows by doubling |
| `Pop`, `Front` | O(1) |
| `At` | O(1), indexed from the oldest element |
| `Len`, `Cap` | O(1) |
| `Reset` | O(n), keeps the backing array |
| `All` | O(n), allocation-free iterator |

`At` and `Front` report `false` rather than panicking on an out-of-range
index or an empty queue. `Pop` and `Reset` zero the slots they vacate, so a
popped element holding pointers does not stay reachable through the buffer.
A `Ring` is not safe for concurrent use.

## Performance

Steady-state churn, where each push is matched by a pop so the length holds
constant, against the same workload on `container/list`:

| depth | `Ring` | `container/list` |
|---|---|---|
| 8 | 4.0 ns, 0 allocs | 35.8 ns, 80 B, 2 allocs |
| 1000 | 4.1 ns, 0 allocs | 36.5 ns, 80 B, 2 allocs |
| 100000 | 4.5 ns, 0 allocs | 54.4 ns, 80 B, 2 allocs |

The two allocations per operation in the linked-list case are the element
node and the boxed value; both become garbage on the next pop. The ring's
cost is flat in the queue depth and allocates nothing at all.
