package mcp

import (
	"context"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/proxy"
	"github.com/stubbedev/srv/internal/redirect"
	"github.com/stubbedev/srv/internal/shell"
	"github.com/stubbedev/srv/internal/traefik"
)

// caNotInstalledErr is returned by add tools when the mkcert CA is missing and
// sudo cannot run non-interactively — installing the CA needs a password the
// MCP server cannot prompt for.
const caNotInstalledMsg = "mkcert CA is not installed and installing it needs sudo, which this MCP server cannot prompt for. Run `srv install` (or `mkcert -install`) once in a terminal, then retry."

// requireCAForLocalCert guards add tools that must issue a local cert: if the
// CA is already installed, the cert can be issued without sudo; otherwise, only
// proceed when sudo is functional non-interactively.
func requireCAForLocalCert() error {
	if traefik.IsCAInstalled() {
		return nil
	}
	if shell.SudoFunctional() {
		return nil
	}
	return fmt.Errorf("%s", caNotInstalledMsg)
}

func registerProxyWriteTools(srv *mcpsdk.Server) {
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "add_proxy",
		Description: "Create a proxy routing a domain to a localhost port or a Docker container (container=\"name:port\"). Issues a local TLS cert and registers local DNS. Set exactly one of `port` or `container`. The CLI-only --fallback sidecar is not exposed. Requires the mkcert CA to be installed (run `srv install` once in a terminal if it is not).",
		Annotations: writeAnno("Add proxy", false, true, true),
		InputSchema: toolInputSchema[addProxyIn](),
	}, addProxyTool)

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "remove_proxy",
		Description: "Remove a proxy: deletes its Traefik config, local cert, DNS registration, and metadata. Destructive — pass dry_run to preview or ack to skip the confirmation prompt.",
		Annotations: writeAnno("Remove proxy", true, true, true),
	}, removeProxyTool)
}

func registerRedirectWriteTools(srv *mcpsdk.Server) {
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "add_redirect",
		Description: "Create a redirect. HTTP mode (default): 301/302 from `domain` to `to` (an absolute http(s) URL), with a local cert. DNS-only mode (dns_only=true): a dnsmasq A-record alias from `domain` to a bare hostname `to` (no TLS, no Traefik). HTTP mode requires the mkcert CA (run `srv install` once in a terminal if missing).",
		Annotations: writeAnno("Add redirect", false, true, true),
		InputSchema: toolInputSchema[addRedirectIn](),
	}, addRedirectTool)

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "remove_redirect",
		Description: "Remove a redirect (HTTP or DNS-only): deletes its yaml and any derived cert/DNS state. Destructive — pass dry_run to preview or ack to skip the confirmation prompt.",
		Annotations: writeAnno("Remove redirect", true, true, true),
	}, removeRedirectTool)
}

// ─── add_proxy ───────────────────────────────────────────────────────

// addProxyIn is proxy.AddSpec itself: its tags define the add_proxy input
// schema (the CLI-only fallback fields are json:"-"), so the handler decodes
// the shared spec directly with no field-by-field copy.
type (
	addProxyIn  = proxy.AddSpec
	addProxyOut struct {
		OK        bool     `json:"ok"`
		Name      string   `json:"name,omitempty"`
		Domain    string   `json:"domain,omitempty"`
		TargetURL string   `json:"target_url,omitempty"`
		Warnings  []string `json:"warnings,omitempty"`
		Error     string   `json:"error,omitempty"`
	}
)

func addProxyTool(_ context.Context, _ *mcpsdk.CallToolRequest, in addProxyIn) (*mcpsdk.CallToolResult, addProxyOut, error) {
	if err := requireCAForLocalCert(); err != nil {
		return nil, addProxyOut{Error: err.Error()}, nil //nolint:nilerr // surfaced in payload
	}
	cfg, err := config.Load()
	if err != nil {
		return nil, addProxyOut{}, err
	}
	res, err := proxy.Add(cfg, in)
	if err != nil {
		return nil, addProxyOut{Error: err.Error()}, nil //nolint:nilerr // surfaced in payload
	}
	return nil, addProxyOut{OK: true, Name: res.Name, Domain: res.Domain, TargetURL: res.TargetURL, Warnings: res.Warnings}, nil
}

// ─── remove_proxy ────────────────────────────────────────────────────

