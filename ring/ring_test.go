package ring

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestZeroValueIsUsable(t *testing.T) {
	var r Ring[int]
	require.Equal(t, 0, r.Len())
	require.Equal(t, 0, r.Cap())

	_, ok := r.Front()
	require.False(t, ok)
	_, ok = r.Pop()
	require.False(t, ok)
	_, ok = r.At(0)
	require.False(t, ok)
	require.Empty(t, r.Items())

	r.Push(1)
	require.Equal(t, []int{1}, r.Items())
}

func TestFIFO(t *testing.T) {
	var r Ring[int]
	for i := range 5 {
		r.Push(i)
	}
	require.Equal(t, 5, r.Len())
	require.Equal(t, []int{0, 1, 2, 3, 4}, r.Items())

	for i := range 5 {
		front, ok := r.Front()
		require.True(t, ok)
		require.Equal(t, i, front)

		popped, ok := r.Pop()
		require.True(t, ok)
		require.Equal(t, i, popped)
	}
	require.Equal(t, 0, r.Len())
	_, ok := r.Pop()
	require.False(t, ok)
}

func TestAt(t *testing.T) {
	var r Ring[string]
	for _, s := range []string{"a", "b", "c"} {
		r.Push(s)
	}
	for i, want := range []string{"a", "b", "c"} {
		got, ok := r.At(i)
		require.True(t, ok)
		require.Equal(t, want, got)
	}
	_, ok := r.At(3)
	require.False(t, ok)
	_, ok = r.At(-1)
	require.False(t, ok)

	// Indices are relative to the oldest element, not the backing array.
	r.Pop()
	got, ok := r.At(0)
	require.True(t, ok)
	require.Equal(t, "b", got)
}

// Growing while the contents straddle the end of the backing array is the
// case a naive copy silently reorders.
func TestGrowthWhileWrapped(t *testing.T) {
	var r Ring[int]
	for i := range minCapacity {
		r.Push(i)
	}
	for range 5 {
		r.Pop()
	}
	for i := minCapacity; i < minCapacity+5; i++ {
		r.Push(i)
	}
	require.Greater(t, r.head, 0, "ring must be wrapped for this test to mean anything")
	require.Equal(t, r.Len(), r.Cap(), "ring must be full so the next push grows it")

	before := r.Items()
	for i := 100; i < 100+minCapacity; i++ {
		r.Push(i)
	}
	require.Equal(t, append(before, 100, 101, 102, 103, 104, 105, 106, 107), r.Items())
}

func TestCapacityIsPowerOfTwo(t *testing.T) {
	var r Ring[int]
	for i := range 1000 {
		r.Push(i)
		c := r.Cap()
		require.Zerof(t, c&(c-1), "capacity %d is not a power of two", c)
	}
}

func TestNewRingWithCapacity(t *testing.T) {
	tests := []struct{ request, want int }{
		{0, 0}, {1, 8}, {8, 8}, {9, 16}, {100, 128}, {1024, 1024}, {-5, 0},
	}
	for _, tc := range tests {
		r := NewRingWithCapacity[int](tc.request)
		require.Equalf(t, tc.want, r.Cap(), "NewRingWithCapacity(%d)", tc.request)
		require.Equal(t, 0, r.Len())
	}

	// A presized ring does not grow until its capacity is exceeded.
	r := NewRingWithCapacity[int](100)
	for i := range 128 {
		r.Push(i)
	}
	require.Equal(t, 128, r.Cap())
	r.Push(128)
	require.Equal(t, 256, r.Cap())
}

// Popped and reset slots must not keep their contents reachable.
func TestVacatedSlotsAreZeroed(t *testing.T) {
	var r Ring[*int]
	v := 42
	r.Push(&v)
	head := r.head
	r.Pop()
	require.Nil(t, r.buf[head], "popped slot should be zeroed")

	for range 5 {
		r.Push(&v)
	}
	r.Reset()
	for i := range r.buf {
		require.Nilf(t, r.buf[i], "slot %d should be zeroed after Reset", i)
	}
}

func TestReset(t *testing.T) {
	var r Ring[int]
	for i := range 20 {
		r.Push(i)
	}
	capBefore := r.Cap()

	r.Reset()
	require.Equal(t, 0, r.Len())
	require.Equal(t, capBefore, r.Cap(), "Reset should keep the backing array")
	_, ok := r.Front()
	require.False(t, ok)

	r.Push(42)
	require.Equal(t, []int{42}, r.Items())
}

func TestItemsReturnsSnapshot(t *testing.T) {
	r := NewRingWithCapacity[int](minCapacity)
	for i := range minCapacity {
		r.Push(i)
	}
	for range 5 {
		r.Pop()
	}
	for i := minCapacity; i < minCapacity+5; i++ {
		r.Push(i)
	}
	require.Greater(t, r.head, 0, "ring must be wrapped for this test to mean anything")

	snapshot := r.Items()
	require.Equal(t, []int{5, 6, 7, 8, 9, 10, 11, 12}, snapshot)

	snapshot[0] = -1
	front, ok := r.Front()
	require.True(t, ok)
	require.Equal(t, 5, front, "mutating the snapshot must not mutate the ring")

	snapshot = r.Items()
	r.Reset()
	r.Push(99)
	require.Equal(t, []int{5, 6, 7, 8, 9, 10, 11, 12}, snapshot,
		"mutating the ring must not mutate an existing snapshot")
}

// Cross-check against a plain slice under randomized push/pop.
func TestMatchesSliceModel(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	var r Ring[int]
	var model []int

	for i := range 20000 {
		if rng.Intn(100) < 55 || len(model) == 0 {
			r.Push(i)
			model = append(model, i)
		} else {
			front, ok := r.Front()
			require.True(t, ok)
			require.Equal(t, model[0], front)

			popped, ok := r.Pop()
			require.True(t, ok)
			require.Equal(t, model[0], popped)
			model = model[1:]
		}
		require.Equal(t, len(model), r.Len())
	}
	require.Equal(t, model, r.Items())
}
