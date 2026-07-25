package stats

import (
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func meas(v int64) measurement {
	return measurement{timestamp: time.Unix(v, 0), value: v}
}

func ringValues(r *ring) []int64 {
	out := make([]int64, 0, r.len())
	for i := range r.len() {
		out = append(out, r.at(i).value)
	}
	return out
}

func TestRingEmpty(t *testing.T) {
	var r ring
	require.Equal(t, 0, r.len())
	_, ok := r.front()
	require.False(t, ok)
	r.pop() // must not panic
	require.Equal(t, 0, r.len())
}

func TestRingFIFO(t *testing.T) {
	var r ring
	for i := range int64(5) {
		r.push(meas(i))
	}
	require.Equal(t, []int64{0, 1, 2, 3, 4}, ringValues(&r))

	for i := range int64(5) {
		front, ok := r.front()
		require.True(t, ok)
		require.Equal(t, i, front.value)
		r.pop()
	}
	require.Equal(t, 0, r.len())
}

// Growth must unwrap correctly: the oldest element is not at index 0 once
// the ring has wrapped, so a naive copy would reorder the contents.
func TestRingGrowthWhileWrapped(t *testing.T) {
	var r ring
	// Fill to capacity, then pop and push so the contents straddle the end
	// of the backing array.
	for i := range int64(minRingCap) {
		r.push(meas(i))
	}
	for range 5 {
		r.pop()
	}
	for i := int64(minRingCap); i < minRingCap+5; i++ {
		r.push(meas(i))
	}
	require.Greater(t, r.head, 0, "ring should be wrapped for this test to mean anything")

	// Force a grow while wrapped.
	before := ringValues(&r)
	for i := int64(100); i < 100+minRingCap; i++ {
		r.push(meas(i))
	}
	require.Equal(t, append(before, []int64{100, 101, 102, 103, 104, 105, 106, 107}...), ringValues(&r))
}

func TestRingCapacityStaysPowerOfTwo(t *testing.T) {
	var r ring
	for i := range int64(1000) {
		r.push(meas(i))
		c := len(r.buf)
		require.Zerof(t, c&(c-1), "capacity %d is not a power of two", c)
	}
}

func TestRingPopReleasesSlot(t *testing.T) {
	var r ring
	r.push(meas(1))
	head := r.head
	r.pop()
	require.Zero(t, r.buf[head].value, "popped slot should be zeroed")
	require.True(t, r.buf[head].timestamp.IsZero(), "popped slot should be zeroed")
}

func TestRingReset(t *testing.T) {
	var r ring
	for i := range int64(20) {
		r.push(meas(i))
	}
	r.reset()
	require.Equal(t, 0, r.len())
	require.Equal(t, 0, r.head)
	_, ok := r.front()
	require.False(t, ok)

	r.push(meas(42))
	require.Equal(t, []int64{42}, ringValues(&r))
}

// Cross-check against a plain slice under randomized push/pop.
func TestRingMatchesSliceModel(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	var r ring
	var model []int64

	for i := range int64(20000) {
		if rng.Intn(100) < 55 || len(model) == 0 {
			r.push(meas(i))
			model = append(model, i)
		} else {
			front, ok := r.front()
			require.True(t, ok)
			require.Equal(t, model[0], front.value)
			r.pop()
			model = model[1:]
		}
		require.Equal(t, len(model), r.len())
	}
	require.Equal(t, model, ringValues(&r))
}