type removeProxyIn struct {
	Name   string `json:"name"              jsonschema:"proxy name as listed by list_proxies"`
	DryRun bool   `json:"dry_run,omitempty" jsonschema:"preview without removing"`
	Ack    bool   `json:"ack,omitempty"     jsonschema:"skip the confirmation prompt"`
}
type removeProxyOut struct {
	OK       bool     `json:"ok"`
	DryRun   bool     `json:"dry_run,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
	Error    string   `json:"error,omitempty"`
}

func removeProxyTool(ctx context.Context, req *mcpsdk.CallToolRequest, in removeProxyIn) (*mcpsdk.CallToolResult, removeProxyOut, error) {
	if in.Name == "" {
		return nil, removeProxyOut{Error: "name is required"}, nil
	}
	cfg, err := config.Load()
	if err != nil {
		return nil, removeProxyOut{}, err
	}
	if in.DryRun {
		return nil, removeProxyOut{OK: true, DryRun: true}, nil
	}
	if ok, reason := confirmDestructive(ctx, req, in.DryRun, in.Ack, fmt.Sprintf("Remove proxy %q? This deletes its Traefik config, cert, and DNS registration.", in.Name)); !ok {
		return nil, removeProxyOut{Error: reason}, nil
	}
	warnings, err := proxy.RemoveProxy(cfg, in.Name)
	if err != nil {
		return nil, removeProxyOut{Error: err.Error()}, nil //nolint:nilerr // surfaced in payload
	}
	return nil, removeProxyOut{OK: true, Warnings: warnings}, nil
}

// ─── add_redirect ────────────────────────────────────────────────────

// addRedirectIn is redirect.AddSpec (whose tags define the add_redirect input
// schema) plus the MCP-only `temporary` convenience flag, the negation of the
// CLI-only Permanent field.
type addRedirectIn struct {
	redirect.AddSpec
	Temporary bool `json:"temporary,omitempty" jsonschema:"description=use a 302 instead of 301 (HTTP mode only)"`
}
type addRedirectOut struct {
	OK       bool     `json:"ok"`
	Name     string   `json:"name,omitempty"`
	Domain   string   `json:"domain,omitempty"`
	Target   string   `json:"target,omitempty"`
	DNSOnly  bool     `json:"dns_only,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
	Error    string   `json:"error,omitempty"`
}

func addRedirectTool(_ context.Context, _ *mcpsdk.CallToolRequest, in addRedirectIn) (*mcpsdk.CallToolResult, addRedirectOut, error) {
	// DNS-only redirects need no cert/CA; only HTTP redirects do.
	if !in.DNSOnly {
		if err := requireCAForLocalCert(); err != nil {
			return nil, addRedirectOut{Error: err.Error()}, nil //nolint:nilerr // surfaced in payload
		}
	}
	cfg, err := config.Load()
	if err != nil {
		return nil, addRedirectOut{}, err
	}
	spec := in.AddSpec
	spec.Permanent = !in.Temporary
	res, err := redirect.Add(cfg, spec)
	if err != nil {
		return nil, addRedirectOut{Error: err.Error()}, nil //nolint:nilerr // surfaced in payload
	}
	return nil, addRedirectOut{OK: true, Name: res.Name, Domain: res.Domain, Target: res.Target, DNSOnly: res.DNSOnly, Warnings: res.Warnings}, nil
}

// ─── remove_redirect ─────────────────────────────────────────────────

type removeRedirectIn struct {
	Name   string `json:"name"              jsonschema:"redirect name as listed by list_redirects"`
	DryRun bool   `json:"dry_run,omitempty" jsonschema:"preview without removing"`
	Ack    bool   `json:"ack,omitempty"     jsonschema:"skip the confirmation prompt"`
}
type removeRedirectOut struct {
	OK       bool     `json:"ok"`
	DryRun   bool     `json:"dry_run,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
	Error    string   `json:"error,omitempty"`
}

func removeRedirectTool(ctx context.Context, req *mcpsdk.CallToolRequest, in removeRedirectIn) (*mcpsdk.CallToolResult, removeRedirectOut, error) {
	if in.Name == "" {
		return nil, removeRedirectOut{Error: "name is required"}, nil
	}
	cfg, err := config.Load()
	if err != nil {
		return nil, removeRedirectOut{}, err
	}
	if in.DryRun {
		return nil, removeRedirectOut{OK: true, DryRun: true}, nil
	}
	if ok, reason := confirmDestructive(ctx, req, in.DryRun, in.Ack, fmt.Sprintf("Remove redirect %q?", in.Name)); !ok {
		return nil, removeRedirectOut{Error: reason}, nil
	}
	warnings, err := redirect.RemoveRedirect(cfg, in.Name)
	if err != nil {
		return nil, removeRedirectOut{Error: err.Error()}, nil //nolint:nilerr // surfaced in payload
	}
	return nil, removeRedirectOut{OK: true, Warnings: warnings}, nil
}
