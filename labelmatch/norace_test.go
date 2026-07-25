//go:build !race

package labelmatch

// raceEnabled reports whether the race detector is active. It instruments
// allocation, so testing.AllocsPerRun results are not meaningful under it.
const raceEnabled = false
