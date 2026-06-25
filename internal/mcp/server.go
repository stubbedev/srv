package mcp

import (
	"context"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/shell"
)

// version is the srv version advertised by the MCP server. Overridden via
// SetVersion from cmd/mcp.go so the server reports the same build info
// `srv version` does. Defaults to the package's compile-time placeholder
// so unit tests don't need to wire it.
var version = constants.DefaultVersion

// SetVersion sets the version string advertised by Serve. Called once from
// cmd/mcp.go during init so MCP clients see the real binary version.
func SetVersion(v string) {
	if v != "" {
		version = v
	}
}

// serverInstructions is sent to the client during MCP `initialize`. It teaches
// the agent that the tool surface is lazy-loaded behind `srv_activate`, when to
// reach for srv's tools instead of shelling out, and flags which tools mutate
// state.
const serverInstructions = `srv manages a Traefik + TLS edge: site routing, local (mkcert) and production
(Let's Encrypt) certificates, and local DNS. Prefer these tools over shelling
out to traefik, mkcert, openssl, docker, or hand-editing files under
~/.config/srv — the tools keep metadata.yml, the Traefik dynamic config, and
dnsmasq in sync, which manual edits do not.

This server LAZY-LOADS its tools. At startup only two are advertised: ` + "`version`" + `
and ` + "`srv_activate`" + `. The ~26 inspection and mutation tools stay hidden until you
ask for them, so they cost no context in sessions that never touch srv.

Start here:
- The moment the user wants to inspect or change their srv setup, call
  ` + "`srv_activate`" + `. Pass group="read" for read-only inspection + diagnostics, or
  group="write" (the default) to also unlock mutations. "write" implies "read".
- After activating, the unlocked tools appear in your tool list automatically.
  Then call ` + "`list_sites`" + ` / ` + "`list_proxies`" + ` / ` + "`list_redirects`" + ` to discover what
  exists, and ` + "`get_site`" + ` / ` + "`get_proxy`" + ` before any mutation.
- Call ` + "`daemon_status`" + ` when something is not routing.

Mutations (in the "write" tier): add_site / add_proxy / add_redirect create;
start_site / stop_site / restart_site control containers; reload_site re-applies
metadata. Destructive tools (remove_site, remove_proxy, remove_redirect) accept
dry_run (preview) and ack (skip the confirmation prompt). Secrets (TLS keys,
credentials) are redacted from all tool output.`

// newServer builds the MCP server without starting a transport, advertising
// only the core tools (version + srv_activate); the read and write tiers are
// registered on demand by srv_activate. Extracted from Serve so tests can
// introspect the tool surface over an in-memory transport.
func newServer() *mcpsdk.Server {
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:    "srv",
		Version: version,
	}, &mcpsdk.ServerOptions{
		Instructions: serverInstructions,
	})

	srv.AddReceivingMiddleware(newToolMiddleware())
	registerVersionTool(srv)
	registerGateway(srv)
	registerResources(srv)
	return srv
}

// Serve boots the MCP server on stdio and blocks until the client
// disconnects or ctx is cancelled. Returns nil on clean shutdown.
func Serve(ctx context.Context) error {
	// MCP runs over stdio with no TTY, so a sudo password prompt would hang the
	// protocol stream. Force non-interactive sudo: privileged steps (mkcert CA
	// install, systemd-resolved drop-in, firewall) fail fast with an actionable
	// error instead of blocking. Operators run those once from a terminal.
	shell.SetNonInteractive(true)
	if err := newServer().Run(ctx, &mcpsdk.StdioTransport{}); err != nil {
		return fmt.Errorf("mcp server: %w", err)
	}
	return nil
}
