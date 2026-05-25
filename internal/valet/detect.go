// Package valet detects a previously-installed Laravel Valet (the Linux port)
// so srv can offer to stop it before binding the same ports.
package valet

import (
	"os"
	"path/filepath"

	"github.com/stubbedev/srv/internal/shell"
)

// candidateUnits is the set of systemd units a Valet-on-Linux install typically
// owns. Probed via `systemctl is-active`; only the active ones surface.
var candidateUnits = []string{
	"nginx",
	"valet-dnsmasq",
	"php8.4-fpm",
	"php8.3-fpm",
	"php8.2-fpm",
	"php8.1-fpm",
	"php8.0-fpm",
	"php-fpm",
}

// Active reports the running Valet units (if any) and whether a Valet config
// directory exists. Both signals matter:
//   - a directory in $HOME without running units is a stale install srv can
//     ignore for the install flow (the importer still finds it later);
//   - units without a directory means another stack happens to share Valet's
//     unit names — srv should not assume.
func Active() (units []string, configDir string) {
	configDir = valetConfigDir()
	units = runningUnits()
	return units, configDir
}

// valetConfigDir returns the first existing Valet config directory under $HOME.
// Returns "" when neither exists.
func valetConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	for _, sub := range []string{".valet", ".config/valet"} {
		p := filepath.Join(home, sub)
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			return p
		}
	}
	return ""
}

// runningUnits returns the candidate Valet systemd units currently active.
// Empty when systemctl is unavailable or none are active.
func runningUnits() []string {
	if !shell.Default.Exists("systemctl") {
		return nil
	}
	var active []string
	for _, u := range candidateUnits {
		out, err := shell.Default.RunQuiet("systemctl", "is-active", u)
		if err != nil {
			continue
		}
		if isActiveOutput(out) {
			active = append(active, u)
		}
	}
	return active
}

// isActiveOutput is the pure-logic half of runningUnits — `systemctl is-active`
// prints `active` for running units and other tokens (inactive/failed/unknown)
// otherwise.
func isActiveOutput(out []byte) bool {
	if len(out) == 0 {
		return false
	}
	// Trim trailing newline.
	for len(out) > 0 && (out[len(out)-1] == '\n' || out[len(out)-1] == '\r' || out[len(out)-1] == ' ') {
		out = out[:len(out)-1]
	}
	return string(out) == "active"
}

// Stop runs `sudo systemctl stop <unit>` for each named unit. Returns the
// first error encountered (the rest are still attempted). Callers should
// confirm with the user first.
func Stop(units []string) error {
	var firstErr error
	for _, u := range units {
		if err := shell.Default.SudoSystemctl("stop", u); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
