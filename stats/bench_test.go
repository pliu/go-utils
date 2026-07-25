package stats

import (
	"fmt"
	"testing"
	"time"
)

// A window long enough that nothing expires during a benchmark, so the
// measurement is of the write path alone.
const noExpiry = time.Hour

func filled(n int, window time.Duration) *Stats {
	s := NewStats(window)
	for i := range n {
		s.Add(int64(i))
	}
	return s
}

// Add with nothing expiring: the ring grows, and each distinct value costs
// one tree node.
func BenchmarkAdd(b *testing.B) {
	b.Run("distinct values", func(b *testing.B) {
		s := NewStats(noExpiry)
		b.ReportAllocs()
		for i := range b.N {
			s.Add(int64(i))
		}
	})
	// Repeats land on an existing tree node, so the only cost is the ring.
	b.Run("repeated values", func(b *testing.B) {
		s := NewStats(noExpiry)
		b.ReportAllocs()
		for i := range b.N {
			s.Add(int64(i % 1000))
		}
	})
}

// Steady state: the window is saturated, so each Add also expires older
// measurements and the ring reuses its slots instead of growing. This is the
// shape a long-running collector actually runs in.
//
// The window is wall-clock, so how many measurements it holds depends on
// machine speed; the allocation counts are the stable signal here, not ns/op.
func BenchmarkAddSteadyState(b *testing.B) {
	for _, window := range []time.Duration{time.Millisecond, 10 * time.Millisecond} {
		b.Run(window.String(), func(b *testing.B) {
			s := NewStats(window)
			for i := range 10_000 {
				s.Add(int64(i % 1000))
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := range b.N {
				s.Add(int64(i % 1000))
			}
		})
	}
}

// Every method takes the same lock, including read-only ones, so concurrent
// use serializes. This measures that contention.
func BenchmarkAddParallel(b *testing.B) {
	s := NewStats(noExpiry)
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			s.Add(int64(i % 1000))
			i++
		}
	})
}

func BenchmarkMixedReadWriteParallel(b *testing.B) {
	s := filled(10_000, noExpiry)
	ps := []float64{50, 90, 99}
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if i%4 == 0 {
				s.Add(int64(i % 1000))
			} else {
				s.Percentile(ps)
			}
			i++
		}
	})
}

// Percentiles are rank lookups into the sorted list, so cost grows with
// log(n) and with how many percentiles are requested, not with a sort.
func BenchmarkPercentile(b *testing.B) {
	for _, n := range []int{100, 10_000, 1_000_000} {
		s := filled(n, noExpiry)
		for _, count := range []int{1, 3, 10} {
			ps := make([]float64, count)
			for i := range ps {
				ps[i] = float64(i) * 100 / float64(count)
			}
			b.Run(fmt.Sprintf("n=%d/percentiles=%d", n, count), func(b *testing.B) {
				b.ReportAllocs()
				for range b.N {
					s.Percentile(ps)
				}
			})
		}
	}
}

// Average is O(1) against a running sum, unlike Percentile.
func BenchmarkAverage(b *testing.B) {
	for _, n := range []int{100, 1_000_000} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			s := filled(n, noExpiry)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				s.Average()
			}
		})
	}
}

func BenchmarkLen(b *testing.B) {
	s := filled(10_000, noExpiry)
	b.ReportAllocs()
	for range b.N {
		s.Len()
	}
}

// Values copies the whole window; this is the cost of preferring it over
// Percentile or Average.
func BenchmarkValues(b *testing.B) {
	for _, n := range []int{100, 10_000} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			s := filled(n, noExpiry)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				_ = s.Values()
			}
		})
	}
}

func BenchmarkMerge(b *testing.B) {
	for _, n := range []int{100, 10_000} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			src := filled(n, noExpiry)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				b.StopTimer()
				dst := filled(n, noExpiry)
				b.StartTimer()
				dst.Merge(src)
			}
		})
	}
}
