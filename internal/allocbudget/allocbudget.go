// Package allocbudget checks that a hot path stays within an allocation
// budget, in tests.
//
// testing.AllocsPerRun reads process-wide counters, so goroutines still
// winding down from earlier tests (servers shutting down, timers firing)
// land in its count on a loaded machine. Such noise only ever adds, so the
// minimum over several rounds is the path's true cost.
//
// When go test may cache a result it runs the binary with -test.testlogfile,
// and package os then records every file it opens — one allocation per open
// that production never pays. A Budget declares its file opens so Check
// adds exactly that overhead back, and only while the log is on.
package allocbudget

import (
	"flag"
	"testing"
)

// Budget is a hot path's allocation allowance per call.
type Budget struct {
	// Allocs is the production cost.
	Allocs float64
	// FileOpens is how many files the path opens per call (os.Open,
	// os.Root.Open, …); each costs one more allocation under the test log.
	FileOpens int
}

// limit is the allowance in this test process.
func (b Budget) limit() float64 {
	if testLogActive() {
		return b.Allocs + float64(b.FileOpens)
	}
	return b.Allocs
}

// testLogActive reports whether go test is recording file access for its
// result cache.
func testLogActive() bool {
	f := flag.Lookup("test.testlogfile")
	return f != nil && f.Value.String() != ""
}

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

// Check fails t when f allocates more than budget allows per call. Under
// -race it does nothing.
func Check(t testing.TB, name string, budget Budget, f func()) {
	t.Helper()
	got, ok := Measure(200, f)
	if ok && got > budget.limit() {
		t.Errorf("%s: %v allocs per call, budget %v (%d file opens)", name, got, budget.Allocs, budget.FileOpens)
	}
}
