// Package traefik — portconflict.go isolates the port-conflict detection +
// auto-fix logic used by `srv install` and `srv doctor`. The split from
// traefik.go keeps the larger config-generation file focused on Traefik
// config and not on host process management.
package traefik

import (
	"fmt"

	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/shell"
)

// CheckPortAvailable checks if a port is available for binding.
func CheckPortAvailable(port int) bool {
	portStr := fmt.Sprintf("%d", port)
	inUse, err := shell.CheckPort(portStr)
	if err != nil {
		return true // Assume available if we can't check
	}
	return !inUse
}

// PortConflict describes a port that is already in use by a non-srv process.
type PortConflict struct {
	Port    int
	Name    string // human-readable name, e.g. "HTTP"
	Process string // process name if identifiable, else ""
}

// StopHint returns an actionable command string to stop the conflicting
// process, or a generic message if the process could not be identified.
func (c PortConflict) StopHint() string {
	switch c.Process {
	case "nginx", "lighttpd", "caddy", "apache2", "httpd":
		return fmt.Sprintf("sudo systemctl stop %s", c.Process)
	default:
		if c.Process != "" {
			return fmt.Sprintf("sudo systemctl stop %s", c.Process)
		}
		return fmt.Sprintf("identify and stop the process using port %d (try: sudo ss -tlnp | grep :%d)", c.Port, c.Port)
	}
}

// CanAutoFix reports whether srv knows how to fix this conflict automatically.
func (c PortConflict) CanAutoFix() bool {
	switch c.Process {
	case "nginx", "apache2", "httpd", "lighttpd", "caddy":
		return true
	}
	return false
}

// AutoFix attempts to automatically resolve the port conflict.
func (c PortConflict) AutoFix() error {
	switch c.Process {
	case "nginx", "lighttpd", "caddy", "apache2", "httpd":
		return shell.SudoRun("systemctl", "stop", c.Process)
	default:
		return fmt.Errorf("no automatic fix available for process %q", c.Process)
	}
}

// CheckPortConflicts checks whether any of the ports srv requires are
// occupied by a non-srv process. It skips ports already owned by srv
// containers. Only ports 80, 443, and 53 are checked; port 8080 (dashboard)
// is advisory only.
//
// Port 53 is checked on 127.0.0.1 specifically, because the dnsmasq
// container binds "127.0.0.1:53:53/udp" — not 0.0.0.0:53. systemd-resolved's
// stub listener on 127.0.0.53:53 does not conflict with this binding and is
// not reported as a conflict.
func CheckPortConflicts() []PortConflict {
	type check struct {
		port      int
		bindAddr  string // specific bind address to check; "" means 0.0.0.0
		name      string
		ownedByFn func() bool
	}

	checks := []check{
		{constants.PortHTTP, "", constants.PortNameHTTP, IsRunning},
		{constants.PortHTTPS, "", constants.PortNameHTTPS, IsRunning},
		// DNS binds on 127.0.0.1 only — check that address, not 0.0.0.0.
		{constants.PortDNS, constants.LocalhostIP, constants.PortNameDNS, IsDNSRunning},
	}

	var conflicts []PortConflict
	for _, c := range checks {
		portStr := fmt.Sprintf("%d", c.port)
		inUse, _ := shell.CheckPortOnAddr(c.bindAddr, portStr)
		if !inUse {
			continue // port is free at the relevant address
		}
		if c.ownedByFn() {
			continue // port is ours — no conflict
		}
		process := shell.IdentifyPortProcess(portStr)
		conflicts = append(conflicts, PortConflict{
			Port:    c.port,
			Name:    c.name,
			Process: process,
		})
	}
	return conflicts
}
