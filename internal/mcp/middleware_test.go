package mcp

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeParams is a minimal mcpsdk.Params for driving the middleware directly.
// CallToolParamsRaw carries the tool name the middleware switches on.
func callParams(name string) *mcpsdk.CallToolParamsRaw {
	return &mcpsdk.CallToolParamsRaw{Name: name}
}

// fakeRequest implements mcpsdk.Request for unit-testing the middleware without
// a live transport. Only the methods the middleware uses return meaningful
// values; the unexported interface methods are never called on this path.
type fakeRequest struct {
	mcpsdk.Request
	params mcpsdk.Params
}

func (f fakeRequest) GetParams() mcpsdk.Params       { return f.params }
func (f fakeRequest) GetSession() mcpsdk.Session     { return nil }
func (f fakeRequest) GetExtra() *mcpsdk.RequestExtra { return nil }

func TestWriteToolSetMatchesWriteTier(t *testing.T) {
	// Every write-tier tool name must be recognized as a write, and no read or
	// core tool may be. Guards against the set drifting from writeToolNames.
	for _, n := range writeToolNames {
		if !isWriteTool(n) {
			t.Errorf("writeToolNames entry %q not seen as a write tool", n)
		}
	}
	for _, n := range append(append([]string{}, readToolNames...), coreToolNames...) {
		if isWriteTool(n) {
			t.Errorf("non-write tool %q wrongly classified as a write", n)
		}
	}
}

func TestMiddlewareSerializesWritesNotReads(t *testing.T) {
	mw := newToolMiddleware()

	// A write handler that records max concurrency. With writeMu it must never
	// see more than one in-flight call.
	var mu sync.Mutex
	var inFlight, maxInFlight int
	enter := func() {
		mu.Lock()
		inFlight++
		if inFlight > maxInFlight {
			maxInFlight = inFlight
		}
		mu.Unlock()
	}
	leave := func() { mu.Lock(); inFlight--; mu.Unlock() }

	handler := mw(func(_ context.Context, _ string, _ mcpsdk.Request) (mcpsdk.Result, error) {
		enter()
		time.Sleep(20 * time.Millisecond)
		leave()
		return &mcpsdk.CallToolResult{}, nil
	})

	call := func(name string) {
		_, _ = handler(context.Background(), "tools/call", fakeRequest{params: callParams(name)})
	}

	// Writes: a real write-tier tool name → serialized.
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); call("add_site") }()
	}
	wg.Wait()
	if maxInFlight != 1 {
		t.Errorf("write calls not serialized: max in-flight = %d, want 1", maxInFlight)
	}

	// Reads: concurrency allowed.
	mu.Lock()
	maxInFlight = 0
	mu.Unlock()
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); call("list_sites") }()
	}
	wg.Wait()
	if maxInFlight < 2 {
		t.Errorf("read calls were serialized: max in-flight = %d, want > 1", maxInFlight)
	}
}

func TestMiddlewareRecoversPanic(t *testing.T) {
	mw := newToolMiddleware()
	handler := mw(func(_ context.Context, _ string, _ mcpsdk.Request) (mcpsdk.Result, error) {
		panic("boom")
	})

	res, err := handler(context.Background(), "tools/call", fakeRequest{params: callParams("add_site")})
	if err == nil {
		t.Fatal("expected panic to surface as an error")
	}
	if res != nil {
		t.Errorf("expected nil result on panic, got %v", res)
	}
	if !strings.Contains(err.Error(), "panicked") {
		t.Errorf("error should mention the panic, got %q", err)
	}

	// The write lock must have been released despite the panic — a second call
	// completes rather than deadlocking.
	done := make(chan struct{})
	go func() {
		_, _ = mw(func(_ context.Context, _ string, _ mcpsdk.Request) (mcpsdk.Result, error) {
			return &mcpsdk.CallToolResult{}, nil
		})(context.Background(), "tools/call", fakeRequest{params: callParams("add_site")})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("write lock not released after panic — deadlock")
	}
}

func TestMiddlewareToolTimeout(t *testing.T) {
	old := toolTimeout
	SetToolTimeout(30 * time.Millisecond)
	defer SetToolTimeout(old)

	mw := newToolMiddleware()
	handler := mw(func(ctx context.Context, _ string, _ mcpsdk.Request) (mcpsdk.Result, error) {
		<-ctx.Done() // a hung tool that only the timeout can free
		return nil, ctx.Err()
	})

	start := time.Now()
	_, err := handler(context.Background(), "tools/call", fakeRequest{params: callParams("add_site")})
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("timeout did not fire promptly: %v", elapsed)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected deadline-exceeded, got %v", err)
	}
}

func TestSetToolTimeoutClampsNegative(t *testing.T) {
	old := toolTimeout
	defer func() { toolTimeout = old }()
	SetToolTimeout(-5 * time.Second)
	if toolTimeout != 0 {
		t.Errorf("negative timeout should clamp to 0 (disabled), got %v", toolTimeout)
	}
}

func TestMiddlewarePassesThroughNonToolCalls(t *testing.T) {
	mw := newToolMiddleware()
	called := false
	handler := mw(func(_ context.Context, method string, _ mcpsdk.Request) (mcpsdk.Result, error) {
		called = true
		if method != "tools/list" {
			t.Errorf("method = %q", method)
		}
		return nil, nil
	})
	if _, err := handler(context.Background(), "tools/list", fakeRequest{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("non-tool RPC was not passed through")
	}
}
