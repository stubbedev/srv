package daemon

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// scheduleReload uses one timer per site and coalesces multiple events within
// the debounce window into a single fire. Verify that 5 rapid events trigger
// the callback exactly once.
func TestScheduleReload_Debounces(t *testing.T) {
	s := &watchState{timers: make(map[string]*time.Timer)}

	var fires int32
	var wg sync.WaitGroup
	wg.Add(1)
	fire := func() {
		atomic.AddInt32(&fires, 1)
		wg.Done()
	}

	delay := 50 * time.Millisecond
	for i := 0; i < 5; i++ {
		s.scheduleReload("kontainer", delay, fire)
		time.Sleep(5 * time.Millisecond) // well inside debounce window
	}

	wg.Wait()
	// Wait a bit past the debounce window to ensure no stray late fires.
	time.Sleep(2 * delay)
	if got := atomic.LoadInt32(&fires); got != 1 {
		t.Errorf("expected exactly 1 fire after coalescing, got %d", got)
	}
}

// Events for different sites fire independently.
func TestScheduleReload_PerSite(t *testing.T) {
	s := &watchState{timers: make(map[string]*time.Timer)}

	var fires int32
	delay := 30 * time.Millisecond
	var wg sync.WaitGroup
	wg.Add(2)
	fire := func() {
		atomic.AddInt32(&fires, 1)
		wg.Done()
	}

	s.scheduleReload("alpha", delay, fire)
	s.scheduleReload("beta", delay, fire)

	wg.Wait()
	if got := atomic.LoadInt32(&fires); got != 2 {
		t.Errorf("expected 2 independent fires, got %d", got)
	}
}
