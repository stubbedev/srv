package daemon

import (
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// scheduleReload uses one timer per site and coalesces multiple events within
// the debounce window into a single fire. Verify that 5 rapid events trigger
// the callback exactly once.
//
// Run inside a synctest bubble: the clock is fake, so the debounce window
// elapses instantly and deterministically. The old version slept for real and
// asserted on a WaitGroup, which made it both slow and racy — a loaded machine
// could let a "stray late fire" land after the assertion rather than before it.
func TestScheduleReload_Debounces(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := &watchState{timers: make(map[string]*time.Timer)}

		var fires atomic.Int32
		fire := func() { fires.Add(1) }

		delay := 50 * time.Millisecond
		for range 5 {
			s.scheduleReload("myapp", delay, fire)
			time.Sleep(5 * time.Millisecond) // well inside the debounce window
		}

		// Past the window, so the coalesced fire has happened and any stray
		// late fire would have too. synctest.Wait blocks until every goroutine
		// in the bubble is idle, so this is an exact observation, not a guess.
		time.Sleep(2 * delay)
		synctest.Wait()

		if got := fires.Load(); got != 1 {
			t.Errorf("expected exactly 1 fire after coalescing, got %d", got)
		}
	})
}

// Events for different sites fire independently.
func TestScheduleReload_PerSite(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := &watchState{timers: make(map[string]*time.Timer)}

		var fires atomic.Int32
		fire := func() { fires.Add(1) }

		delay := 30 * time.Millisecond
		s.scheduleReload("alpha", delay, fire)
		s.scheduleReload("beta", delay, fire)

		time.Sleep(2 * delay)
		synctest.Wait()

		if got := fires.Load(); got != 2 {
			t.Errorf("expected 2 independent fires, got %d", got)
		}
	})
}
