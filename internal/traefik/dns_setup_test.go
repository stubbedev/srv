package traefik

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/shell"
	"github.com/stubbedev/srv/internal/shell/shelltest"
)

// uniqueDomain returns a non-local-TLD domain that cannot match any pre-existing
// /etc/systemd/resolved.conf.d/srv-local.conf content on a CI runner — that
// would otherwise trigger the byte-identical early-return in
// updateSystemdResolvedConfig and bypass the faked shell error.
func uniqueDomain(t *testing.T) []string {
	t.Helper()
	return []string{fmt.Sprintf("srv-test-%d.dev", time.Now().UnixNano())}
}

func swapShell(t *testing.T, r shell.Runner) {
	t.Helper()
	t.Cleanup(shell.SwapDefault(r))
}

func TestUpdateSystemdResolvedConfigWrites(t *testing.T) {
	fake := shelltest.New(nil)
	swapShell(t, fake)
	if err := updateSystemdResolvedConfig([]string{"foo.dev"}); err != nil {
		t.Fatalf("err: %v", err)
	}
	calls := fake.Snapshot()
	var sawWrite, sawRestart, sawMkdir bool
	for _, c := range calls {
		switch c.Method {
		case "SudoMkdir":
			sawMkdir = true
		case "SudoWrite":
			sawWrite = true
			if !strings.Contains(c.Stdin, "~foo.dev") {
				t.Errorf("config missing custom domain: %q", c.Stdin)
			}
			if !strings.Contains(c.Stdin, "~test") {
				t.Errorf("config missing test TLD: %q", c.Stdin)
			}
		case "SudoSystemctl":
			sawRestart = true
		}
	}
	if !sawMkdir || !sawWrite || !sawRestart {
		t.Errorf("missing calls: mkdir=%v write=%v restart=%v", sawMkdir, sawWrite, sawRestart)
	}
}

func TestUpdateSystemdResolvedConfigSkipsTLDChildren(t *testing.T) {
	fake := shelltest.New(nil)
	swapShell(t, fake)
	if err := updateSystemdResolvedConfig([]string{"foo.test", "bar.dev"}); err != nil {
		t.Fatal(err)
	}
	for _, c := range fake.Snapshot() {
		if c.Method == "SudoWrite" && strings.Contains(c.Stdin, "~foo.test") {
			t.Errorf("foo.test should be covered by ~test TLD: %q", c.Stdin)
		}
	}
}

// .local must be routed per-name, never as the whole ~local TLD, so unrelated
// LAN mDNS names (other-host.local) keep resolving via mDNS.
func TestUpdateSystemdResolvedConfigDotLocalPerName(t *testing.T) {
	fake := shelltest.New(nil)
	swapShell(t, fake)
	if err := updateSystemdResolvedConfig([]string{"grafana.local"}); err != nil {
		t.Fatal(err)
	}
	for _, c := range fake.Snapshot() {
		if c.Method != "SudoWrite" {
			continue
		}
		if !strings.Contains(c.Stdin, "~grafana.local") {
			t.Errorf("grafana.local should be routed per-name: %q", c.Stdin)
		}
		for _, tok := range strings.Fields(c.Stdin) {
			if tok == "~local" {
				t.Errorf("~local TLD-wide route must not be emitted: %q", c.Stdin)
			}
		}
	}
}

func TestUpdateSystemdResolvedConfigMkdirErr(t *testing.T) {
	fake := shelltest.New(map[string]shelltest.Response{
		"sudo:mkdir": {Err: errors.New("boom")},
	})
	swapShell(t, fake)
	if err := updateSystemdResolvedConfig(uniqueDomain(t)); err == nil {
		t.Error("expected mkdir err to propagate")
	}
}

func TestUpdateSystemdResolvedConfigWriteErr(t *testing.T) {
	fake := shelltest.New(map[string]shelltest.Response{
		"sudo:tee": {Err: errors.New("boom")},
	})
	swapShell(t, fake)
	if err := updateSystemdResolvedConfig(uniqueDomain(t)); err == nil {
		t.Error("expected write err")
	}
}

func TestUpdateSystemdResolvedConfigSystemctlErr(t *testing.T) {
	fake := shelltest.New(map[string]shelltest.Response{
		"sudo:systemctl": {Err: errors.New("nope")},
	})
	swapShell(t, fake)
	if err := updateSystemdResolvedConfig(uniqueDomain(t)); err == nil {
		t.Error("expected systemctl err")
	}
}

func TestUpdateNetworkManagerConfigWrites(t *testing.T) {
	fake := shelltest.New(nil)
	swapShell(t, fake)
	if err := updateNetworkManagerConfig([]string{"app.dev"}); err != nil {
		t.Fatalf("err: %v", err)
	}
	var sawWrite, sawRestart bool
	for _, c := range fake.Snapshot() {
		if c.Method == "SudoWrite" {
			sawWrite = true
			if !strings.Contains(c.Stdin, "server=/app.dev/127.0.0.1") {
				t.Errorf("missing app.dev server line: %q", c.Stdin)
			}
			if !strings.Contains(c.Stdin, "server=/test/127.0.0.1") {
				t.Errorf("missing test TLD: %q", c.Stdin)
			}
		}
		if c.Method == "SudoSystemctl" {
			sawRestart = true
		}
	}
	if !sawWrite || !sawRestart {
		t.Errorf("missing calls: write=%v restart=%v", sawWrite, sawRestart)
	}
}

