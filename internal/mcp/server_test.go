package mcp

import (
	"context"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// wantTools is the authoritative, exact set of MCP tools srv advertises.
// TestNewServerRegistersTools asserts the advertised set matches this exactly —
// adding, removing, renaming, or duplicating a tool without updating this list
// fails the test, keeping the surface honest (mirrors treeman's guard test).
var wantTools = []string{
	// read
	"version", "paths", "list_sites", "get_site", "validate_site",
	"list_proxies", "get_proxy", "list_redirects",
	// diagnostics
	"daemon_status", "daemon_log", "metrics_status",
	// write
	"reload_site",
	"add_proxy", "remove_proxy",
	"add_redirect", "remove_redirect",
	// site lifecycle + mutators
	"start_site", "stop_site", "restart_site", "remove_site",
	"add_alias", "remove_alias", "set_internal_listener",
	"add_volume", "remove_volume",
}

// TestNewServerRegistersTools spins up the server over an in-memory transport,
// lists the advertised tools, and asserts the set equals wantTools exactly.
func TestNewServerRegistersTools(t *testing.T) {
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

	res, err := clientSession.ListTools(ctx, &mcpsdk.ListToolsParams{})
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}

	want := make(map[string]bool, len(wantTools))
	for _, n := range wantTools {
		want[n] = true
	}

	got := make(map[string]int, len(res.Tools))
	for _, tool := range res.Tools {
		got[tool.Name]++
	}
	for name, count := range got {
		if count > 1 {
			t.Errorf("tool %q registered %d times (duplicate)", name, count)
		}
		if !want[name] {
			t.Errorf("unexpected tool %q advertised — add it to wantTools or remove the registration", name)
		}
	}
	for name := range want {
		if got[name] == 0 {
			t.Errorf("missing tool %q in advertised set", name)
		}
	}
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
