// Package site — route.go builds and applies extra Traefik routes to a site.
// The CLI (`srv route`) and the MCP route tools share BuildRoute (validation +
// id derivation + upstream resolution) and the site-side apply helpers. Proxy
// routes live in internal/proxy (which imports this package), and the
// site-or-proxy dispatch lives in the caller, because site cannot import proxy.
package site

import (
	"fmt"
	"regexp"
	"strings"
)

// routeIDPattern (the allowed shape for a route id) is declared in reload.go.

// RouteInput is the explicit, surface-agnostic description of a route to build.
// Exactly one of Port / Container / URL must be set; exactly one of Path /
// PathRegex must be set.
type RouteInput struct {
	ID               string
	Path             string
	PathRegex        string
	Rewrite          string
	Port             int    // localhost upstream
	Container        string // "name:port" upstream
	URL              string // raw URL upstream
	PreserveHost     *bool  // nil → true
	PassRangeHeaders bool
	Priority         int
}

// BuildRoute validates the input and returns a site.Route, deriving the id from
// the path when not supplied.
func BuildRoute(in RouteInput) (Route, error) {
	if in.Path != "" && in.PathRegex != "" {
		return Route{}, fmt.Errorf("path and path_regex are mutually exclusive")
	}
	if in.Path == "" && in.PathRegex == "" {
		return Route{}, fmt.Errorf("one of path or path_regex is required")
	}
	if in.Rewrite != "" && in.PathRegex == "" {
		return Route{}, fmt.Errorf("rewrite requires path_regex")
	}
	if in.PathRegex != "" {
		if _, err := regexp.Compile(in.PathRegex); err != nil {
			return Route{}, fmt.Errorf("invalid path_regex: %w", err)
		}
	}

	upstream, err := buildUpstream(in)
	if err != nil {
		return Route{}, err
	}

	id := strings.TrimSpace(in.ID)
	if id == "" {
		id = autoRouteID(in.Path, in.PathRegex)
		if id == "" {
			return Route{}, fmt.Errorf("could not derive id from path; supply id explicitly")
		}
	}
	if !routeIDPattern.MatchString(id) {
		return Route{}, fmt.Errorf("id %q must match [a-z0-9][a-z0-9-]*", id)
	}

	preserve := true
	if in.PreserveHost != nil {
		preserve = *in.PreserveHost
	}
	return Route{
		ID:               id,
		Path:             in.Path,
		PathRegex:        in.PathRegex,
		Rewrite:          in.Rewrite,
		Upstream:         upstream,
		PreserveHost:     &preserve,
		PassRangeHeaders: in.PassRangeHeaders,
		Priority:         in.Priority,
	}, nil
}

func buildUpstream(in RouteInput) (Upstream, error) {
	forms := 0
	if in.Port != 0 {
		forms++
	}
	if in.Container != "" {
		forms++
	}
	if in.URL != "" {
		forms++
	}
	if forms == 0 {
		return Upstream{}, fmt.Errorf("one of port, container, url is required")
	}
	if forms > 1 {
		return Upstream{}, fmt.Errorf("port, container, url are mutually exclusive")
	}
	switch {
	case in.Port != 0:
		return Upstream{Kind: "localhost", Port: in.Port}, nil
	case in.Container != "":
		name, port, err := SplitContainerPort(in.Container)
		if err != nil {
			return Upstream{}, err
		}
		return Upstream{Kind: "container", Container: name, Port: port}, nil
	default:
		return Upstream{Kind: "url", URL: in.URL}, nil
	}
}

// AddRoute appends a route to a site's metadata and reloads it.
func AddRoute(name string, route Route) error {
	meta, err := requireMeta(name)
	if err != nil {
		return err
	}
	for _, existing := range meta.Routes {
		if existing.ID == route.ID {
			return fmt.Errorf("route %q already exists on %s — remove it first or pick a different id", route.ID, name)
		}
	}
	meta.Routes = append(meta.Routes, route)
	if err := ValidateMetadata(meta); err != nil {
		return fmt.Errorf("route would produce invalid metadata: %w", err)
	}
	if err := WriteSiteMetadata(name, *meta); err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}
	if _, err := Reload(name); err != nil {
		return fmt.Errorf("refresh routing config: %w", err)
	}
	return nil
}

// RemoveRoute drops a route by id from a site's metadata and reloads it.
func RemoveRoute(name, id string) error {
	meta, err := requireMeta(name)
	if err != nil {
		return err
	}
	filtered, removed := DropRoute(meta.Routes, id)
	if !removed {
		return fmt.Errorf("route %q not found on %s", id, name)
	}
	meta.Routes = filtered
	if err := WriteSiteMetadata(name, *meta); err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}
	if _, err := Reload(name); err != nil {
		return fmt.Errorf("refresh routing config: %w", err)
	}
	return nil
}

// DropRoute returns routes with the entry matching id removed, and whether one
// was removed. Shared by the site and proxy route removers.
func DropRoute(routes []Route, id string) ([]Route, bool) {
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

// SplitContainerPort parses a "name:port" upstream spec.
func SplitContainerPort(s string) (string, int, error) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("container must be name:port")
	}
	var port int
	if _, err := fmt.Sscanf(parts[1], "%d", &port); err != nil || port <= 0 {
		return "", 0, fmt.Errorf("invalid container port %q", parts[1])
	}
	return parts[0], port, nil
}

// autoRouteID derives a route id from a path or regex source.
func autoRouteID(path, regex string) string {
	src := path
	if src == "" {
		src = regex
	}
	src = strings.ToLower(strings.Trim(src, "/^$"))
	var id strings.Builder
	for _, r := range src {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			id.WriteRune(r)
		case r == '/' || r == '-' || r == '_' || r == ' ':
			if id.Len() == 0 || id.String()[id.Len()-1] == '-' {
				continue
			}
			id.WriteRune('-')
		}
	}
	return strings.Trim(id.String(), "-")
}
