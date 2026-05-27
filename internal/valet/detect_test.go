package valet

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/srv/internal/shell"
	"github.com/stubbedev/srv/internal/shell/shelltest"
)

func TestIsActiveOutput(t *testing.T) {
	cases := map[string]bool{
		"active\n":   true,
		"active":     true,
		"active  \n": true,
		"inactive\n": false,
		"failed\n":   false,
		"activating": false,
		"":           false,
		"unknown":    false,
	}
	for in, want := range cases {
		if got := isActiveOutput([]byte(in)); got != want {
			t.Errorf("isActiveOutput(%q) = %v, want %v", in, got, want)
		}
	}
}

// withFakeHome points $HOME at a fresh tempdir for the lifetime of the test
// so valetConfigDir's lookups can be controlled without touching the
// developer's real ~/.valet or ~/.config/valet.
func withFakeHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

func TestValetConfigDirReturnsLegacyValetDir(t *testing.T) {
	home := withFakeHome(t)
	dotValet := filepath.Join(home, ".valet")
	if err := os.MkdirAll(dotValet, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := valetConfigDir(); got != dotValet {
		t.Errorf("got %q, want %q", got, dotValet)
	}
}

func TestValetConfigDirPrefersDotValetWhenBothPresent(t *testing.T) {
	// `.valet` is listed before `.config/valet` in the candidate list — the
	// legacy path wins when both exist, which keeps behaviour stable for
	// users who haven't migrated.
	home := withFakeHome(t)
	dotValet := filepath.Join(home, ".valet")
	xdg := filepath.Join(home, ".config", "valet")
	if err := os.MkdirAll(dotValet, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(xdg, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := valetConfigDir(); got != dotValet {
		t.Errorf("got %q, want %q", got, dotValet)
	}
}

func TestValetConfigDirReturnsXDGWhenLegacyMissing(t *testing.T) {
	home := withFakeHome(t)
	xdg := filepath.Join(home, ".config", "valet")
	if err := os.MkdirAll(xdg, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := valetConfigDir(); got != xdg {
		t.Errorf("got %q, want %q", got, xdg)
	}
}

func TestValetConfigDirReturnsEmptyWhenAbsent(t *testing.T) {
	withFakeHome(t)
	if got := valetConfigDir(); got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestRunningUnitsNoneWhenSystemctlAbsent(t *testing.T) {
	// shelltest.New with an empty map → Exists("systemctl") is false.
	restore := shell.SwapDefault(shelltest.New(nil))
	t.Cleanup(restore)
	if got := runningUnits(); got != nil {
		t.Errorf("expected nil with no systemctl, got %v", got)
	}
}

func TestRunningUnitsOnlyReturnsActive(t *testing.T) {
	// Mark nginx active, php8.3-fpm inactive, php8.4-fpm errored out.
	// shelltest's Handler lets us match on the unit name (args[1]).
	fake := shelltest.New(nil)
	fake.Handler = func(method, name string, args []string, _ string) (shelltest.Response, bool) {
		if method == "Exists" && name == "systemctl" {
			return shelltest.Response{Exists: true}, true
		}
		if method == "RunQuiet" && name == "systemctl" && len(args) == 2 && args[0] == "is-active" {
			switch args[1] {
			case "nginx":
				return shelltest.Response{Out: []byte("active\n")}, true
			case "php8.3-fpm":
				return shelltest.Response{Out: []byte("inactive\n")}, true
			case "php8.4-fpm":
				return shelltest.Response{Err: errors.New("not loaded")}, true
			}
			// Treat all other units as inactive.
			return shelltest.Response{Out: []byte("inactive\n")}, true
		}
		return shelltest.Response{}, false
	}
	restore := shell.SwapDefault(fake)
	t.Cleanup(restore)

	got := runningUnits()
	if len(got) != 1 || got[0] != "nginx" {
		t.Errorf("expected [nginx], got %v", got)
	}
}
