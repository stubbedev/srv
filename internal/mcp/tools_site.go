package mcp

import (
	"context"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/stubbedev/srv/internal/site"
)

// registerSiteWriteTools binds the site lifecycle and metadata-mutator tools.
func registerSiteWriteTools(srv *mcpsdk.Server) {
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "start_site",
		Description: "Start a site's containers (docker compose up). Regenerates per-site artifacts and connects the service to the srv network. Set build=true to rebuild images first. Idempotent.",
		Annotations: writeAnno("Start site", false, true, true),
	}, startSiteTool)

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "stop_site",
		Description: "Stop a site's containers (docker compose stop). Idempotent.",
		Annotations: writeAnno("Stop site", false, true, true),
	}, stopSiteTool)

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "restart_site",
		Description: "Restart a site's containers, regenerating artifacts first. Set build=true to rebuild images. Idempotent.",
		Annotations: writeAnno("Restart site", false, true, true),
	}, restartSiteTool)

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "add_site",
		Description: "Register a new site from a project directory and start it. Auto-detects type (docker-compose.yml → compose, Dockerfile → dockerfile, else static); override with `type`. `domain` is required. Set `local` for mkcert TLS (otherwise Let's Encrypt). For a multi-service compose project pass `service`. Local sites need the mkcert CA (run `srv install` once in a terminal if missing). Set start=false to register without starting.",
		Annotations: writeAnno("Add site", false, false, true),
	}, addSiteTool)

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "remove_site",
		Description: "Remove a site: stop its containers and delete its Traefik config, local cert, DNS registrations, and metadata directory. Destructive — pass dry_run to preview or ack to skip the confirmation prompt. (Adding a site is interactive and remains CLI-only via `srv add`.)",
		Annotations: writeAnno("Remove site", true, true, true),
	}, removeSiteTool)

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "add_alias",
		Description: "Add an extra hostname (alias) to a site. Updates metadata, DNS, the local cert, and routing. Run restart_site afterward to apply. Idempotent — a no-op if the alias already exists.",
		Annotations: writeAnno("Add site alias", false, true, true),
	}, addAliasTool)

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "remove_alias",
		Description: "Remove an alias hostname from a site (the canonical first domain cannot be removed this way). Destructive — pass dry_run to preview or ack to skip the confirmation prompt.",
		Annotations: writeAnno("Remove site alias", true, true, true),
	}, removeAliasTool)

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "set_internal_listener",
		Description: "Enable or disable the plain-HTTP `internal` entrypoint for a site (enable=true/false). Run restart_site afterward to apply. Idempotent.",
		Annotations: writeAnno("Set internal listener", false, true, true),
	}, setInternalListenerTool)

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "add_volume",
		Description: "Attach an extra host bind-mount to a site's container. source=host path, target=container path, read_only optional. Cannot overlap the project bind at /app. Run restart_site afterward.",
		Annotations: writeAnno("Add site volume", false, true, true),
	}, addVolumeTool)

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "remove_volume",
		Description: "Detach a bind-mount from a site by its container target path. Destructive — pass dry_run to preview or ack to skip the confirmation prompt.",
		Annotations: writeAnno("Remove site volume", true, true, true),
	}, removeVolumeTool)
}

// ─── lifecycle ───────────────────────────────────────────────────────

type lifecycleIn struct {
	Name  string `json:"name" jsonschema:"site name as listed by list_sites"`
	Build bool   `json:"build,omitempty" jsonschema:"rebuild images before starting (start/restart only)"`
}
type okOut struct {
	OK       bool     `json:"ok"`
	Warnings []string `json:"warnings,omitempty"`
	Error    string   `json:"error,omitempty"`
}

func startSiteTool(_ context.Context, _ *mcpsdk.CallToolRequest, in lifecycleIn) (*mcpsdk.CallToolResult, okOut, error) {
	if in.Name == "" {
		return nil, okOut{Error: "name is required"}, nil
	}
	if err := site.StartSite(in.Name, in.Build); err != nil {
		return nil, okOut{Error: err.Error()}, nil //nolint:nilerr // surfaced in payload
	}
	return nil, okOut{OK: true}, nil
}

