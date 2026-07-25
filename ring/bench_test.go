package ring

import (
	"container/list"
	"fmt"
	"testing"
)

// payload is deliberately larger than a word so that storing elements by
// value has a visible cost, rather than flattering the ring.
type payload struct {
	a, b int64
	s    string
}

func BenchmarkPush(b *testing.B) {
	b.Run("grows from empty", func(b *testing.B) {
		var r Ring[payload]
		b.ReportAllocs()
		for i := range b.N {
			r.Push(payload{a: int64(i)})
		}
	})
	b.Run("presized", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		r := NewRingWithCapacity[payload](b.N)
		for i := range b.N {
			r.Push(payload{a: int64(i)})
		}
	})
}

// Push followed by Pop holds the length steady, so the backing array is
// reused and nothing should allocate. This is the sliding-window shape the
// package exists for.
func BenchmarkPushPopChurn(b *testing.B) {
	for _, depth := range []int{8, 1000, 100_000} {
		b.Run(fmt.Sprintf("depth=%d", depth), func(b *testing.B) {
			r := NewRingWithCapacity[payload](depth)
			for i := range depth {
				r.Push(payload{a: int64(i)})
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := range b.N {
				r.Push(payload{a: int64(i)})
				r.Pop()
			}
		})
	}
}

// The same churn against container/list, which is what stats used before
// this package existed: a node allocation per push, and garbage per pop.
func BenchmarkPushPopChurnLinkedList(b *testing.B) {
	for _, depth := range []int{8, 1000, 100_000} {
		b.Run(fmt.Sprintf("depth=%d", depth), func(b *testing.B) {
			l := list.New()
			for i := range depth {
				l.PushBack(&payload{a: int64(i)})
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := range b.N {
				l.PushBack(&payload{a: int64(i)})
				l.Remove(l.Front())
			}
		})
	}
}

func BenchmarkAt(b *testing.B) {
	r := NewRingWithCapacity[payload](10_000)
	for i := range 10_000 {
		r.Push(payload{a: int64(i)})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		r.At(i % 10_000)
	}
}

func BenchmarkAll(b *testing.B) {
	for _, n := range []int{100, 10_000} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			r := NewRingWithCapacity[payload](n)
			for i := range n {
				r.Push(payload{a: int64(i)})
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				var sum int64
				for _, v := range r.All() {
					sum += v.a
				}
				_ = sum
			}
		})
	}
}
