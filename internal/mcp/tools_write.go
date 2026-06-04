package mcp

import (
	"context"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/stubbedev/srv/internal/site"
)

// registerWriteTools binds mutating tools. Orchestration lives in the internal
// packages (site, proxy, redirect, traefik) so the CLI and MCP share it.
//
// Privileged steps (mkcert CA install, systemd-resolved drop-in) cannot prompt
// for a password over stdio, so the server runs sudo non-interactively; tools
// that need an uninstalled CA preflight-check and return an actionable error
// rather than hanging.
//
// Still CLI-only: `srv add` (interactive project detection + prompts), route
// and network mutators, install/uninstall, import valet, the proxy
// `--fallback` sidecar, and shell/open (inherently interactive).
func registerWriteTools(srv *mcpsdk.Server) {
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "reload_site",
		Description: "Re-apply a site's metadata.yml without restarting the container. The daemon normally does this automatically on file save; call this to force a sync from MCP (for example after a write to metadata.yml made via another tool). Idempotent.",
		Annotations: writeAnno("Reload site", false, true, true),
	}, reloadSiteTool)

	registerProxyWriteTools(srv)
	registerRedirectWriteTools(srv)
	registerSiteWriteTools(srv)
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
		return nil, reloadSiteOut{OK: false, Error: err.Error()}, nil //nolint:nilerr // reload failure reported in payload, not as call error
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
