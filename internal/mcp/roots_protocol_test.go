package mcp

import (
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestRootsAllowedProtocolGate pins where roots stop being askable. MCP
// 2026-07-28 (SEP-2322/2575) forbids server-initiated requests, so roots/list
// is refused from that revision on and the workspace has to arrive by header or
// tool argument instead. Getting this boundary wrong means either a pointless
// failing round-trip on every call, or dropping roots for clients that still
// answer them.
func TestRootsAllowedProtocolGate(t *testing.T) {
	caps := &mcpsdk.ClientCapabilities{RootsV2: &mcpsdk.RootCapabilities{}}
	for ver, want := range map[string]bool{
		"2024-11-05": true,
		"2025-06-18": true,
		"2025-11-25": true,
		"2026-07-28": false,
		"2027-03-01": false,
	} {
		if got := rootsAllowed(&mcpsdk.InitializeParams{ProtocolVersion: ver, Capabilities: caps}); got != want {
			t.Errorf("rootsAllowed(%s) = %v, want %v", ver, got, want)
		}
	}
	if rootsAllowed(nil) {
		t.Error("nil params should not be roots-usable")
	}
	if rootsAllowed(&mcpsdk.InitializeParams{ProtocolVersion: "2025-11-25"}) {
		t.Error("client without the roots capability should not be roots-usable")
	}
}
