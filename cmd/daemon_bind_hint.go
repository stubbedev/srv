// Package cmd — daemon_bind_hint.go annotates bind failures on the daemon's
// own ports: when the running daemon is the holder, `srv dnsd` / `srv httpd`
// say so instead of surfacing a bare "address already in use", keeping the
// one-daemon-serves-everything rule visible at every entrypoint.
package cmd

import (
	"errors"
	"fmt"
	"syscall"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/daemon"
)

// daemonBindHint returns an error explaining that the srv daemon already
// serves the port, or nil when err is not a bind conflict on a daemon-owned
// port or no daemon is behind it. daemonOwnedPort marks the daemon's fixed
// ports; a conflict on a custom --port belongs to whatever else holds it.
func daemonBindHint(err error, daemonOwnedPort bool) error {
	if !daemonOwnedPort || !errors.Is(err, syscall.EADDRINUSE) {
		return nil
	}
	cfg, cfgErr := config.Load()
	if cfgErr == nil {
		if _, held := daemon.ServingPid(cfg); held || daemon.IsRunning() {
			return fmt.Errorf("%w\n  the srv daemon already serves this port: one daemon handles DNS and static sites for every site; stop it with 'srv daemon stop', or pass --port for a throwaway instance", err)
		}
	}
	return nil
}
