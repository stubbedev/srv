package mcp

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// writeMu serializes mutating tool calls across every session. srv drives one
// shared edge — files under ~/.config/srv, the Traefik dynamic config, dnsmasq,
// and docker — so two concurrent mutations (a shared `srv mcp --http` daemon
// serving many clients) could corrupt that state or race the daemon's
// hot-reload. Reads are NOT serialized: at worst an inspection observes an
// in-progress mutation, which is acceptable and keeps reads from blocking
// behind a long-running or interactively-confirmed write.
var writeMu sync.Mutex

// writeToolSet indexes the write tier for O(1) lookup in the middleware. Built
// from writeToolNames (the activation source of truth) so the two never drift.
var writeToolSet = func() map[string]bool {
	m := make(map[string]bool, len(writeToolNames))
	for _, n := range writeToolNames {
		m[n] = true
	}
	return m
}()

func isWriteTool(name string) bool { return writeToolSet[name] }

// toolTimeout bounds a single tool call. Because mutations hold writeMu, a hung
// call (a wedged docker or traefik operation) would otherwise block every other
// client's mutations — and the whole daemon — indefinitely. The timeout caps
// that blast radius. It is generous so interactive confirmations have time to
// be answered. 0 disables it. Set once at startup via SetToolTimeout.
var toolTimeout = 10 * time.Minute

// SetToolTimeout overrides the per-tool-call timeout. d <= 0 disables the
// timeout. Call before Serve/ServeHTTP; it is not safe to change while serving.
func SetToolTimeout(d time.Duration) {
	if d < 0 {
		d = 0
	}
	toolTimeout = d
}

// newToolMiddleware builds the receiving middleware for one server (one
// session, since the HTTP handler creates a fresh server per session). It:
//
//   - resolves the caller's workspace root BEFORE taking writeMu, so a
//     roots/list round-trip to a slow client never blocks another client's
//     mutation, and stashes it on the context for path-bearing tools;
//   - bounds the call with toolTimeout so a hung mutation can't wedge the
//     shared lock forever;
//   - recovers panics into errors so one client's bad input cannot crash the
//     daemon and drop every other client;
//   - serializes write-tier tools through writeMu.
//
// The workspace cache lives in this closure, so it is scoped to the session and
// garbage-collected with the server — no global map to reap.
func newToolMiddleware() mcpsdk.Middleware {
	ws := &sessionWorkspace{}
	return func(next mcpsdk.MethodHandler) mcpsdk.MethodHandler {
		return func(ctx context.Context, method string, req mcpsdk.Request) (result mcpsdk.Result, err error) {
			if method != "tools/call" {
				return next(ctx, method, req)
			}

			name := ""
			if p, ok := req.GetParams().(*mcpsdk.CallToolParamsRaw); ok {
				name = p.Name
			}

			// Resolve workspace context off the lock and stash it for the handler.
			var header http.Header
			if ex := req.GetExtra(); ex != nil {
				header = ex.Header
			}
			sess, _ := req.GetSession().(*mcpsdk.ServerSession)
			ctx = context.WithValue(ctx, wsRootKey{}, ws.resolve(ctx, header, sess))

			if toolTimeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, toolTimeout)
				defer cancel()
			}

			// Deferred recover catches a panic from next() regardless of the
			// other defers' order; writeMu's deferred Unlock still runs first, so
			// the lock is released even on panic.
			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("tool %q panicked: %v", name, r)
					result = nil
				}
			}()

			if isWriteTool(name) {
				writeMu.Lock()
				defer writeMu.Unlock()
			}
			return next(ctx, method, req)
		}
	}
}
