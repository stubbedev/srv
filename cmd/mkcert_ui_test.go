package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/srv/internal/mkcert"
)

func TestOsReleaseIDsMissing(t *testing.T) {
	// Re-point /etc/os-release via the function's read path — we can't change
	// the absolute path, so test by calling it directly. On most CI systems
	// /etc/os-release exists, so we just verify it returns something or empty
	// gracefully.
	id, _ := osReleaseIDs()
	_ = id
}

func TestHasNSSProfileFalseInEmptyHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if hasNSSProfile() {
		t.Error("expected false for empty home")
	}
}

func TestHasNSSProfileFirefox(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	profileDir := filepath.Join(home, ".mozilla", "firefox", "abc.default")
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if !hasNSSProfile() {
		t.Error("expected true with firefox profile")
	}
}

func TestHasNSSProfileNssdb(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".pki", "nssdb"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !hasNSSProfile() {
		t.Error("expected true with .pki/nssdb")
	}
}

func TestReportCAInstallSystemTrust(t *testing.T) {
	// Just exercise the print paths — they write to stdout via ui.*.
	reportCAInstall(mkcert.InstallResult{SystemTrustOK: true}, false)
	reportCAInstall(mkcert.InstallResult{SystemTrustOK: true}, true)
}

func TestReportCAInstallNewCA(t *testing.T) {
	reportCAInstall(mkcert.InstallResult{NewCA: true, CARootPath: "/x/rootCA.pem"}, false)
}

func TestReportCAInstallSystemUnsupported(t *testing.T) {
	reportCAInstall(mkcert.InstallResult{SystemUnsupported: true, CARootPath: "/x"}, false)
}

func TestReportCAInstallBrowserUnavailable(t *testing.T) {
	reportCAInstall(mkcert.InstallResult{BrowserUnavailable: true}, true)
}

func TestPrintSystemTrustHelpEmptyCAPath(t *testing.T) {
	// Empty CA path triggers default fallback.
	printSystemTrustHelp("")
}

func TestPrintSystemTrustHelpWithPath(t *testing.T) {
	printSystemTrustHelp("/tmp/rootCA.pem")
}

func TestPrintBrowserTrustHelpAllBranches(t *testing.T) {
	// Just call the function in different states to exercise its branches.
	// We can't easily flip exec.LookPath, but we can flip hasNSSProfile by
	// changing $HOME.
	home := t.TempDir()
	t.Setenv("HOME", home)
	// No NSS profile → at least one branch exercised.
	printBrowserTrustHelp()
	// Add NSS profile so another branch fires.
	_ = os.MkdirAll(filepath.Join(home, ".pki", "nssdb"), 0o755)
	printBrowserTrustHelp()
}

func TestCertutilInstallHintNonLinux(t *testing.T) {
	// platform.IsLinux is fixed at compile time, so we can only verify the
	// branch we're on. On non-Linux this returns empty, on Linux it depends
	// on /etc/os-release. Just call and ensure no panic.
	_ = certutilInstallHint()
}

func TestIsNixOS(t *testing.T) {
	_ = isNixOS()
}

func TestColorFlagLine(t *testing.T) {
	id := func(s ...any) string {
		// noop colorer
		out := ""
		for _, x := range s {
			out += x.(string)
		}
		return out
	}
	cases := []struct {
		in   string
		want string
	}{
		{"  -f, --flag  Description here", "  -f, --flag  Description here"},
		{"plain text", "plain text"},
	}
	for _, c := range cases {
		got := colorFlagLine(c.in, id)
		if got != c.want {
			t.Errorf("colorFlagLine(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSplitLines(t *testing.T) {
	got := splitLines("a\nb\nc")
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("got %v", got)
	}
	if out := splitLines(""); len(out) != 0 {
		t.Errorf("empty -> %v", out)
	}
}
