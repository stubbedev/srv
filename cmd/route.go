// Package cmd — route.go implements `srv route` for attaching extra Traefik
// routers (path-prefix / regex-rewrite) to an existing site or proxy.
package cmd

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/stubbedev/srv/internal/proxy"
	"github.com/stubbedev/srv/internal/site"
	"github.com/stubbedev/srv/internal/ui"
)

var routeAddFlags struct {
	id           string
	path         string
	pathRegex    string
	rewrite      string
	port         int
	container    string
	url          string
	preserveHost bool
	rangeHeaders bool
	priority     int
}

var routeCmd = &cobra.Command{
	Use:   "route",
	Short: "Manage extra Traefik routers attached to a site",
	Long: `Each route adds a higher-priority router for one site/host that matches
a path prefix or regex and forwards to a separate upstream. Used for
WebSocket splits (e.g. /app → :6001) or regex rewrites (e.g. /videos/...
rewritten and proxied to an S3 gateway).`,
}

var routeAddCmd = &cobra.Command{
	Use:   "add SITE",
	Short: "Attach a route to a site",
	Args:  cobra.ExactArgs(1),
	RunE:  runRouteAdd,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			return routeTargetNames(), cobra.ShellCompDirectiveNoFileComp
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	},
}

var routeListCmd = &cobra.Command{
	Use:   "list SITE",
	Short: "List routes attached to a site",
	Args:  cobra.ExactArgs(1),
	RunE:  runRouteList,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			return routeTargetNames(), cobra.ShellCompDirectiveNoFileComp
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	},
}

var routeRemoveCmd = &cobra.Command{
	Use:     "remove SITE ID",
	Aliases: []string{"rm"},
	Short:   "Remove a route from a site",
	Args:    cobra.ExactArgs(2),
	RunE:    runRouteRemove,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			return GetSiteNames(), cobra.ShellCompDirectiveNoFileComp
		}
		if len(args) == 1 {
			return GetSiteRouteIDs(args[0]), cobra.ShellCompDirectiveNoFileComp
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	},
}

func init() {
	routeAddCmd.Flags().StringVar(&routeAddFlags.id, "id", "", "Stable identifier for this route (auto-derived from --path if omitted)")
	routeAddCmd.Flags().StringVar(&routeAddFlags.path, "path", "", "Path prefix to match (e.g. /app); mutually exclusive with --path-regex")
	routeAddCmd.Flags().StringVar(&routeAddFlags.pathRegex, "path-regex", "", "Regex matcher for the request path; mutually exclusive with --path")
	routeAddCmd.Flags().StringVar(&routeAddFlags.rewrite, "rewrite", "", "Replacement pattern (requires --path-regex)")
	routeAddCmd.Flags().IntVar(&routeAddFlags.port, "port", 0, "Upstream localhost port")
	routeAddCmd.Flags().StringVar(&routeAddFlags.container, "container", "", "Upstream container (container[:port])")
	routeAddCmd.Flags().StringVar(&routeAddFlags.url, "url", "", "Upstream URL (http:// or https://)")
	routeAddCmd.Flags().BoolVar(&routeAddFlags.preserveHost, "preserve-host", true, "Forward the Host header unchanged to the upstream")
	routeAddCmd.Flags().BoolVar(&routeAddFlags.rangeHeaders, "pass-range-headers", false, "Documentation-only; Traefik forwards Range headers by default")
	routeAddCmd.Flags().IntVar(&routeAddFlags.priority, "priority", 0, "Override the auto-computed Traefik router priority")

	routeCmd.GroupID = GroupSites
	routeCmd.AddCommand(routeAddCmd, routeListCmd, routeRemoveCmd)
	RootCmd.AddCommand(routeCmd)
}

func runRouteAdd(cmd *cobra.Command, args []string) error {
	target := args[0]
	route, err := buildRouteFromFlags()
	if err != nil {
		return err
	}

	if meta, _ := site.ReadSiteMetadata(target); meta != nil {
		return addRouteToSite(target, meta, route)
	}
	if pmeta, _ := proxy.Read(target); pmeta != nil {
		return addRouteToProxy(target, pmeta, route)
	}
	return fmt.Errorf("no site or proxy named %q", target)
}

