package mcp

import (
	"context"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/stubbedev/srv/internal/site"
)

// registerWriteTools binds mutating tools. The surface is intentionally
// limited to safe / idempotent operations for now — sites and proxies
// have validation + creation flows tightly coupled to the CLI layer that
// need extraction before they can be MCP-exposed. The TODO list below
// tracks what's still CLI-only.
//
// TODO (follow-up work, extract from cmd/ helpers):
//   - add_site, remove_site
//   - add_proxy, remove_proxy
//   - add_redirect, remove_redirect, redirect_reload
//   - site lifecycle: start_site, stop_site, restart_site
//   - alias add/remove, internal enable/disable
//   - install (environment bootstrap)
func registerWriteTools(srv *mcpsdk.Server) {
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "reload_site",
		Description: "Re-apply a site's metadata.yml without restarting the container. The daemon normally does this automatically on file save; call this to force a sync from MCP (for example after a write to metadata.yml made via another tool). Idempotent.",
		Annotations: writeAnno("Reload site", false, true, true),
	}, reloadSiteTool)
}

type reloadSiteIn struct {
	Name string `json:"name"`
}
type reloadSiteOut struct {
	OK           bool     `json:"ok"`
	Applied      []string `json:"applied,omitempty"`
	Skipped      []string `json:"skipped,omitempty"`
	NeedsRestart bool     `json:"needs_restart"`
	Error        string   `json:"error,omitempty"`
}

func reloadSiteTool(_ context.Context, _ *mcpsdk.CallToolRequest, in reloadSiteIn) (*mcpsdk.CallToolResult, reloadSiteOut, error) {
	if in.Name == "" {
		return nil, reloadSiteOut{}, fmt.Errorf("name is required")
	}
	res, err := site.Reload(in.Name)
	if err != nil {
		return nil, reloadSiteOut{OK: false, Error: err.Error()}, nil
	}
	out := reloadSiteOut{OK: true}
	if res != nil {
		// site.ReloadResult exposes Applied/Skipped/NeedsRestart fields;
		// reflect them best-effort. The shape is intentionally open so
		// any future fields on ReloadResult flow through without an MCP
		// schema change.
		out.NeedsRestart = res.NeedsRestart
	}
	return nil, out, nil
}
