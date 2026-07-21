package sorted_list

import (
	"math/rand"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSortedListInsertAndGetByIndex(t *testing.T) {
	sl := NewSortedList()
	sl.Insert(10)
	sl.Insert(20)
	sl.Insert(5)
	sl.Insert(10) // duplicate
	sl.Insert(15)

	require.Equal(t, 5, sl.Len())

	expected := []int64{5, 10, 10, 15, 20}
	for i, key := range expected {
		value, ok := sl.GetByIndex(i)
		require.True(t, ok)
		require.Equal(t, key, value)
	}

	_, ok := sl.GetByIndex(len(expected))
	require.False(t, ok)
}

func TestSortedListDelete(t *testing.T) {
	sl := NewSortedList()
	sl.Insert(10)
	sl.Insert(10)
	sl.Insert(5)
	sl.Insert(20)

	sl.Delete(10)
	require.Equal(t, 3, sl.Len())

	value, ok := sl.GetByIndex(0)
	require.True(t, ok)
	require.Equal(t, int64(5), value)

	value, ok = sl.GetByIndex(1)
	require.True(t, ok)
	require.Equal(t, int64(10), value)

	sl.Delete(10)
	require.Equal(t, 2, sl.Len())
	value, ok = sl.GetByIndex(1)
	require.True(t, ok)
	require.Equal(t, int64(20), value)

	sl.Delete(42) // no-op
	require.Equal(t, 2, sl.Len())
}

func TestSortedListRandomized(t *testing.T) {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	sl := NewSortedList()
	values := make([]int64, 0, 1000)

	for range 1000 {
		v := int64(r.Intn(100))
		sl.Insert(v)
		values = append(values, v)
	}

	for range 200 {
		if len(values) == 0 {
			break
		}
		idx := r.Intn(len(values))
		value := values[idx]
		sl.Delete(value)
		values = append(values[:idx], values[idx+1:]...)
	}

	require.Equal(t, len(values), sl.Len())
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	for i, expected := range values {
		actual, ok := sl.GetByIndex(i)
		require.True(t, ok)
		require.Equal(t, expected, actual)
	}
}

func TestSortedListMerge(t *testing.T) {
	left := NewSortedList()
	left.Insert(10)
	left.Insert(5)
	left.Insert(10)

	right := NewSortedList()
	right.Insert(7)
	right.Insert(10)
	right.Insert(15)

	left.Merge(right)
	require.Equal(t, 6, left.Len())

	expected := []int64{5, 7, 10, 10, 10, 15}
	for i, key := range expected {
		value, ok := left.GetByIndex(i)
		require.True(t, ok)
		require.Equal(t, key, value)
	}

	require.Equal(t, 3, right.Len())
}

func TestSortedListKeys(t *testing.T) {
	sl := NewSortedList()
	require.Empty(t, sl.Keys())

	values := []int64{10, 5, 10, 7, 7, 20}
	for _, v := range values {
		sl.Insert(v)
	}

	require.Equal(t, []int64{5, 7, 7, 10, 10, 20}, sl.Keys())
}
