package mcp

import (
	"context"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// connectClient spins up newServer() over an in-memory transport and returns a
// connected client session for introspecting the live tool surface.
func connectClient(t *testing.T) (*mcpsdk.ClientSession, context.Context) {
	t.Helper()
	srv := newServer()
	if srv == nil {
		t.Fatal("newServer returned nil")
	}

	serverTransport, clientTransport := mcpsdk.NewInMemoryTransports()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	serverSession, err := srv.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "test"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })

	return clientSession, ctx
}

// advertisedTools lists the tool names the server currently advertises,
// failing on any duplicate (which would mean a double registration).
func advertisedTools(t *testing.T, cs *mcpsdk.ClientSession, ctx context.Context) map[string]bool {
	t.Helper()
	res, err := cs.ListTools(ctx, &mcpsdk.ListToolsParams{})
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	got := make(map[string]bool, len(res.Tools))
	for _, tool := range res.Tools {
		if got[tool.Name] {
			t.Errorf("tool %q registered more than once (duplicate)", tool.Name)
		}
		got[tool.Name] = true
	}
	return got
}

// assertExactly fails unless got holds exactly the named tools.
func assertExactly(t *testing.T, got map[string]bool, want []string) {
	t.Helper()
	wantSet := make(map[string]bool, len(want))
	for _, n := range want {
		wantSet[n] = true
		if !got[n] {
			t.Errorf("missing tool %q in advertised set", n)
		}
	}
	for n := range got {
		if !wantSet[n] {
			t.Errorf("unexpected tool %q advertised", n)
		}
	}
}

// concat flattens name lists into one slice.
func concat(lists ...[]string) []string {
	var out []string
	for _, l := range lists {
		out = append(out, l...)
	}
	return out
}

// TestInitialSurfaceIsCoreOnly asserts that before any activation the server
// advertises only the two core tools — the whole point of lazy-loading is that
// the read/write tiers cost no context until requested.
func TestInitialSurfaceIsCoreOnly(t *testing.T) {
	cs, ctx := connectClient(t)
	assertExactly(t, advertisedTools(t, cs, ctx), coreToolNames)
}

// TestActivateRegistersTiers drives srv_activate and asserts each tier appears
// exactly once, cumulatively, with "write" implying "read".
func TestActivateRegistersTiers(t *testing.T) {
	cs, ctx := connectClient(t)

	// read tier: core + read.
	if _, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "srv_activate",
		Arguments: map[string]any{"group": "read"},
	}); err != nil {
		t.Fatalf("activate read: %v", err)
	}
	assertExactly(t, advertisedTools(t, cs, ctx), concat(coreToolNames, readToolNames))

	// write tier: core + read + write (write implies read; read tools must not
	// be re-registered as duplicates).
	if _, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "srv_activate",
		Arguments: map[string]any{"group": "write"},
	}); err != nil {
		t.Fatalf("activate write: %v", err)
	}
	assertExactly(t, advertisedTools(t, cs, ctx), concat(coreToolNames, readToolNames, writeToolNames))

	// Idempotent: re-activating write changes nothing.
	if _, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "srv_activate",
		Arguments: map[string]any{"group": "write"},
	}); err != nil {
		t.Fatalf("activate write again: %v", err)
	}
	assertExactly(t, advertisedTools(t, cs, ctx), concat(coreToolNames, readToolNames, writeToolNames))
}

// TestActivateWriteImpliesRead asserts a bare write activation (no prior read)
// registers the read tier too.
func TestActivateWriteImpliesRead(t *testing.T) {
	cs, ctx := connectClient(t)
	if _, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "srv_activate",
		Arguments: map[string]any{"group": "write"},
	}); err != nil {
		t.Fatalf("activate write: %v", err)
	}
	assertExactly(t, advertisedTools(t, cs, ctx), concat(coreToolNames, readToolNames, writeToolNames))
}

func TestSetVersionUpdatesAdvertisedString(t *testing.T) {
	// SetVersion is sticky for the package's lifetime, so capture the
	// current value and restore it via t.Cleanup to avoid leaking state
	// into TestNewServerRegistersTools or other parallel tests.
	prev := version
	t.Cleanup(func() { version = prev })

	SetVersion("v9.9.9-test")
	if version != "v9.9.9-test" {
		t.Errorf("SetVersion did not stick: %q", version)
	}

	// Empty string is treated as a no-op so callers can pass an unset
	// flag without clobbering the embedded default.
	SetVersion("")
	if version != "v9.9.9-test" {
		t.Errorf("empty SetVersion clobbered value: %q", version)
	}
}
