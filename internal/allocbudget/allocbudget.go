// Package allocbudget checks that a hot path stays within an allocation
// budget, in tests.
//
// testing.AllocsPerRun reads process-wide counters, so goroutines still
// winding down from earlier tests (servers shutting down, timers firing)
// land in its count on a loaded machine. Such noise only ever adds, so the
// minimum over several rounds is the path's true cost.
package allocbudget

import "testing"

// rounds is how many AllocsPerRun measurements Check takes the minimum of.
const rounds = 5

// Measure returns f's allocations per call: the minimum over several
// AllocsPerRun rounds of runs calls each. The bool is false under -race,
// where counts are meaningless.
func Measure(runs int, f func()) (float64, bool) {
	if raceEnabled {
		return 0, false
	}
	best := testing.AllocsPerRun(runs, f)
	for range rounds - 1 {
		best = min(best, testing.AllocsPerRun(runs, f))
	}
	return best, true
}

// Check fails t when f allocates more than budget per call. Under -race it
// does nothing.
func Check(t testing.TB, name string, budget float64, f func()) {
	t.Helper()
	got, ok := Measure(200, f)
	if ok && got > budget {
		t.Errorf("%s: %v allocs per call, budget %v", name, got, budget)
	}
}
