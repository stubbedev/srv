// Package cmd — route.go implements `srv route` for attaching extra Traefik
// routers (path-prefix / regex-rewrite) to an existing site or proxy.
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/stubbedev/srv/internal/proxy"
	"github.com/stubbedev/srv/internal/site"
	"github.com/stubbedev/srv/internal/ui"
)

var routeAddFlags struct {
	id                 string
	path               string
	pathRegex          string
	rewrite            string
	port               int
	container          string
	url                string
	preserveHost       bool
	rangeHeaders       bool
	priority           int
	insecureSkipVerify bool
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
	routeAddCmd.Flags().BoolVar(&routeAddFlags.insecureSkipVerify, "insecure-skip-verify", false, "Skip TLS cert verification for an https --url upstream (self-signed / mismatched cert)")

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
	// Route attaches to whichever of site/proxy exists. Orchestration lives in
	// internal/site and internal/proxy, shared with the MCP add_route tool.
	switch {
	case site.Exists(target):
		if err := site.AddRoute(target, route); err != nil {
			return err
		}
		ui.Success("Added route %q on %s", route.ID, target)
	case proxy.Exists(target):
		if err := proxy.AddRoute(target, route); err != nil {
			return err
		}
		ui.Success("Added route %q on proxy %s", route.ID, target)
	default:
		return fmt.Errorf("no site or proxy named %q", target)
	}
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
	switch {
	case site.Exists(target):
		if err := site.RemoveRoute(target, id); err != nil {
			return err
		}
		ui.Success("Removed route %q from %s", id, target)
	case proxy.Exists(target):
		if err := proxy.RemoveRoute(target, id); err != nil {
			return err
		}
		ui.Success("Removed route %q from proxy %s", id, target)
	default:
		return fmt.Errorf("no site or proxy named %q", target)
	}
	return nil
}

// buildRouteFromFlags maps the routeAddFlags snapshot onto site.RouteInput and
// builds the route via the shared internal/site validator.
func buildRouteFromFlags() (site.Route, error) {
	preserve := routeAddFlags.preserveHost
	return site.BuildRoute(site.RouteInput{
		ID:                 routeAddFlags.id,
		Path:               routeAddFlags.path,
		PathRegex:          routeAddFlags.pathRegex,
		Rewrite:            routeAddFlags.rewrite,
		Port:               routeAddFlags.port,
		Container:          routeAddFlags.container,
		URL:                routeAddFlags.url,
		PreserveHost:       &preserve,
		PassRangeHeaders:   routeAddFlags.rangeHeaders,
		Priority:           routeAddFlags.priority,
		InsecureSkipVerify: routeAddFlags.insecureSkipVerify,
	})
}

// routeTargetNames returns the union of site names and proxy names for shell
// completion of `srv route` subcommands.
func routeTargetNames() []string {
	out := GetSiteNames()
	out = append(out, proxy.ListNames()...)
	return out
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
