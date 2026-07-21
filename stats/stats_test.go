package stats

import (
	"math/rand"
	"testing"
	"time"

	"github.com/benbjohnson/clock"
	"github.com/stretchr/testify/require"
)

func TestStats_BasicPercentiles(t *testing.T) {
	stats := NewStatsWithClock(1*time.Second, clock.NewMock())

	// Add 100 values from 1 to 100
	for i := 1; i <= 100; i++ {
		stats.Add(int64(i))
	}

	// Check percentiles
	values, ok := stats.Percentile([]float64{50, 90, 99, 100})
	require.True(t, ok)
	require.Equal(t, []int64{50, 90, 99, 100}, values)

	avg, ok := stats.Average()
	require.True(t, ok)
	require.Equal(t, float64(50.5), avg)
}

func TestStats_SlidingWindow(t *testing.T) {
	windowSize := 1 * time.Second
	mockClock := clock.NewMock()
	stats := NewStatsWithClock(windowSize, mockClock)

	// Add initial set of values (1-100)
	for i := 1; i <= 100; i++ {
		stats.Add(int64(i))
	}
	require.Equal(t, 100, stats.values.Len())
	vals, ok := stats.Percentile([]float64{50, 99})
	require.True(t, ok)
	require.Equal(t, []int64{50, 99}, vals)
	avg, ok := stats.Average()
	require.True(t, ok)
	require.InDelta(t, 50.5, avg, 1e-9)

	// Advance clock by half the window size
	mockClock.Add(windowSize / 2)

	// Add a second set of values (101-200)
	for i := 101; i <= 200; i++ {
		stats.Add(int64(i))
	}
	require.Equal(t, 200, stats.values.Len())

	// Check percentiles with mixed data {1..100, 101..200}
	vals, ok = stats.Percentile([]float64{50, 95, 99})
	require.True(t, ok)
	require.Equal(t, int64(100), vals[0])
	require.Equal(t, int64(190), vals[1])
	require.Equal(t, int64(198), vals[2])
	avg, ok = stats.Average()
	require.True(t, ok)
	require.InDelta(t, 100.5, avg, 1e-9)

	// Advance clock so the first set expires
	mockClock.Add(windowSize/2 + 1*time.Millisecond)

	// Add a third set of values (201-300). This will trigger cleanup.
	for i := 201; i <= 300; i++ {
		stats.Add(int64(i))
	}

	// Now the list should contain {101..200, 201..300}
	require.Equal(t, 200, stats.values.Len())

	vals, ok = stats.Percentile([]float64{50, 95, 99})
	require.True(t, ok)
	require.Equal(t, int64(200), vals[0])
	require.Equal(t, int64(290), vals[1])
	require.Equal(t, int64(298), vals[2])
	avg, ok = stats.Average()
	require.True(t, ok)
	require.InDelta(t, 200.5, avg, 1e-9)
}

func TestStats_Performance(t *testing.T) {
	stats := NewStatsWithClock(1*time.Second, clock.NewMock())

	// Pre-populate the tracker with 50000 data points
	for range 50000 {
		stats.Add(rand.Int63n(10000))
	}

	addDurations := make([]time.Duration, 1000)
	percentileDurations := make([]time.Duration, 1000)
	averageDurations := make([]time.Duration, 1000)
	for i := range 1000 {
		start := time.Now()
		stats.Add(rand.Int63n(1000))
		addDurations[i] = time.Since(start)

		start = time.Now()
		stats.Percentile([]float64{50, 99})
		percentileDurations[i] = time.Since(start)

		start = time.Now()
		stats.Average()
		averageDurations[i] = time.Since(start)
	}

	t.Logf("Data points: %d", stats.Len())

	// Calculate average latencies
	var totalAdd time.Duration
	for _, d := range addDurations {
		totalAdd += d
	}
	avgAdd := totalAdd / time.Duration(len(addDurations))

	var totalPercentile time.Duration
	for _, d := range percentileDurations {
		totalPercentile += d
	}
	avgPercentile := totalPercentile / time.Duration(len(percentileDurations))

	var totalAverage time.Duration
	for _, d := range averageDurations {
		totalAverage += d
	}
	avgAverage := totalAverage / time.Duration(len(averageDurations))

	t.Logf("Average Add latency: %v", avgAdd)
	t.Logf("Average Percentile latency: %v", avgPercentile)
	t.Logf("Average Average latency: %v", avgAverage)
}

func TestStatsMerge(t *testing.T) {
	clk := clock.NewMock()
	left := NewStatsWithClock(5*time.Minute, clk)
	right := NewStatsWithClock(5*time.Minute, clk)

	left.Add(10)
	clk.Add(10 * time.Millisecond)
	left.Add(20)
	right.Add(30)
	clk.Add(10 * time.Millisecond)
	right.Add(40)

	// Interleave timestamps by adding more data to both trackers.
	clk.Add(2 * time.Minute)
	left.Add(25)
	clk.Add(10 * time.Millisecond)
	right.Add(35)
	clk.Add(10 * time.Millisecond)
	left.Add(45)

	left.Merge(right)
	require.Equal(t, 7, left.Len())
	require.Equal(t, []int64{10, 20, 25, 30, 35, 40, 45}, left.Values())
	require.Equal(t, 3, right.Len())
	require.Equal(t, []int64{30, 35, 40}, right.Values())

	avg, ok := left.Average()
	require.True(t, ok)
	require.InDelta(t, 205.0/7.0, avg, 1e-9)

	vals, ok := left.Percentile([]float64{50, 75})
	require.True(t, ok)
	require.Equal(t, []int64{30, 35}, vals)

	// This should drop the first 4 data points
	clk.Add(4 * time.Minute)
	left.Add(50)
	right.Add(10)
	require.Equal(t, 4, left.Len())
	require.Equal(t, []int64{25, 35, 45, 50}, left.Values())
	require.Equal(t, 2, right.Len())
	require.Equal(t, []int64{10, 35}, right.Values())

	avg, ok = left.Average()
	require.True(t, ok)
	require.InDelta(t, 155.0/4.0, avg, 1e-9)

	vals, ok = left.Percentile([]float64{50})
	require.True(t, ok)
	require.Equal(t, []int64{35}, vals)
}
