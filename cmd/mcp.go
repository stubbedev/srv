package cmd

import (
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/mcp"
)

// =============================================================================
// mcp command
// =============================================================================

var (
	mcpHTTPAddr       string
	mcpHTTPPath       string
	mcpToolTimeout    time.Duration
	mcpTrustedOrigins []string
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Start the srv MCP server (stdio, or --http for a shared daemon)",
	Long: `Run the Model Context Protocol server so AI agents can drive srv the
same way a human does from the CLI — inspecting and mutating sites, proxies,
redirects, routes, and networks.

The tool surface is lazy-loaded: at startup only 'version' and 'srv_activate'
are advertised, so srv costs no context in sessions that never use it. The
agent calls srv_activate(group="read") to unlock inspection + diagnostics, or
srv_activate(group="write") to also unlock mutations; the client refreshes its
tool list automatically.

Transports:

  stdio (default)   One server per client, launched on demand:

      { "mcpServers": { "srv": { "command": "srv", "args": ["mcp"] } } }

  --http            One long-running daemon shared by every MCP client on the
                    host (each Claude Code instance, etc.). Per-request
                    workspace context is taken from the client's MCP roots or an
                    X-Repo-Root header, so a single instance serves all clients:

      srv mcp --http                  # listens on 127.0.0.1:8765/mcp
      srv mcp --http=0.0.0.0:9000     # bind elsewhere (loopback only by default)

                    Then point clients at the URL:

      { "mcpServers": { "srv": { "url": "http://127.0.0.1:8765/mcp" } } }

The HTTP endpoint trusts local processes (loopback, no auth) — it mutates a
privileged Traefik edge, so keep it behind a reverse proxy if bound off-host.
`,
	RunE: runMCP,
}

func init() {
	mcpCmd.GroupID = GroupSystem
	// --http takes an optional value: bare --http uses the default addr, while
	// --http=host:port overrides it. Absent → stdio. NoOptDefVal supplies the
	// default when the flag is given without "=value".
	mcpCmd.Flags().StringVar(&mcpHTTPAddr, "http", "", "serve over HTTP at this address instead of stdio (default "+constants.DefaultMCPHTTPAddr+" when given without a value; env "+constants.EnvMCPHTTPAddr+")")
	mcpCmd.Flags().Lookup("http").NoOptDefVal = constants.DefaultMCPHTTPAddr
	mcpCmd.Flags().StringVar(&mcpHTTPPath, "http-path", "", "HTTP endpoint path (default "+constants.DefaultMCPHTTPPath+"; env "+constants.EnvMCPHTTPPath+")")
	mcpCmd.Flags().DurationVar(&mcpToolTimeout, "tool-timeout", 10*time.Minute, "max duration for a single tool call before it is cancelled (0 disables; a hung mutation otherwise blocks the shared write lock)")
	mcpCmd.Flags().StringSliceVar(&mcpTrustedOrigins, "trusted-origin", nil, "browser Origin allowed through cross-origin protection (repeatable; only for browser MCP clients on an off-host bind)")
	RootCmd.AddCommand(mcpCmd)
}

func runMCP(cmd *cobra.Command, args []string) error {
	mcp.SetVersion(Version)
	mcp.SetToolTimeout(mcpToolTimeout)

	addr := firstNonEmpty(mcpHTTPAddr, os.Getenv(constants.EnvMCPHTTPAddr))
	path := firstNonEmpty(mcpHTTPPath, os.Getenv(constants.EnvMCPHTTPPath))

	if addr != "" || path != "" || len(mcpTrustedOrigins) > 0 {
		return mcp.ServeHTTP(cmd.Context(), mcp.HTTPOptions{
			Addr:           addr,
			Path:           path,
			TrustedOrigins: mcpTrustedOrigins,
		})
	}
	return mcp.Serve(cmd.Context())
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