func addRouteToSite(name string, meta *site.SiteMetadata, route site.Route) error {
	for _, existing := range meta.Routes {
		if existing.ID == route.ID {
			return fmt.Errorf("route %q already exists on %s — remove it first or pick a different --id", route.ID, name)
		}
	}

	meta.Routes = append(meta.Routes, route)
	if err := site.ValidateMetadata(meta); err != nil {
		return fmt.Errorf("route would produce invalid metadata: %w", err)
	}
	if err := site.WriteSiteMetadata(name, *meta); err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}
	if _, err := site.Reload(name); err != nil {
		ui.Warn("Failed to refresh routing config: %v", err)
	}
	ui.Success("Added route %q on %s", route.ID, name)
	return nil
}

func addRouteToProxy(name string, meta *proxy.Metadata, route site.Route) error {
	for _, existing := range meta.Routes {
		if existing.ID == route.ID {
			return fmt.Errorf("route %q already exists on %s — remove it first or pick a different --id", route.ID, name)
		}
	}
	meta.Routes = append(meta.Routes, route)
	if err := proxy.Write(*meta); err != nil {
		return fmt.Errorf("write proxy metadata: %w", err)
	}
	if err := proxy.Reload(name); err != nil {
		ui.Warn("Failed to refresh proxy routes: %v", err)
	}
	ui.Success("Added route %q on proxy %s", route.ID, name)
	return nil
}

func runRouteList(cmd *cobra.Command, args []string) error {
	target := args[0]
	if meta, _ := site.ReadSiteMetadata(target); meta != nil {
		return printRoutes(target, meta.Routes)
	}
	if pmeta, _ := proxy.Read(target); pmeta != nil {
		return printRoutes(target, pmeta.Routes)
	}
	return fmt.Errorf("no site or proxy named %q", target)
}

// routeListOut is the json shape under `srv route list --format json`.
type routeListOut struct {
	Target string       `json:"target"`
	Routes []site.Route `json:"routes"`
}

func printRoutes(name string, routes []site.Route) error {
	if jsonOutput() {
		return ui.PrintJSON(routeListOut{Target: name, Routes: routes})
	}
	if len(routes) == 0 {
		ui.Dim("No routes attached to %s", name)
		return nil
	}
	for _, r := range routes {
		match := r.Path
		if match == "" {
			match = "regex:" + r.PathRegex
		}
		upstream := describeUpstream(r.Upstream.Kind, r.Upstream.Container, r.Upstream.URL, r.Upstream.Port)
		extra := ""
		if r.Rewrite != "" {
			extra = " → " + r.Rewrite
		}
		ui.Print("  %s  %s%s  →  %s", r.ID, match, extra, upstream)
	}
	return nil
}

func runRouteRemove(cmd *cobra.Command, args []string) error {
	target, id := args[0], args[1]
	if meta, _ := site.ReadSiteMetadata(target); meta != nil {
		return removeRouteFromSite(target, meta, id)
	}
	if pmeta, _ := proxy.Read(target); pmeta != nil {
		return removeRouteFromProxy(target, pmeta, id)
	}
	return fmt.Errorf("no site or proxy named %q", target)
}

func removeRouteFromSite(name string, meta *site.SiteMetadata, id string) error {
	filtered, removed := dropRoute(meta.Routes, id)
	if !removed {
		return fmt.Errorf("route %q not found on %s", id, name)
	}
	meta.Routes = filtered
	if err := site.WriteSiteMetadata(name, *meta); err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}
	if _, err := site.Reload(name); err != nil {
		ui.Warn("Failed to refresh routing config: %v", err)
	}
	ui.Success("Removed route %q from %s", id, name)
	return nil
}

func removeRouteFromProxy(name string, meta *proxy.Metadata, id string) error {
	filtered, removed := dropRoute(meta.Routes, id)
	if !removed {
		return fmt.Errorf("route %q not found on proxy %s", id, name)
	}
	meta.Routes = filtered
	if err := proxy.Write(*meta); err != nil {
		return fmt.Errorf("write proxy metadata: %w", err)
	}
	if err := proxy.Reload(name); err != nil {
		ui.Warn("Failed to refresh proxy routes: %v", err)
	}
	ui.Success("Removed route %q from proxy %s", id, name)
	return nil
}

func dropRoute(routes []site.Route, id string) ([]site.Route, bool) {
	out := routes[:0]
	removed := false
	for _, r := range routes {
		if r.ID == id {
			removed = true
			continue
		}
		out = append(out, r)
	}
	return out, removed
}

