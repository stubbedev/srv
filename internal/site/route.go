// Package site — route.go builds and applies extra Traefik routes to a site.
// The CLI (`srv route`) and the MCP route tools share BuildRoute (validation +
// id derivation + upstream resolution) and the site-side apply helpers. Proxy
// routes live in internal/proxy (which imports this package), and the
// site-or-proxy dispatch lives in the caller, because site cannot import proxy.
package site

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/stubbedev/srv/internal/constants"
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
	// InsecureSkipVerify skips TLS verification on an https url upstream.
	InsecureSkipVerify bool
}

// BuildRoute validates the input and returns a site.Route, deriving the id from
// the path when not supplied.
func BuildRoute(in RouteInput) (Route, error) {
	if in.Path != "" && in.PathRegex != "" {
		return Route{}, errors.New("path and path_regex are mutually exclusive")
	}
	if in.Path == "" && in.PathRegex == "" {
		return Route{}, errors.New("one of path or path_regex is required")
	}
	if in.Rewrite != "" && in.PathRegex == "" {
		return Route{}, errors.New("rewrite requires path_regex")
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
			return Route{}, errors.New("could not derive id from path; supply id explicitly")
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
		return Upstream{}, errors.New("one of port, container, url is required")
	}
	if forms > 1 {
		return Upstream{}, errors.New("port, container, url are mutually exclusive")
	}
	if in.InsecureSkipVerify && in.URL == "" {
		return Upstream{}, errors.New("insecure_skip_verify only applies to a url upstream")
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
		return Upstream{Kind: "url", URL: in.URL, InsecureSkipVerify: in.InsecureSkipVerify}, nil
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

// SplitContainerPort parses a "name:port" upstream spec. Both halves are
// validated here because the result goes straight into a Traefik service URL:
// an empty name yields "http://:3000" and an out-of-range port yields a router
// Traefik rejects at load time, taking every other route on the site with it.
func SplitContainerPort(s string) (string, int, error) {
	name, portStr, ok := strings.Cut(s, ":")
	if !ok {
		return "", 0, errors.New("container must be name:port")
	}
	if name == "" {
		return "", 0, errors.New("container name cannot be empty")
	}
	var port int
	if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil {
		return "", 0, fmt.Errorf("invalid container port %q", portStr)
	}
	if port < constants.PortMin || port > constants.PortMax {
		return "", 0, fmt.Errorf("invalid container port %q: out of range 1-%d", portStr, constants.PortMax)
	}
	return name, port, nil
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
