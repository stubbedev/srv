package mcp

import (
	"context"
	"fmt"
	"slices"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/stubbedev/srv/internal/proxy"
	"github.com/stubbedev/srv/internal/site"
)

// addRouteSchema is reflected once per process (see addSiteSchema).
var addRouteSchema = toolInputSchema[addRouteIn]()

// registerRouteNetworkTools binds the extra-route and docker-network tools.
func registerRouteNetworkTools(srv *mcpsdk.Server) {
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "add_route",
		Description: "Attach an extra Traefik route (path-prefix or regex) to a site or proxy `target`. Set one of path/path_regex and one upstream (port, container as name:port, or url). rewrite requires path_regex. id is derived from the path when omitted. Run restart afterward for label-based sites.",
		Annotations: writeAnno("Add route", false, true, true),
		InputSchema: addRouteSchema,
	}, addRouteTool)

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "remove_route",
		Description: "Remove an extra route by id from a site or proxy `target`. Destructive — pass dry_run to preview or ack to skip the confirmation prompt.",
		Annotations: writeAnno("Remove route", true, true, true),
	}, removeRouteTool)

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "attach_network",
		Description: "Attach an existing Docker network to a site so its container can reach services on that network. Run restart_site afterward. Idempotent.",
		Annotations: writeAnno("Attach network", false, true, true),
	}, attachNetworkTool)

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "detach_network",
		Description: "Detach an extra Docker network from a site. Destructive — pass dry_run to preview or ack to skip the confirmation prompt.",
		Annotations: writeAnno("Detach network", true, true, true),
	}, detachNetworkTool)
}

// ─── add_route / remove_route ────────────────────────────────────────

// addRouteIn is site.RouteInput (whose tags define the add_route input schema)
// plus the MCP-only target selector.
type addRouteIn struct {
	site.RouteInput
	Target string `json:"target" jsonschema:"description=site or proxy name to attach the route to"`
}
type routeOut struct {
	OK      bool           `json:"ok"`
	Target  string         `json:"target,omitempty"`
	Kind    string         `json:"kind,omitempty"` // "site" or "proxy"
	ID      string         `json:"id,omitempty"`
	DryRun  bool           `json:"dry_run,omitempty"`
	Error   string         `json:"error,omitempty"`
	Preview *dryRunPreview `json:"preview,omitempty"`
}

func addRouteTool(_ context.Context, _ *mcpsdk.CallToolRequest, in addRouteIn) (*mcpsdk.CallToolResult, routeOut, error) {
	if in.Target == "" {
		return nil, routeOut{Error: "target is required"}, nil
	}
	route, err := site.BuildRoute(in.RouteInput)
	if err != nil {
		return nil, routeOut{Error: err.Error()}, nil //nolint:nilerr // surfaced in payload
	}
	kind, err := applyRoute(in.Target, func() error { return site.AddRoute(in.Target, route) }, func() error { return proxy.AddRoute(in.Target, route) })
	if err != nil {
		return nil, routeOut{Error: err.Error()}, nil //nolint:nilerr // surfaced in payload
	}
	return nil, routeOut{OK: true, Target: in.Target, Kind: kind, ID: route.ID}, nil
}

type removeRouteIn struct {
	Target string `json:"target"            jsonschema:"site or proxy name"`
	ID     string `json:"id"                jsonschema:"route id to remove"`
	DryRun bool   `json:"dry_run,omitempty"`
	Ack    bool   `json:"ack,omitempty"`
}

func removeRouteTool(ctx context.Context, req *mcpsdk.CallToolRequest, in removeRouteIn) (*mcpsdk.CallToolResult, routeOut, error) {
	if in.Target == "" || in.ID == "" {
		return nil, routeOut{Error: "target and id are required"}, nil
	}
	if in.DryRun {
		preview, err := routeRemovalPreview(in.Target, in.ID)
		if err != nil {
			return nil, routeOut{Error: err.Error()}, nil //nolint:nilerr // surfaced in payload
		}
		return nil, routeOut{OK: true, Target: in.Target, Kind: preview.Kind, ID: in.ID, DryRun: true, Preview: preview}, nil
	}
	if ok, reason := confirmDestructive(ctx, req, in.DryRun, in.Ack, fmt.Sprintf("Remove route %q from %q?", in.ID, in.Target)); !ok {
		return nil, routeOut{Error: reason}, nil
	}
	kind, err := applyRoute(in.Target, func() error { return site.RemoveRoute(in.Target, in.ID) }, func() error { return proxy.RemoveRoute(in.Target, in.ID) })
	if err != nil {
		return nil, routeOut{Error: err.Error()}, nil //nolint:nilerr // surfaced in payload
	}
	return nil, routeOut{OK: true, Target: in.Target, Kind: kind, ID: in.ID}, nil
}