// buildRouteFromFlags assembles a site.Route from the current routeAddFlags
// snapshot. Validation is partial here; the full check happens in
// site.ValidateMetadata after the new entry is appended.
func buildRouteFromFlags() (site.Route, error) {
	if routeAddFlags.path != "" && routeAddFlags.pathRegex != "" {
		return site.Route{}, fmt.Errorf("--path and --path-regex are mutually exclusive")
	}
	if routeAddFlags.path == "" && routeAddFlags.pathRegex == "" {
		return site.Route{}, fmt.Errorf("one of --path or --path-regex is required")
	}
	if routeAddFlags.rewrite != "" && routeAddFlags.pathRegex == "" {
		return site.Route{}, fmt.Errorf("--rewrite requires --path-regex")
	}
	if routeAddFlags.pathRegex != "" {
		if _, err := regexp.Compile(routeAddFlags.pathRegex); err != nil {
			return site.Route{}, fmt.Errorf("invalid --path-regex: %w", err)
		}
	}

	upstream, err := upstreamFromFlags()
	if err != nil {
		return site.Route{}, err
	}

	id := strings.TrimSpace(routeAddFlags.id)
	if id == "" {
		id = autoRouteID(routeAddFlags.path, routeAddFlags.pathRegex)
		if id == "" {
			return site.Route{}, fmt.Errorf("could not derive --id from path; supply --id explicitly")
		}
	}
	if !routeIDFlagPattern.MatchString(id) {
		return site.Route{}, fmt.Errorf("--id %q must match [a-z0-9][a-z0-9-]*", id)
	}

	preserve := routeAddFlags.preserveHost
	return site.Route{
		ID:               id,
		Path:             routeAddFlags.path,
		PathRegex:        routeAddFlags.pathRegex,
		Rewrite:          routeAddFlags.rewrite,
		Upstream:         upstream,
		PreserveHost:     &preserve,
		PassRangeHeaders: routeAddFlags.rangeHeaders,
		Priority:         routeAddFlags.priority,
	}, nil
}

var routeIDFlagPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// routeTargetNames returns the union of site names and proxy names for shell
// completion of `srv route` subcommands.
func routeTargetNames() []string {
	out := GetSiteNames()
	out = append(out, proxy.ListNames()...)
	return out
}

// autoRouteID derives a stable id from --path ("/app" → "app", "/api/v1" →
// "api-v1") or from a regex when its first literal segment is obvious. Returns
// "" when nothing usable can be derived; caller must supply --id.
func autoRouteID(path, regex string) string {
	src := path
	if src == "" {
		src = regex
	}
	src = strings.Trim(src, "/^$")
	src = strings.ToLower(src)
	id := strings.Builder{}
	for _, r := range src {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			id.WriteRune(r)
		case r == '/' || r == '-' || r == '_' || r == ' ':
			if id.Len() == 0 || id.String()[id.Len()-1] == '-' {
				continue
			}
			id.WriteRune('-')
		default:
			// Skip non-identifier characters from regex literals.
		}
	}
	return strings.Trim(id.String(), "-")
}

// upstreamFromFlags maps the three --port / --container / --url flags onto a
// site.Upstream. Exactly one form must be set.
func upstreamFromFlags() (site.Upstream, error) {
	forms := 0
	if routeAddFlags.port != 0 {
		forms++
	}
	if routeAddFlags.container != "" {
		forms++
	}
	if routeAddFlags.url != "" {
		forms++
	}
	if forms == 0 {
		return site.Upstream{}, fmt.Errorf("one of --port, --container, --url is required")
	}
	if forms > 1 {
		return site.Upstream{}, fmt.Errorf("--port, --container, --url are mutually exclusive")
	}
	switch {
	case routeAddFlags.port != 0:
		return site.Upstream{Kind: "localhost", Port: routeAddFlags.port}, nil
	case routeAddFlags.container != "":
		name, port, err := splitContainerPort(routeAddFlags.container)
		if err != nil {
			return site.Upstream{}, err
		}
		return site.Upstream{Kind: "container", Container: name, Port: port}, nil
	default:
		return site.Upstream{Kind: "url", URL: routeAddFlags.url}, nil
	}
}

func splitContainerPort(s string) (string, int, error) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("--container must be name:port")
	}
	var port int
	if _, err := fmt.Sscanf(parts[1], "%d", &port); err != nil || port <= 0 {
		return "", 0, fmt.Errorf("invalid container port %q", parts[1])
	}
	return parts[0], port, nil
}

func describeUpstream(kind, container, url string, port int) string {
	switch kind {
	case "localhost":
		return fmt.Sprintf("localhost:%d", port)
	case "container":
		return fmt.Sprintf("container %s:%d", container, port)
	case "url":
		return url
	}
	return "(unknown)"
}
