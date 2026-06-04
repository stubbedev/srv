// Package proxy — route.go applies extra Traefik routes to a proxy's metadata.
// Mirrors site.AddRoute/RemoveRoute; the site-or-proxy dispatch lives in the
// caller (CLI / MCP) because internal/site cannot import this package.
package proxy

import (
	"fmt"

	"github.com/stubbedev/srv/internal/site"
)

// AddRoute appends a route to a proxy's metadata sidecar and regenerates its
// Traefik routes file.
func AddRoute(name string, route site.Route) error {
	meta, err := Read(name)
	if err != nil {
		return err
	}
	if meta == nil {
		return fmt.Errorf("proxy %q not found", name)
	}
	for _, existing := range meta.Routes {
		if existing.ID == route.ID {
			return fmt.Errorf("route %q already exists on %s — remove it first or pick a different id", route.ID, name)
		}
	}
	meta.Routes = append(meta.Routes, route)
	if err := Write(*meta); err != nil {
		return fmt.Errorf("write proxy metadata: %w", err)
	}
	if err := Reload(name); err != nil {
		return fmt.Errorf("refresh proxy routes: %w", err)
	}
	return nil
}

// RemoveRoute drops a route by id from a proxy's metadata sidecar.
func RemoveRoute(name, id string) error {
	meta, err := Read(name)
	if err != nil {
		return err
	}
	if meta == nil {
		return fmt.Errorf("proxy %q not found", name)
	}
	filtered, removed := site.DropRoute(meta.Routes, id)
	if !removed {
		return fmt.Errorf("route %q not found on proxy %s", id, name)
	}
	meta.Routes = filtered
	if err := Write(*meta); err != nil {
		return fmt.Errorf("write proxy metadata: %w", err)
	}
	if err := Reload(name); err != nil {
		return fmt.Errorf("refresh proxy routes: %w", err)
	}
	return nil
}
