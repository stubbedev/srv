package daemon

import (
	"strings"
	"testing"
)

// The daemon installers must carry SRV_ROOT and SRV_CONTAINER_ENGINE into the
// service environment: they are precedence-1 overrides, and a daemon
// installed under one would otherwise silently point at the default
// root/engine. Without the vars set, neither unit may mention them.
func TestRenderedServicesCarryEnvOverrides(t *testing.T) {
	t.Setenv("SRV_ROOT", "/custom/srv-root")
	t.Setenv("SRV_CONTAINER_ENGINE", "podman")

	unit := renderSystemdUnit("/usr/bin/srv", "/home/u")
	for _, want := range []string{"SRV_ROOT=/custom/srv-root", "SRV_CONTAINER_ENGINE=podman"} {
		if !strings.Contains(unit, want) {
			t.Errorf("systemd unit missing %q:\n%s", want, unit)
		}
	}

	plist := renderLaunchdPlist("/usr/bin/srv", "/tmp/srv.log")
	for _, want := range []string{"SRV_ROOT", "SRV_CONTAINER_ENGINE", "/custom/srv-root"} {
		if !strings.Contains(plist, want) {
			t.Errorf("launchd plist missing %q:\n%s", want, plist)
		}
	}
}

func TestRenderedServicesOmitUnsetEnv(t *testing.T) {
	t.Setenv("SRV_ROOT", "")
	t.Setenv("SRV_CONTAINER_ENGINE", "")

	unit := renderSystemdUnit("/usr/bin/srv", "/home/u")
	plist := renderLaunchdPlist("/usr/bin/srv", "/tmp/srv.log")
	for _, out := range []string{unit, plist} {
		if strings.Contains(out, "SRV_ROOT") || strings.Contains(out, "SRV_CONTAINER_ENGINE") {
			t.Errorf("unset overrides must not appear:\n%s", out)
		}
	}
}