func TestUpdateNetworkManagerConfigErrs(t *testing.T) {
	cases := []struct {
		key string
	}{
		{"sudo:mkdir"},
		{"sudo:tee"},
		{"sudo:systemctl"},
	}
	for _, c := range cases {
		fake := shelltest.New(map[string]shelltest.Response{
			c.key: {Err: errors.New("boom")},
		})
		swapShell(t, fake)
		if err := updateNetworkManagerConfig(nil); err == nil {
			t.Errorf("%s err should propagate", c.key)
		}
	}
}

func TestUpdateMacOSResolverConfig(t *testing.T) {
	fake := shelltest.New(nil)
	swapShell(t, fake)
	// Function should run without panicking even when run on Linux — it just
	// writes resolver files via sudo tee, which is fully faked.
	_ = updateMacOSResolverConfig([]string{"foo.dev"})
	calls := fake.Snapshot()
	// At least one SudoWrite was attempted.
	sawWrite := false
	for _, c := range calls {
		if c.Method == "SudoWrite" {
			sawWrite = true
			break
		}
	}
	if !sawWrite {
		t.Error("expected SudoWrite for macOS resolver")
	}
}

func TestGetResolverName(t *testing.T) {
	// Just exercise the function on this platform.
	got := GetResolverName()
	switch got {
	case "systemd-resolved", "macOS resolver", "NetworkManager", "unknown":
		// all valid
	default:
		t.Errorf("unexpected resolver name: %q", got)
	}
}

func TestSetupDNSDelegates(t *testing.T) {
	// On Linux SetupDNS calls setupSystemdResolved which calls
	// LoadLocalDomains (returns empty here because SRV_ROOT isn't set yet)
	// and then updateSystemdResolvedConfig. With shell fake, no real
	// system change happens.
	t.Setenv("SRV_ROOT", t.TempDir())
	fake := shelltest.New(nil)
	swapShell(t, fake)
	_ = SetupDNS() // result depends on platform; just exercising the path
}

func TestCheckDNSDelegates(t *testing.T) {
	// CheckDNS does live DNS lookups — we just call it to exercise the code
	// path; result is platform-dependent.
	_ = CheckDNS("nonexistent-srv-test.invalid")
}

func TestFlushDNSCache(t *testing.T) {
	fake := shelltest.New(nil)
	swapShell(t, fake)
	FlushDNSCache() // no return value; just confirm no panic
}

func TestSetupSystemdResolvedCallsUpdate(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SRV_ROOT", root)
	config.ResetCache()
	t.Cleanup(config.ResetCache)
	// Seed a unique registered domain so the rendered config cannot match any
	// pre-existing /etc/systemd/resolved.conf.d/srv-local.conf — otherwise the
	// byte-identical early-return in updateSystemdResolvedConfig would silence
	// the shell calls this test is asserting on.
	traefikDir := filepath.Join(root, "traefik")
	if err := os.MkdirAll(traefikDir, 0o755); err != nil {
		t.Fatal(err)
	}
	domain := fmt.Sprintf("srv-test-%d.dev", time.Now().UnixNano())
	if err := os.WriteFile(filepath.Join(traefikDir, "local-domains.txt"), []byte(domain+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := shelltest.New(nil)
	swapShell(t, fake)
	_ = setupSystemdResolved()
	if len(fake.Snapshot()) == 0 {
		t.Error("expected setupSystemdResolved to invoke shell calls")
	}
}

func TestSetupNetworkManagerCallsUpdate(t *testing.T) {
	t.Setenv("SRV_ROOT", t.TempDir())
	fake := shelltest.New(nil)
	swapShell(t, fake)
	_ = setupNetworkManager()
	if len(fake.Snapshot()) == 0 {
		t.Error("expected setupNetworkManager to invoke shell calls")
	}
}

func TestRemoveDNSExercise(t *testing.T) {
	t.Setenv("SRV_ROOT", t.TempDir())
	swapShell(t, shelltest.New(nil))
	_ = RemoveDNS() // result depends on host's resolver state
}

func TestGetResolverNameOnHost(t *testing.T) {
	got := GetResolverName()
	switch got {
	case "systemd-resolved", "macOS resolver", "NetworkManager", "unknown":
	default:
		t.Errorf("unexpected: %q", got)
	}
}

func TestFlushDNSCacheCallsResolver(t *testing.T) {
	fake := shelltest.New(map[string]shelltest.Response{
		"systemd-resolve": {Exists: true},
		"resolvectl":      {Exists: true},
	})
	swapShell(t, fake)
	FlushDNSCache()
}
