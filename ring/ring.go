// Package ring provides a growable FIFO queue backed by a ring buffer.
//
// A Ring holds its elements by value in a single slice and wraps around it,
// so pushing and popping never allocate once the buffer has reached the
// working set: vacated slots are reused as the queue advances. That is the
// difference from a linked list, which allocates a node per push and leaves
// it for the collector on every pop — the cost that matters when a queue is
// churning continuously, such as a sliding window.
//
// This is not container/ring, which is a circular doubly-linked list with no
// queue semantics.
//
// A Ring is not safe for concurrent use.
package ring

import (
	"math/bits"
)

// minCapacity is the smallest backing array allocated. Capacities are always
// powers of two so that wrapping an index is a mask rather than a division.
const minCapacity = 8

// Ring is a growable FIFO queue. The zero value is an empty queue ready to
// use; NewRingWithCapacity avoids the regrowth of filling one from empty.
//
// Capacity only ever grows. Pop leaves the vacated slot allocated for the
// next Push to reuse, and Reset keeps the whole array, so a Ring holds
// memory for the longest it has ever been rather than for its current
// length. That reuse is what makes churn allocation-free.
type Ring[T any] struct {
	buf  []T
	head int // index of the oldest element
	n    int // number of elements held
}

// NewRing returns an empty Ring. It is equivalent to new(Ring[T]) and exists
// for symmetry with NewRingWithCapacity.
func NewRing[T any]() *Ring[T] {
	return &Ring[T]{}
}

// NewRingWithCapacity returns an empty Ring whose backing array can hold at
// least capacity elements before it has to grow. The capacity is rounded up
// to a power of two, with a floor of 8.
func NewRingWithCapacity[T any](capacity int) *Ring[T] {
	r := &Ring[T]{}
	if capacity > 0 {
		r.buf = make([]T, roundUpPow2(capacity))
	}
	return r
}

func roundUpPow2(n int) int {
	if n <= minCapacity {
		return minCapacity
	}
	return 1 << bits.Len(uint(n-1))
}

// Len returns the number of elements held.
func (r *Ring[T]) Len() int { return r.n }

// Cap returns how many elements the Ring can hold before it grows.
func (r *Ring[T]) Cap() int { return len(r.buf) }

// At returns the i-th oldest element, reporting false if i is out of range.
// At(0) is the element Front and Pop return.
func (r *Ring[T]) At(i int) (T, bool) {
	if i < 0 || i >= r.n {
		var zero T
		return zero, false
	}
	return r.buf[(r.head+i)&(len(r.buf)-1)], true
}

// Front returns the oldest element without removing it, reporting false if
// the Ring is empty.
func (r *Ring[T]) Front() (T, bool) {
	if r.n == 0 {
		var zero T
		return zero, false
	}
	return r.buf[r.head], true
}

// Push appends v as the newest element, growing the backing array if it is
// full.
func (r *Ring[T]) Push(v T) {
	if r.n == len(r.buf) {
		r.grow()
	}
	r.buf[(r.head+r.n)&(len(r.buf)-1)] = v
	r.n++
}

// Pop removes and returns the oldest element, reporting false if the Ring is
// empty. The vacated slot is zeroed, so a popped element holding pointers
// does not keep them reachable.
func (r *Ring[T]) Pop() (T, bool) {
	if r.n == 0 {
		var zero T
		return zero, false
	}
	v := r.buf[r.head]
	var zero T
	r.buf[r.head] = zero
	r.head = (r.head + 1) & (len(r.buf) - 1)
	r.n--
	return v, true
}

// Reset removes every element, keeping the backing array for reuse. Slots
// are zeroed for the same reason as in Pop.
func (r *Ring[T]) Reset() {
	clear(r.buf)
	r.head = 0
	r.n = 0
}

// All returns a snapshot of the elements from oldest to newest. The returned
// slice does not share its backing array with the Ring.
func (r *Ring[T]) All() []T {
	items := make([]T, r.n)
	if r.n == 0 {
		return items
	}

	copied := copy(items, r.buf[r.head:])
	copy(items[copied:], r.buf[:r.head])
	return items
}

// grow doubles the capacity and unwraps the contents so the oldest element
// sits at index 0. Doubling is what keeps Push amortized O(1).
func (r *Ring[T]) grow() {
	capacity := 2 * len(r.buf)
	if capacity < minCapacity {
		capacity = minCapacity
	}
	buf := make([]T, capacity)
	if r.n > 0 {
		// The contents may straddle the end of the old array, so this takes
		// the tail first and then the wrapped head.
		copied := copy(buf, r.buf[r.head:])
		copy(buf[copied:], r.buf[:r.head])
	}
	r.buf = buf
	r.head = 0
}
