package sorted_list

import (
	"fmt"
	"math/rand"
	"testing"
)

var sizes = []int{100, 10_000, 1_000_000}

// fill returns a list of n occurrences drawn from a keyspace of the given
// size, so a keyspace far below n exercises the multiset's shared-node path
// and a keyspace at or above n exercises distinct nodes. seed selects the
// key set: two fills with different seeds overlap only by chance.
func fill(n, keyspace int, seed int64) (*SortedList, []int64) {
	rng := rand.New(rand.NewSource(seed))
	sl := NewSortedList()
	keys := make([]int64, n)
	for i := range n {
		keys[i] = rng.Int63n(int64(keyspace))
		sl.Insert(keys[i])
	}
	return sl, keys
}

// Insert grows the tree as the benchmark runs, so the reported cost is an
// average across sizes rather than the cost at a fixed n. The churn
// benchmarks below hold n steady and are the better read on steady state.
func BenchmarkInsert(b *testing.B) {
	b.Run("sequential", func(b *testing.B) {
		sl := NewSortedList()
		b.ReportAllocs()
		for i := range b.N {
			sl.Insert(int64(i))
		}
	})
	b.Run("random", func(b *testing.B) {
		rng := rand.New(rand.NewSource(1))
		sl := NewSortedList()
		b.ReportAllocs()
		for range b.N {
			sl.Insert(rng.Int63())
		}
	})
	// Every insert lands on an existing node, so no allocation and no
	// rebalancing: this is the multiset fast path.
	b.Run("duplicates", func(b *testing.B) {
		sl := NewSortedList()
		b.ReportAllocs()
		for range b.N {
			sl.Insert(42)
		}
	})
}

// Insert followed by Delete of the same key holds the tree at a fixed size,
// which is what a sliding window does.
func BenchmarkInsertDeleteChurn(b *testing.B) {
	for _, n := range sizes {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			sl, keys := fill(n, n, 1)
			b.ReportAllocs()
			b.ResetTimer()
			for i := range b.N {
				k := keys[i%len(keys)]
				sl.Delete(k)
				sl.Insert(k)
			}
		})
	}
}

// The rank lookup the size augmentation exists for: O(log n), not O(k).
func BenchmarkGetByIndex(b *testing.B) {
	for _, n := range sizes {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			sl, _ := fill(n, n, 1)
			rng := rand.New(rand.NewSource(2))
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				sl.GetByIndex(rng.Intn(n))
			}
		})
	}
}

func BenchmarkDeleteMissing(b *testing.B) {
	for _, n := range sizes {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			sl, _ := fill(n, n, 1)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				sl.Delete(-1) // never present: pure lookup cost
			}
		})
	}
}

func BenchmarkKeys(b *testing.B) {
	for _, n := range sizes {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			sl, _ := fill(n, n, 1)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				_ = sl.Keys()
			}
		})
	}
}

// Duplicate-heavy lists share nodes, so both the tree and these operations
// scale with the number of distinct keys rather than the occurrence count.
func BenchmarkDuplicateHeavy(b *testing.B) {
	const n = 100_000
	for _, keyspace := range []int{10, 1_000, 100_000} {
		b.Run(fmt.Sprintf("keyspace=%d", keyspace), func(b *testing.B) {
			sl, keys := fill(n, keyspace, 1)
			b.ReportAllocs()
			b.ResetTimer()
			for i := range b.N {
				k := keys[i%len(keys)]
				sl.Delete(k)
				sl.Insert(k)
			}
		})
	}
}

func BenchmarkMerge(b *testing.B) {
	for _, n := range []int{100, 10_000} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			// Different seeds, so the merge mostly creates new nodes rather
			// than incrementing counts on keys the destination already has.
			_, keys := fill(n, n, 1)
			src, _ := fill(n, n, 99)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				b.StopTimer()
				dst := NewSortedList()
				for _, k := range keys {
					dst.Insert(k)
				}
				b.StartTimer()
				dst.Merge(src)
			}
		})
	}
}
