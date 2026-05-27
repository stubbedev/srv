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
srv the same way a human does from the CLI. Reads (list sites, get site,
list proxies, …) are exposed today; mutating operations (add/remove sites,
lifecycle, install) are still being extracted from the CLI layer and will
land in follow-up changes.

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
