package mcp

import (
	"context"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/stubbedev/srv/internal/constants"
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
// the agent when to reach for srv's tools instead of shelling out to traefik,
// mkcert, openssl, or docker, and flags which tools mutate state.
const serverInstructions = `srv manages a Traefik + TLS edge: site routing, local (mkcert) and production
(Let's Encrypt) certificates, and local DNS. Prefer these tools over shelling
out to traefik, mkcert, openssl, docker, or hand-editing files under
~/.config/srv — the tools keep metadata.yml, the Traefik dynamic config, and
dnsmasq in sync, which manual edits do not.

Start here:
- Call ` + "`list_sites`" + ` / ` + "`list_proxies`" + ` / ` + "`list_redirects`" + ` to discover what exists.
- Call ` + "`get_site`" + ` / ` + "`get_proxy`" + ` before any mutation to see current state.
- Call ` + "`daemon_status`" + ` and ` + "`doctor`" + ` when something is not routing.

Mutations: add_site / add_proxy / add_redirect create; start_site / stop_site /
restart_site control containers; reload_site re-applies metadata. Destructive
tools (remove_site, remove_proxy, remove_redirect) accept dry_run (preview) and
ack (skip the confirmation prompt). Secrets (TLS keys, credentials) are redacted
from all tool output.`

// newServer builds the fully-registered MCP server without starting a
// transport. Extracted from Serve so tests can introspect the tool
// surface over an in-memory transport.
func newServer() *mcpsdk.Server {
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:    "srv",
		Version: version,
	}, &mcpsdk.ServerOptions{
		Instructions: serverInstructions,
	})

	registerReadTools(srv)
	registerDiagTools(srv)
	registerWriteTools(srv)
	registerResources(srv)
	return srv
}

// Serve boots the MCP server on stdio and blocks until the client
// disconnects or ctx is cancelled. Returns nil on clean shutdown.
func Serve(ctx context.Context) error {
	if err := newServer().Run(ctx, &mcpsdk.StdioTransport{}); err != nil {
		return fmt.Errorf("mcp server: %w", err)
	}
	return nil
}