// applyRoute dispatches a route op to the site or proxy named target. A route
// attaches to whichever exists; this mirrors the CLI's `srv route` behaviour.
func applyRoute(target string, onSite, onProxy func() error) (kind string, err error) {
	if site.Exists(target) {
		return "site", onSite()
	}
	if proxy.Exists(target) {
		return "proxy", onProxy()
	}
	return "", fmt.Errorf("no site or proxy named %q", target)
}

// routeRemovalPreview resolves a route removal against the target's metadata
// so a dry_run cannot validate an id that is not attached.
func routeRemovalPreview(target, id string) (*dryRunPreview, error) {
	if site.Exists(target) {
		meta, err := siteMeta(target)
		if err != nil {
			return nil, err
		}
		for _, r := range meta.Routes {
			if r.ID == id {
				return &dryRunPreview{Target: target, Kind: "site", WouldRemove: []string{"route " + id + " (" + r.Path + r.PathRegex + ")"}}, nil
			}
		}
		return nil, fmt.Errorf("route %q not found on site %s", id, target)
	}
	if proxy.Exists(target) {
		meta, err := proxy.Read(target)
		if err != nil {
			return nil, err
		}
		if meta == nil {
			return nil, fmt.Errorf("proxy %q not found", target)
		}
		for _, r := range meta.Routes {
			if r.ID == id {
				return &dryRunPreview{Target: target, Kind: "proxy", WouldRemove: []string{"route " + id + " (" + r.Path + r.PathRegex + ")"}}, nil
			}
		}
		return nil, fmt.Errorf("route %q not found on proxy %s", id, target)
	}
	return nil, fmt.Errorf("no site or proxy named %q", target)
}

// ─── attach_network / detach_network ─────────────────────────────────

type attachNetworkIn struct {
	Name    string `json:"name"    jsonschema:"site name"`
	Network string `json:"network" jsonschema:"existing Docker network name to attach"`
}

func attachNetworkTool(_ context.Context, _ *mcpsdk.CallToolRequest, in attachNetworkIn) (*mcpsdk.CallToolResult, okOut, error) {
	if in.Name == "" || in.Network == "" {
		return nil, okOut{Error: "name and network are required"}, nil
	}
	_, warnings, err := site.AttachNetwork(in.Name, in.Network)
	if err != nil {
		return nil, okOut{Error: err.Error()}, nil //nolint:nilerr // surfaced in payload
	}
	return nil, okOut{OK: true, Warnings: warnings}, nil
}

type detachNetworkIn struct {
	Name    string `json:"name"              jsonschema:"site name"`
	Network string `json:"network"           jsonschema:"network to detach"`
	DryRun  bool   `json:"dry_run,omitempty"`
	Ack     bool   `json:"ack,omitempty"`
}

func detachNetworkTool(ctx context.Context, req *mcpsdk.CallToolRequest, in detachNetworkIn) (*mcpsdk.CallToolResult, okOut, error) {
	if in.Name == "" || in.Network == "" {
		return nil, okOut{Error: "name and network are required"}, nil
	}
	if in.DryRun {
		meta, err := siteMeta(in.Name)
		if err != nil {
			return nil, okOut{Error: err.Error()}, nil //nolint:nilerr // surfaced in payload
		}
		if slices.Contains(meta.ExtraNetworks, in.Network) {
			return nil, okOut{OK: true, Preview: &dryRunPreview{Target: in.Network, Kind: "network", WouldRemove: []string{
				"network " + in.Network + " from site " + in.Name + " (restarted without it)",
			}}}, nil
		}
		return nil, okOut{Error: fmt.Sprintf("network %q not attached to %s", in.Network, in.Name)}, nil
	}
	if ok, reason := confirmDestructive(ctx, req, in.DryRun, in.Ack, fmt.Sprintf("Detach network %q from site %q?", in.Network, in.Name)); !ok {
		return nil, okOut{Error: reason}, nil
	}
	warnings, err := site.DetachNetwork(in.Name, in.Network)
	if err != nil {
		return nil, okOut{Error: err.Error()}, nil //nolint:nilerr // surfaced in payload
	}
	return nil, okOut{OK: true, Warnings: warnings}, nil
}
