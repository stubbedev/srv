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

func addVolumeTool(_ context.Context, _ *mcpsdk.CallToolRequest, in addVolumeIn) (*mcpsdk.CallToolResult, okOut, error) {
	if in.Name == "" || in.Source == "" || in.Target == "" {
		return nil, okOut{Error: "name, source, and target are required"}, nil
	}
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
