package set

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSet_Add_Contains_Remove_Values(t *testing.T) {
	s := NewSet[int]()

	// Test Add and Contains
	s.Add(1)
	s.Add(2)
	s.Add(3)

	require.True(t, s.Contains(1))
	require.True(t, s.Contains(2))
	require.True(t, s.Contains(3))
	require.False(t, s.Contains(4)) // Non-existent element

	// Test Values helper
	slice := s.Items()
	require.Len(t, slice, 3)
	valsFromSlice := make(map[int]struct{}, len(slice))
	for _, v := range slice {
		valsFromSlice[v] = struct{}{}
	}
	_, ok := valsFromSlice[1]
	require.True(t, ok)
	_, ok = valsFromSlice[2]
	require.True(t, ok)
	_, ok = valsFromSlice[3]
	require.True(t, ok)

	// Test Remove
	s.Remove(2)
	require.False(t, s.Contains(2)) // Should no longer contain 2
	require.True(t, s.Contains(1))
	require.True(t, s.Contains(3))
	require.False(t, s.Contains(4))

	// Test Values helper after removal
	slice = s.Items()
	require.Len(t, slice, 2)
	valsFromSlice = make(map[int]struct{}, len(slice))
	for _, v := range slice {
		valsFromSlice[v] = struct{}{}
	}
	_, ok = valsFromSlice[1]
	require.True(t, ok)
	_, ok = valsFromSlice[3]
	require.True(t, ok)
	_, ok = valsFromSlice[2]
	require.False(t, ok)

	// Test removing non-existent element
	s.Remove(999) // Should not panic
	require.Equal(t, 2, s.Len())

	// Test adding duplicate element (should not increase size)
	s.Add(1) // 1 already exists
	require.Equal(t, 2, s.Len())
}

func TestSet_Equals(t *testing.T) {
	// Test with empty sets
	s1 := NewSet[int]()
	s2 := NewSet[int]()
	require.True(t, s1.Equals(s2))
	require.True(t, s2.Equals(s1))

	// Test with one empty, one non-empty
	s3 := NewSet[int]()
	s3.Add(1)
	require.False(t, s1.Equals(s3))
	require.False(t, s3.Equals(s1))

	// Test with same elements
	s4 := NewSet[int]()
	s4.Add(1)
	s4.Add(2)
	s4.Add(3)

	s5 := NewSet[int]()
	s5.Add(3)
	s5.Add(1)
	s5.Add(2)

	require.True(t, s4.Equals(s5))
	require.True(t, s5.Equals(s4))

	// Test with different elements
	s6 := NewSet[int]()
	s6.Add(1)
	s6.Add(2)
	s6.Add(4)

	require.False(t, s4.Equals(s6))
	require.False(t, s6.Equals(s4))

	// Test with same elements but different counts (sets ignore duplicates)
	s7 := NewSet[string]()
	s7.Add("a")
	s7.Add("b")

	s8 := NewSet[string]()
	s8.Add("a")
	s8.Add("b")
	s8.Add("a") // This doesn't add a duplicate

	require.True(t, s7.Equals(s8))
	require.True(t, s8.Equals(s7))

	// Test with nil other
	require.False(t, s4.Equals(nil))

	// Test with empty set vs nil
	emptySet := NewSet[int]()
	require.False(t, emptySet.Equals(nil))
}
