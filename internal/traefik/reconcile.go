// Package traefik — reconcile.go applies the edge config (traefik.yml,
// docker-compose.yml, dnsmasq) and recreates the Traefik + DNS containers when
// the srv binary is upgraded.
//
// srv generates this config once at `srv install` time; nothing regenerates it
// afterwards. A package-manager upgrade (home-manager, brew) swaps the binary
// but leaves the old config and old running containers in place, so new
// behaviour — image bumps, added entrypoints, new bind mounts — does not take
// effect until the user re-runs `srv install`. ReconcileVersion closes that gap
// by re-applying on first use after an upgrade, keyed off a version marker.
package traefik

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/fsutil"
)

// installedVersionPath is the marker recording which srv version last generated
// the edge config.
func installedVersionPath(cfg *config.Config) string {
	return filepath.Join(cfg.Root, ".installed-version")
}

// InstalledVersion returns the srv version that last wrote the edge config, or
// "" if unknown / never installed.
func InstalledVersion(cfg *config.Config) string {
	data, err := os.ReadFile(installedVersionPath(cfg))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// MarkInstalled records version as the one that generated the current edge
// config. Called by `srv install` and after a successful auto-reconcile.
func MarkInstalled(cfg *config.Config, version string) error {
	return fsutil.AtomicWriteFile(installedVersionPath(cfg), []byte(version+"\n"), constants.FilePermDefault)
}

// IsInstalled reports whether srv's edge config has been generated (the Traefik
// compose file exists) — a cheap proxy for "srv install has run".
func IsInstalled(cfg *config.Config) bool {
	_, err := os.Stat(cfg.TraefikComposePath())
	return err == nil
}

// ReconcileVersion re-applies the edge config and recreates the Traefik + DNS
// containers when the recorded marker differs from the running binary version.
// It is a no-op when:
//   - version is the "dev" placeholder (unversioned local build — the dev runs
//     `srv install` manually);
//   - srv is not installed yet (first install handles it);
//   - the marker already matches.
//
// Returns whether it actually reconciled, so callers can log it.
func ReconcileVersion(version string) (reconciled bool, err error) {
	if version == "" || version == constants.DefaultVersion {
		return false, nil
	}
	cfg, err := config.Load()
	if err != nil {
		return false, err
	}
	if !IsInstalled(cfg) || InstalledVersion(cfg) == version {
		return false, nil
	}

	// Re-render config from the new binary's templates. EnsureConfig is
	// idempotent and skips unchanged files. The persisted ACME email is reused
	// ("" simply leaves production SSL disabled, which is the existing state).
	email, _ := GetEmail("")
	if err := EnsureConfig(email); err != nil {
		return false, fmt.Errorf("reconcile edge config: %w", err)
	}

	// Recreate the edge containers so regenerated compose (new image tags,
	// entrypoints, bind mounts) takes effect — only when Traefik is currently
	// running, so a stopped stack is not started behind the user's back. The
	// compose covers both the traefik and dns services.
	if IsRunning() {
		if err := docker.Compose(cfg.TraefikDir, "up", "-d", "--force-recreate"); err != nil {
			return false, fmt.Errorf("recreate edge containers: %w", err)
		}
	}

	if err := MarkInstalled(cfg, version); err != nil {
		return false, err
	}
	return true, nil
}
