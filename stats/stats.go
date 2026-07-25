package stats

import (
	"sync"
	"time"

	"github.com/benbjohnson/clock"
	"github.com/pliu/go-utils/sorted_list"
)

// Stats tracks numeric values in a sliding window and can produce summaries.
//
// Values are held twice: in insertion order by window, so the oldest can be
// expired, and in sorted order by values, so percentiles are a rank lookup
// rather than a sort.
type Stats struct {
	mu         sync.Mutex
	values     *sorted_list.SortedList
	window     ring
	windowSize time.Duration
	clock      clock.Clock
	sum        int64
}

func NewStats(windowSize time.Duration) *Stats {
	return NewStatsWithClock(windowSize, clock.New())
}

func NewStatsWithClock(windowSize time.Duration, clk clock.Clock) *Stats {
	return &Stats{
		values:     sorted_list.NewSortedList(),
		windowSize: windowSize,
		clock:      clk,
	}
}

type measurement struct {
	timestamp time.Time
	value     int64
}

func (s *Stats) Add(value int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.clock.Now()
	s.values.Insert(value)
	s.window.push(measurement{timestamp: now, value: value})
	s.sum += value
	s.cleanup(now)
}

func (s *Stats) Average() (float64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanup(s.clock.Now())

	count := s.values.Len()
	if count == 0 {
		return 0, false
	}
	return float64(s.sum) / float64(count), true
}

func (s *Stats) Percentile(percentiles []float64) ([]int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanup(s.clock.Now())
	if len(percentiles) == 0 {
		return nil, false
	}
	for _, p := range percentiles {
		if p < 0 || p > 100 {
			return nil, false
		}
	}

	count := s.values.Len()
	if count == 0 {
		return nil, false
	}

	results := make([]int64, 0, len(percentiles))
	for _, p := range percentiles {
		index := int(float64(count-1) * (p / 100.0))
		key, ok := s.values.GetByIndex(index)
		if !ok {
			return nil, false
		}
		results = append(results, key)
	}
	return results, true
}

func (s *Stats) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanup(s.clock.Now())
	return s.values.Len()
}

func (s *Stats) Values() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanup(s.clock.Now())

	if s.values == nil || s.values.Len() == 0 {
		return []int64{}
	}
	return s.values.Keys()
}

func (s *Stats) Merge(other *Stats) {
	if other == nil {
		return
	}
	if s == other {
		return
	}

	// Grab other's state by copying its sorted list.
	other.mu.Lock()
	otherLen := other.values.Len()
	if otherLen == 0 {
		other.mu.Unlock()
		return
	}
	tmpValues := sorted_list.NewSortedList()
	tmpValues.Merge(other.values)
	otherSum := other.sum
	measurements := make([]measurement, 0, other.window.len())
	for i := range other.window.len() {
		measurements = append(measurements, other.window.at(i))
	}
	other.mu.Unlock()

	s.mu.Lock()
	s.values.Merge(tmpValues)
	s.sum += otherSum
	s.mergeMeasurements(measurements)
	s.cleanup(s.clock.Now())
	s.mu.Unlock()
}

// mergeMeasurements splices ms into the window, keeping it ordered by
// timestamp. Both inputs are already sorted, so this is a single linear
// merge; unlike Add it is not allocation-free, but merging is a rare
// bulk operation rather than the hot path.
func (s *Stats) mergeMeasurements(ms []measurement) {
	if len(ms) == 0 {
		return
	}
	if s.window.len() == 0 {
		for _, m := range ms {
			s.window.push(m)
		}
		return
	}

	merged := make([]measurement, 0, s.window.len()+len(ms))
	i, j := 0, 0
	for j < s.window.len() {
		existing := s.window.at(j)
		if i < len(ms) && ms[i].timestamp.Before(existing.timestamp) {
			merged = append(merged, ms[i])
			i++
			continue
		}
		merged = append(merged, existing)
		j++
	}
	merged = append(merged, ms[i:]...)

	s.window.reset()
	for _, m := range merged {
		s.window.push(m)
	}
}

// cleanup removes measurements that are older than the window size.
func (s *Stats) cleanup(now time.Time) {
	for {
		m, ok := s.window.front()
		if !ok || now.Sub(m.timestamp) <= s.windowSize {
			// The window is ordered by time, so the first live measurement
			// means everything behind it is live too.
			return
		}
		s.values.Delete(m.value)
		s.sum -= m.value
		s.window.pop()
	}
}
