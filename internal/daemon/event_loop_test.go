package daemon

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/engine"
	"github.com/stubbedev/srv/internal/ops"
)

// swapEventStream feeds runEventLoop the given channels instead of a live
// Docker event stream.
func swapEventStream(t *testing.T, eventCh <-chan docker.Event, errCh <-chan error) {
	t.Helper()
	t.Cleanup(ops.SwapEngine(engine.For(engine.Docker)))
	prev := watchDockerEvents
	watchDockerEvents = func(context.Context) (<-chan docker.Event, <-chan error, error) {
		return eventCh, errCh, nil
	}
	t.Cleanup(func() { watchDockerEvents = prev })
}

// runEventLoopWithin runs one event-loop session and fails the test when it
// has not returned within the bound. A loop that keeps reading a closed
// channel never returns: it spins on zero-value events at 100% CPU.
func runEventLoopWithin(t *testing.T, d *Daemon, bound time.Duration) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- d.runEventLoop() }()
	select {
	case err := <-done:
		return err
	case <-time.After(bound):
		d.cancel() // unwind the spinning loop so the test binary can exit
		<-done
		t.Fatalf("runEventLoop still running %v after the event stream closed (busy loop on a closed channel)", bound)
		return nil
	}
}

// TestRunEventLoopReturnsWhenStreamClosesCleanly is the regression test for
// the daemon pinning a core after a Docker restart: the engine ends the
// event stream with a clean EOF, the stream closes its channel without an
// error, and the loop must hand back to watchEvents to reconnect.
func TestRunEventLoopReturnsWhenStreamClosesCleanly(t *testing.T) {
	d, err := newDaemonForTest(t)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.cancel)
	eventCh := make(chan docker.Event)
	close(eventCh)
	swapEventStream(t, eventCh, make(chan error, 1))

	err = runEventLoopWithin(t, d, time.Second)
	if !errors.Is(err, docker.ErrEventStreamClosed) {
		t.Errorf("err = %v, want ErrEventStreamClosed", err)
	}
}

// A stream that fails sends its error and then closes the event channel;
// whichever the select sees first, the loop must return that error.
func TestRunEventLoopReturnsStreamError(t *testing.T) {
	d, err := newDaemonForTest(t)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.cancel)
	streamErr := errors.New("unexpected EOF")
	for range 50 { // both select arms are ready; cover either being picked
		eventCh := make(chan docker.Event)
		errCh := make(chan error, 1)
		errCh <- streamErr
		close(eventCh)
		swapEventStream(t, eventCh, errCh)

		if err := runEventLoopWithin(t, d, time.Second); !errors.Is(err, streamErr) {
			t.Fatalf("err = %v, want the stream's error", err)
		}
	}
}

// Events that arrive before the stream ends are still dispatched.
func TestRunEventLoopHandlesEventsThenReturns(t *testing.T) {
	d, err := newDaemonForTest(t)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.cancel)
	eventCh := make(chan docker.Event, 3)
	for range 3 {
		eventCh <- docker.Event{Actor: docker.EventActor{Attributes: map[string]string{"name": "untracked"}}}
	}
	close(eventCh)
	swapEventStream(t, eventCh, make(chan error, 1))

	if err := runEventLoopWithin(t, d, time.Second); !errors.Is(err, docker.ErrEventStreamClosed) {
		t.Errorf("err = %v, want ErrEventStreamClosed", err)
	}
	if len(eventCh) != 0 {
		t.Errorf("%d events left unread", len(eventCh))
	}
}
