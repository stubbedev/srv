package mcp

import (
	"context"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestNewServerRegistersTools spins up the server over an in-memory
// transport, asks for the tool list, and asserts the read tools surface
// at minimum is present. Keeps the assertion broad (set of names) so
// adding a new tool doesn't break the test, but removing or renaming
// an existing one does.
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

	want := map[string]bool{
		"version":         true,
		"paths":           true,
		"list_sites":      true,
		"get_site":        true,
		"validate_site":   true,
		"list_proxies":    true,
		"get_proxy":       true,
		"list_redirects":  true,
		"reload_site":     true,
	}

	got := make(map[string]bool, len(res.Tools))
	for _, tool := range res.Tools {
		got[tool.Name] = true
	}
	for name := range want {
		if !got[name] {
			t.Errorf("missing tool %q in registered set %v", name, got)
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
