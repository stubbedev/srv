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

// newServer builds the fully-registered MCP server without starting a
// transport. Extracted from Serve so tests can introspect the tool
// surface over an in-memory transport.
func newServer() *mcpsdk.Server {
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:    "srv",
		Version: version,
	}, &mcpsdk.ServerOptions{})

	registerReadTools(srv)
	registerWriteTools(srv)
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
