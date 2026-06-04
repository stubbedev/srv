// Package traefik — reconcile.go applies the edge config (traefik.yml,
// docker-compose.yml, dnsmasq) and recreates the Traefik + DNS containers when
// the srv binary is upgraded.
//
// srv generates this config once at `srv install` time; nothing regenerates it
// afterwards. A package-manager upgrade (home-manager, brew) swaps the binary
// but leaves the old config in place, so new behaviour does not take effect
// until the user re-runs `srv install`. ReconcileVersion closes that gap on
// first use after an upgrade, keyed off a version marker — as a seamless
// in-place update: it rewrites the config files (Traefik and dnsmasq hot-reload
// those, no container disruption) and only brings the stack up when the compose
// definition itself changed, via plain `up -d` so unchanged containers keep
// running untouched.
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

// ReconcileVersion re-applies the edge config in place when the recorded marker
// differs from the running binary version. Config files are always refreshed
// (hot-reloaded by Traefik/dnsmasq); containers are only brought up — never
// force-recreated — when the compose definition changed. It is a no-op when:
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

	// Snapshot the compose definition so we only touch containers if the new
	// version actually changed it. Most upgrades only change the config files
	// Traefik and dnsmasq hot-reload (dynamic config, dnsmasq.conf) — those
	// apply with zero container disruption.
	composePath := cfg.TraefikComposePath()
	composeBefore, _ := os.ReadFile(composePath)

	// Re-render config from the new binary's templates. EnsureConfig is
	// idempotent and skips unchanged files. The persisted ACME email is reused
	// ("" simply leaves production SSL disabled, which is the existing state).
	email, _ := GetEmail("")
	if err := EnsureConfig(email); err != nil {
		return false, fmt.Errorf("reconcile edge config: %w", err)
	}

	// Bring the stack up in place ONLY when the compose definition changed
	// (new image tag, entrypoint, or bind mount). Plain `up -d` — never
	// --force-recreate — so docker recreates only the service(s) whose
	// definition actually changed and leaves everything else running. When the
	// compose is unchanged the containers are not touched at all.
	composeAfter, _ := os.ReadFile(composePath)
	if IsRunning() && string(composeBefore) != string(composeAfter) {
		if err := docker.Compose(cfg.TraefikDir, "up", "-d"); err != nil {
			return false, fmt.Errorf("apply edge container changes: %w", err)
		}
	}

	if err := MarkInstalled(cfg, version); err != nil {
		return false, err
	}
	return true, nil
}