func stopSiteTool(_ context.Context, _ *mcpsdk.CallToolRequest, in lifecycleIn) (*mcpsdk.CallToolResult, okOut, error) {
	if in.Name == "" {
		return nil, okOut{Error: "name is required"}, nil
	}
	if err := site.StopSite(in.Name); err != nil {
		return nil, okOut{Error: err.Error()}, nil //nolint:nilerr // surfaced in payload
	}
	return nil, okOut{OK: true}, nil
}

func restartSiteTool(_ context.Context, _ *mcpsdk.CallToolRequest, in lifecycleIn) (*mcpsdk.CallToolResult, okOut, error) {
	if in.Name == "" {
		return nil, okOut{Error: "name is required"}, nil
	}
	if err := site.RestartSite(in.Name, in.Build); err != nil {
		return nil, okOut{Error: err.Error()}, nil //nolint:nilerr // surfaced in payload
	}
	return nil, okOut{OK: true}, nil
}

// ─── add_site ────────────────────────────────────────────────────────

type addSiteVolume struct {
	Source   string `json:"source" jsonschema:"host path"`
	Target   string `json:"target" jsonschema:"container path (must not overlap /app)"`
	ReadOnly bool   `json:"read_only,omitempty"`
}

type addSiteIn struct {
	Path         string          `json:"path" jsonschema:"project directory to register"`
	Domain       string          `json:"domain" jsonschema:"canonical hostname (required)"`
	Type         string          `json:"type,omitempty" jsonschema:"force site type: compose, dockerfile, or static (default: auto-detect)"`
	Name         string          `json:"name,omitempty" jsonschema:"site name; derived from domain when omitted"`
	Aliases      []string        `json:"aliases,omitempty" jsonschema:"extra hostnames mapped to the same site"`
	Port         int             `json:"port,omitempty" jsonschema:"container port (default 80)"`
	Local        bool            `json:"local,omitempty" jsonschema:"use local mkcert TLS instead of Let's Encrypt"`
	Wildcard     bool            `json:"wildcard,omitempty" jsonschema:"match one-level subdomains (local only)"`
	InternalHTTP bool            `json:"internal_http,omitempty" jsonschema:"also expose on the internal plain-HTTP entrypoint"`
	Service      string          `json:"service,omitempty" jsonschema:"compose service to route to (multi-service projects)"`
	Profile      string          `json:"profile,omitempty" jsonschema:"compose profile to select"`
	SPA          bool            `json:"spa,omitempty" jsonschema:"static sites: SPA fallback to index.html"`
	Cache        bool            `json:"cache,omitempty" jsonschema:"static sites: asset caching headers"`
	CORS         bool            `json:"cors,omitempty" jsonschema:"static sites: permissive CORS headers"`
	Volumes      []addSiteVolume `json:"volumes,omitempty" jsonschema:"extra host bind-mounts"`
	Force        bool            `json:"force,omitempty" jsonschema:"overwrite an existing site"`
	Start        *bool           `json:"start,omitempty" jsonschema:"start the containers after adding (default true)"`
}
type addSiteOut struct {
	OK       bool     `json:"ok"`
	Name     string   `json:"name,omitempty"`
	Domain   string   `json:"domain,omitempty"`
	Type     string   `json:"type,omitempty"`
	IsLocal  bool     `json:"is_local,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
	Error    string   `json:"error,omitempty"`
}

func addSiteTool(ctx context.Context, req *mcpsdk.CallToolRequest, in addSiteIn) (*mcpsdk.CallToolResult, addSiteOut, error) {
	if in.Path == "" || in.Domain == "" {
		return nil, addSiteOut{Error: "path and domain are required"}, nil
	}
	// A shared HTTP daemon's cwd is not the caller's, so anchor a relative
	// project path (and any relative bind-mount source) to the client's
	// workspace root. Absolute paths and stdio callers are unaffected.
	in.Path = anchorPath(ctx, req, in.Path)
	// Local sites issue a mkcert cert; guard the CA install behind the same
	// non-interactive-sudo preflight the proxy/redirect add tools use.
	if in.Local {
		if err := requireCAForLocalCert(); err != nil {
			return nil, addSiteOut{Error: err.Error()}, nil //nolint:nilerr // surfaced in payload
		}
	}
	start := true
	if in.Start != nil {
		start = *in.Start
	}
	mounts := make([]site.VolumeMount, 0, len(in.Volumes))
	for _, v := range in.Volumes {
		mounts = append(mounts, site.VolumeMount{Source: anchorPath(ctx, req, v.Source), Target: v.Target, ReadOnly: v.ReadOnly})
	}
	res, err := site.Add(site.AddOptions{
		Path:         in.Path,
		TypeOverride: in.Type,
		Name:         in.Name,
		Domain:       in.Domain,
		Aliases:      in.Aliases,
		Port:         in.Port,
		Local:        in.Local,
		Wildcard:     in.Wildcard,
		InternalHTTP: in.InternalHTTP,
		Service:      in.Service,
		Profile:      in.Profile,
		SPA:          in.SPA,
		Cache:        in.Cache,
		CORS:         in.CORS,
		Volumes:      mounts,
		Force:        in.Force,
		Start:        start,
	})
	if err != nil {
		return nil, addSiteOut{Error: err.Error()}, nil //nolint:nilerr // surfaced in payload
	}
	return nil, addSiteOut{OK: true, Name: res.Name, Domain: res.Domain, Type: res.Type, IsLocal: res.IsLocal, Warnings: res.Warnings}, nil
}

// ─── remove_site ─────────────────────────────────────────────────────

type removeSiteIn struct {
	Name   string `json:"name" jsonschema:"site name as listed by list_sites"`
	DryRun bool   `json:"dry_run,omitempty" jsonschema:"preview without removing"`
	Ack    bool   `json:"ack,omitempty" jsonschema:"skip the confirmation prompt"`
}

func removeSiteTool(ctx context.Context, req *mcpsdk.CallToolRequest, in removeSiteIn) (*mcpsdk.CallToolResult, okOut, error) {
	if in.Name == "" {
		return nil, okOut{Error: "name is required"}, nil
	}
	if in.DryRun {
		return nil, okOut{OK: true}, nil
	}
	if ok, reason := confirmDestructive(ctx, req, in.DryRun, in.Ack, fmt.Sprintf("Remove site %q? This stops its containers and deletes its config, cert, DNS, and metadata.", in.Name)); !ok {
		return nil, okOut{Error: reason}, nil
	}
	warnings, err := site.RemoveSite(in.Name)
	if err != nil {
		return nil, okOut{Error: err.Error()}, nil //nolint:nilerr // surfaced in payload
	}
	return nil, okOut{OK: true, Warnings: warnings}, nil
}

// ─── aliases ─────────────────────────────────────────────────────────

type aliasIn struct {
	Name   string `json:"name" jsonschema:"site name"`
	Alias  string `json:"alias" jsonschema:"the hostname to add or remove"`
	DryRun bool   `json:"dry_run,omitempty"`
	Ack    bool   `json:"ack,omitempty"`
}

func addAliasTool(_ context.Context, _ *mcpsdk.CallToolRequest, in aliasIn) (*mcpsdk.CallToolResult, okOut, error) {
	if in.Name == "" || in.Alias == "" {
		return nil, okOut{Error: "name and alias are required"}, nil
	}
	_, warnings, err := site.AddAlias(in.Name, in.Alias)
	if err != nil {
		return nil, okOut{Error: err.Error()}, nil //nolint:nilerr // surfaced in payload
	}
	return nil, okOut{OK: true, Warnings: warnings}, nil
}

func removeAliasTool(ctx context.Context, req *mcpsdk.CallToolRequest, in aliasIn) (*mcpsdk.CallToolResult, okOut, error) {
	if in.Name == "" || in.Alias == "" {
		return nil, okOut{Error: "name and alias are required"}, nil
	}
	if in.DryRun {
		return nil, okOut{OK: true}, nil
	}
	if ok, reason := confirmDestructive(ctx, req, in.DryRun, in.Ack, fmt.Sprintf("Remove alias %q from site %q?", in.Alias, in.Name)); !ok {
		return nil, okOut{Error: reason}, nil
	}
	warnings, err := site.RemoveAlias(in.Name, in.Alias)
	if err != nil {
		return nil, okOut{Error: err.Error()}, nil //nolint:nilerr // surfaced in payload
	}
	return nil, okOut{OK: true, Warnings: warnings}, nil
}

// ─── internal listener ───────────────────────────────────────────────

type setInternalIn struct {
	Name   string `json:"name" jsonschema:"site name"`
	Enable bool   `json:"enable" jsonschema:"true to enable the internal listener, false to disable"`
}

func setInternalListenerTool(_ context.Context, _ *mcpsdk.CallToolRequest, in setInternalIn) (*mcpsdk.CallToolResult, okOut, error) {
	if in.Name == "" {
		return nil, okOut{Error: "name is required"}, nil
	}
	_, warnings, err := site.SetInternalListener(in.Name, in.Enable)
	if err != nil {
		return nil, okOut{Error: err.Error()}, nil //nolint:nilerr // surfaced in payload
	}
	return nil, okOut{OK: true, Warnings: warnings}, nil
}

// ─── volumes ─────────────────────────────────────────────────────────

type addVolumeIn struct {
	Name     string `json:"name" jsonschema:"site name"`
	Source   string `json:"source" jsonschema:"host path to mount"`
	Target   string `json:"target" jsonschema:"container path to mount at (must not overlap /app)"`
	ReadOnly bool   `json:"read_only,omitempty" jsonschema:"mount read-only"`
}

func addVolumeTool(ctx context.Context, req *mcpsdk.CallToolRequest, in addVolumeIn) (*mcpsdk.CallToolResult, okOut, error) {
	if in.Name == "" || in.Source == "" || in.Target == "" {
		return nil, okOut{Error: "name, source, and target are required"}, nil
	}
	// Anchor a relative host source to the caller's workspace (shared HTTP
	// daemon); absolute paths and stdio callers pass through unchanged.
	in.Source = anchorPath(ctx, req, in.Source)
	warnings, err := site.AddVolume(in.Name, site.VolumeMount{Source: in.Source, Target: in.Target, ReadOnly: in.ReadOnly})
	if err != nil {
		return nil, okOut{Error: err.Error()}, nil //nolint:nilerr // surfaced in payload
	}
	return nil, okOut{OK: true, Warnings: warnings}, nil
}

type removeVolumeIn struct {
	Name   string `json:"name" jsonschema:"site name"`
	Target string `json:"target" jsonschema:"the container target path of the mount to remove"`
	DryRun bool   `json:"dry_run,omitempty"`
	Ack    bool   `json:"ack,omitempty"`
}

func removeVolumeTool(ctx context.Context, req *mcpsdk.CallToolRequest, in removeVolumeIn) (*mcpsdk.CallToolResult, okOut, error) {
	if in.Name == "" || in.Target == "" {
		return nil, okOut{Error: "name and target are required"}, nil
	}
	if in.DryRun {
		return nil, okOut{OK: true}, nil
	}
	if ok, reason := confirmDestructive(ctx, req, in.DryRun, in.Ack, fmt.Sprintf("Detach volume %q from site %q?", in.Target, in.Name)); !ok {
		return nil, okOut{Error: reason}, nil
	}
	warnings, err := site.RemoveVolume(in.Name, in.Target)
	if err != nil {
		return nil, okOut{Error: err.Error()}, nil //nolint:nilerr // surfaced in payload
	}
	return nil, okOut{OK: true, Warnings: warnings}, nil
}
