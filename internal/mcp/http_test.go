package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// newTestHTTPServer stands up the same handler stack ServeHTTP uses — fresh
// server per session, wrapped in stdlib cross-origin protection — behind an
// httptest server, returning its URL. Wrapping with cross-origin protection
// guards that the protection does not reject a well-behaved (Origin-less) MCP
// client.
func newTestHTTPServer(t *testing.T) string {
	t.Helper()
	handler := mcpsdk.NewStreamableHTTPHandler(
		func(*http.Request) *mcpsdk.Server { return newServer() },
		&mcpsdk.StreamableHTTPOptions{SessionTimeout: time.Minute},
	)
	ts := httptest.NewServer(http.NewCrossOriginProtection().Handler(handler))
	t.Cleanup(ts.Close)
	return ts.URL
}

func connectHTTPClient(t *testing.T, endpoint string) (*mcpsdk.ClientSession, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "test"}, nil)
	cs, err := client.Connect(ctx, &mcpsdk.StreamableClientTransport{Endpoint: endpoint}, nil)
	if err != nil {
		t.Fatalf("http client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs, ctx
}

// TestHTTPTransportInitializeAndActivate drives the full HTTP path: initialize
// over Streamable HTTP, see only the core tools, then srv_activate to unlock the
// read tier and confirm the client re-lists the new tools.
func TestHTTPTransportInitializeAndActivate(t *testing.T) {
	cs, ctx := connectHTTPClient(t, newTestHTTPServer(t))

	assertExactly(t, advertisedTools(ctx, t, cs), coreToolNames)

	if _, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "srv_activate",
		Arguments: map[string]any{"group": "read"},
	}); err != nil {
		t.Fatalf("activate read over http: %v", err)
	}
	assertExactly(t, advertisedTools(ctx, t, cs), concat(coreToolNames, readToolNames))
}

// TestHTTPSessionsAreIsolated asserts each session gets its own lazy-activation
// state: activating one client's read tier must not leak into another's surface.
func TestHTTPSessionsAreIsolated(t *testing.T) {
	url := newTestHTTPServer(t)
	a, ctxA := connectHTTPClient(t, url)
	b, ctxB := connectHTTPClient(t, url)

	if _, err := a.CallTool(ctxA, &mcpsdk.CallToolParams{
		Name:      "srv_activate",
		Arguments: map[string]any{"group": "read"},
	}); err != nil {
		t.Fatalf("activate read on client A: %v", err)
	}

	assertExactly(t, advertisedTools(ctxA, t, a), concat(coreToolNames, readToolNames))
	// Client B never activated — still core-only.
	assertExactly(t, advertisedTools(ctxB, t, b), coreToolNames)
}

func TestHealthzEndpoint(t *testing.T) {
	// Build the same mux ServeHTTP serves and probe /healthz.
	handler := mcpsdk.NewStreamableHTTPHandler(
		func(*http.Request) *mcpsdk.Server { return newServer() }, nil)
	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/healthz status = %d, want 200", resp.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode /healthz body: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("/healthz body = %v, want status=ok", body)
	}
}
