package cmd

import (
	"github.com/spf13/cobra"

	"github.com/stubbedev/srv/internal/mcp"
)

// =============================================================================
// mcp command
// =============================================================================

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Start the srv MCP server (stdio)",
	Long: `Run the Model Context Protocol server on stdio so AI agents can drive
srv the same way a human does from the CLI — inspecting and mutating sites,
proxies, redirects, routes, and networks.

The tool surface is lazy-loaded: at startup only 'version' and 'srv_activate'
are advertised, so srv costs no context in sessions that never use it. The
agent calls srv_activate(group="read") to unlock inspection + diagnostics, or
srv_activate(group="write") to also unlock mutations; the client refreshes its
tool list automatically.

Intended to be launched by an MCP client config such as:

  {
    "mcpServers": {
      "srv": {
        "command": "srv",
        "args": ["mcp"]
      }
    }
  }
`,
	RunE: runMCP,
}

func init() {
	mcpCmd.GroupID = GroupSystem
	RootCmd.AddCommand(mcpCmd)
}

func runMCP(cmd *cobra.Command, args []string) error {
	mcp.SetVersion(Version)
	return mcp.Serve(cmd.Context())
}
