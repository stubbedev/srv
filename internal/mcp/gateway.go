package mcp

import (
	"context"
	"fmt"
	"sync"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// The MCP surface is lazy-loaded. At `initialize` the server advertises only
// the two core tools (`version`, `srv_activate`); the other 26 stay hidden so
// they cost zero context tokens in the ~99% of sessions that never touch srv.
// An agent calls `srv_activate` to register a tier on demand, and the SDK fires
// notifications/tools/list_changed so the client re-fetches tools/list.
//
// Tiers are cumulative: "write" implies "read". Activation is one-way and
// per-process (each stdio client launches a fresh `srv mcp`).

// readToolNames are the read-only inspection + diagnostics tools (the "read"
// tier). registerReadTools + registerDiagTools bind these. Kept here as the
// single source of truth so the activation result and the guard test agree.
var readToolNames = []string{
	// reads (registerReadTools, minus version which is core/tier-0)
	"paths", "list_sites", "get_site", "validate_site",
	"list_proxies", "get_proxy", "list_redirects",
	// diagnostics (registerDiagTools)
	"daemon_status", "daemon_log", "metrics_status",
}

// writeToolNames are the mutating tools (the "write" tier). registerWriteTools
// binds these.
var writeToolNames = []string{
	"reload_site",
	"add_proxy", "remove_proxy",
	"add_redirect", "remove_redirect",
	"add_site", "start_site", "stop_site", "restart_site", "remove_site",
	"add_alias", "remove_alias", "set_internal_listener",
	"add_volume", "remove_volume",
	"add_route", "remove_route", "attach_network", "detach_network",
}

// coreToolNames are the tools advertised at initialize, before any activation.
var coreToolNames = []string{"version", "srv_activate"}

// activator owns the lazy tool-registration state for one server. Tool handlers
// can run concurrently, so mu guards the registered flags against a double
// AddTool (which would advertise a duplicate).
type activator struct {
	srv   *mcpsdk.Server
	mu    sync.Mutex
	read  bool // read + diagnostics tools registered
	write bool // mutating tools registered
}

// activate registers the tools for the named tier (and any tier it implies) if
// not already present, returning the names it newly added. Idempotent: a second
// call for an already-active tier adds nothing. Caller-facing validation of the
// group string happens in the tool handler.
func (a *activator) activate(group string) []string {
	a.mu.Lock()
	defer a.mu.Unlock()

	var added []string

	// "write" implies "read": you must inspect (get_site) before you mutate.
	wantRead := group == "read" || group == "write"
	wantWrite := group == "write"

	if wantRead && !a.read {
		registerReadTools(a.srv)
		registerDiagTools(a.srv)
		a.read = true
		added = append(added, readToolNames...)
	}
	if wantWrite && !a.write {
		registerWriteTools(a.srv)
		a.write = true
		added = append(added, writeToolNames...)
	}
	return added
}

type activateIn struct {
	// Group selects the tier to unlock: "read" registers the read-only
	// inspection + diagnostics tools; "write" registers those plus every
	// mutating tool (destructive ones still gate on confirm/ack). Defaults to
	// "write" when empty, since unlocking the full surface is the common intent.
	Group string `json:"group"`
}

type activateOut struct {
	Group     string   `json:"group"`     // the tier that was applied
	Activated []string `json:"activated"` // tool names newly registered (empty if already active)
	Message   string   `json:"message"`   // human/agent-facing summary
}

// activateTool is the srv_activate handler. It unlocks a tier of tools at
// runtime; the SDK notifies the client, which re-lists tools to see them.
func (a *activator) activateTool(_ context.Context, _ *mcpsdk.CallToolRequest, in activateIn) (*mcpsdk.CallToolResult, activateOut, error) {
	group := in.Group
	if group == "" {
		group = "write"
	}
	if group != "read" && group != "write" {
		return nil, activateOut{}, fmt.Errorf("group must be \"read\" or \"write\" (got %q)", in.Group)
	}

	added := a.activate(group)

	var msg string
	if len(added) == 0 {
		msg = fmt.Sprintf("%q tier already active; no new tools registered.", group)
	} else {
		msg = fmt.Sprintf("Registered %d %q-tier tool(s). Your client's tool list has refreshed — the new tools are now callable.", len(added), group)
	}
	return nil, activateOut{Group: group, Activated: added, Message: msg}, nil
}

// registerGateway binds the always-on srv_activate gateway tool.
func registerGateway(srv *mcpsdk.Server) {
	act := &activator{srv: srv}
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name: "srv_activate",
		Description: "Unlock a tier of srv tools. This server lazy-loads: only `version` and " +
			"`srv_activate` are advertised until you call this. Pass group=\"read\" to register " +
			"read-only inspection + diagnostics tools (list_sites, get_site, daemon_status, …), or " +
			"group=\"write\" (the default) to also register every mutating tool (add_site, start_site, " +
			"remove_proxy, …). \"write\" implies \"read\". Call this first whenever the user wants to " +
			"inspect or change their srv setup. Activation lasts for this session; after it, the new " +
			"tools appear in your tool list automatically.",
		Annotations: writeAnno("Activate srv tools", false, true, false),
	}, act.activateTool)
}
