package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/stubbedev/srv/internal/mkcert"
	"github.com/stubbedev/srv/internal/platform"
	"github.com/stubbedev/srv/internal/ui"
)

// reportCAInstall renders a clean, distro-aware status for the outcome of
// `mkcert -install`. mkcert prints multi-line warnings (and emoji-laden
// notes) to stderr that are noisy and confusing on platforms where it cannot
// auto-trust the CA — so we suppress that output upstream and surface a
// single, actionable message here.
//
// quiet=true suppresses the success line (used in `srv install` where the
// step framework already prints the headline).
func reportCAInstall(res mkcert.InstallResult, quiet bool) {
	if !quiet {
		switch {
		case res.SystemTrustOK:
			ui.Success("mkcert CA installed in system trust store")
		case res.NewCA && res.CARootPath != "":
			ui.Success("mkcert CA created at %s", res.CARootPath)
		default:
			ui.Success("mkcert CA installed")
		}
	}

	if !res.SystemTrustOK && res.SystemUnsupported {
		printSystemTrustHelp(res.CARootPath)
	}
	if !res.BrowserTrustOK && (res.BrowserUnavailable || res.CertutilMissing) {
		printBrowserTrustHelp()
	}
	if res.SystemTrustOK || res.BrowserTrustOK {
		ui.Dim("Restart your browser for the CA to take effect")
	}
}

func printSystemTrustHelp(caPath string) {
	if caPath == "" {
		caPath = "~/.local/share/mkcert/rootCA.pem"
	}
	if isNixOS() {
		ui.Dim("System trust store install isn't automated on NixOS.")
		ui.Dim("To trust system-wide, add to your NixOS config and rebuild:")
		ui.Code("  security.pki.certificateFiles = [ \"%s\" ];", caPath)
		return
	}
	ui.Dim("System trust store install isn't supported on this Linux.")
	ui.Dim("Manually trust the root CA at: %s", caPath)
}

// printBrowserTrustHelp explains why mkcert couldn't install browser trust and
// gives a concrete next step. We branch on (a) is certutil missing? and
// (b) does the user even have an NSS profile that mkcert could write to?
// Without certutil installed, mkcert can't touch any NSS db; without an NSS
// db, there's nothing for it to update. Either case is recoverable, but the
// fix differs.
func printBrowserTrustHelp() {
	_, lookErr := exec.LookPath("certutil")
	missingCertutil := lookErr != nil
	hasNSS := hasNSSProfile()

	switch {
	case missingCertutil && hasNSS:
		// certutil missing, NSS db exists — installing certutil + re-running
		// will copy the CA into place automatically.
		ui.Dim("Browser trust skipped — certutil is not installed.")
		if cmd := certutilInstallHint(); cmd != "" {
			ui.Dim("Install it and re-run 'srv install' to trust the CA in browsers:")
			ui.Code("  %s", cmd)
		} else {
			ui.Dim("Install nss-tools (provides certutil) and re-run 'srv install'.")
		}
	case missingCertutil && !hasNSS:
		// Both missing. User needs to run a browser at least once and install
		// certutil. Less common path — keep the message terse.
		ui.Dim("Browser trust skipped — no Firefox/Chrome profile and certutil is not installed.")
		ui.Dim("Run a browser once, install nss-tools, then re-run 'srv install'.")
	case !missingCertutil && !hasNSS:
		// certutil OK but no browser profile yet. mkcert needs an existing
		// NSS db to write to; opening Chrome/Chromium or Firefox once
		// creates one (~/.pki/nssdb or ~/.mozilla/firefox/<profile>).
		ui.Dim("Browser trust skipped — no Firefox/Chrome profile found.")
		ui.Dim("Open Chrome/Firefox once, then re-run 'srv install'.")
	default:
		// certutil present and a profile exists, yet mkcert reported failure.
		// Either an NSS error or partial state — point the user at the raw
		// log via VerboseLog rather than guessing.
		ui.Dim("Browser trust skipped — mkcert could not write to the NSS database.")
		ui.Dim("Run with --verbose to see mkcert's output.")
	}
}

// hasNSSProfile mirrors mkcert's own profile detection: a profile is "an
// existing cert9.db in any known location". Matching mkcert's logic keeps our
// hint accurate — we only suggest installing certutil when doing so would
// actually unblock mkcert on the next run.
func hasNSSProfile() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	candidates := []string{
		filepath.Join(home, ".pki", "nssdb"),
		filepath.Join(home, "snap", "chromium", "current", ".pki", "nssdb"),
		"/etc/pki/nssdb",
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	matches, _ := filepath.Glob(filepath.Join(home, ".mozilla", "firefox", "*"))
	return len(matches) > 0
}

// certutilInstallHint returns the canonical "install nss-tools" command for
// the host distro, or an empty string if we can't identify it. We parse
// /etc/os-release rather than running lsb_release: it's universal, doesn't
// require a subprocess, and gives us ID + ID_LIKE for derivative distros.
func certutilInstallHint() string {
	if !platform.IsLinux() {
		return ""
	}
	id, idLike := osReleaseIDs()
	families := append([]string{id}, idLike...)
	for _, f := range families {
		switch f {
		case "nixos":
			return "nix profile install nixpkgs#nss-tools"
		case "debian", "ubuntu":
			return "sudo apt install libnss3-tools"
		case "fedora", "rhel", "centos":
			return "sudo dnf install nss-tools"
		case "arch", "manjaro":
			return "sudo pacman -S nss"
		case "alpine":
			return "sudo apk add nss-tools"
		case "opensuse", "opensuse-leap", "opensuse-tumbleweed", "suse":
			return "sudo zypper install mozilla-nss-tools"
		}
	}
	return ""
}

// osReleaseIDs returns (ID, ID_LIKE…) parsed from /etc/os-release. Returns
// empty strings if the file can't be read.
func osReleaseIDs() (string, []string) {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "", nil
	}
	var id string
	var idLike []string
	for _, line := range strings.Split(string(data), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v = strings.Trim(v, `"`)
		switch k {
		case "ID":
			id = v
		case "ID_LIKE":
			idLike = strings.Fields(v)
		}
	}
	return id, idLike
}

// isNixOS reports whether the host is running NixOS by checking
// /etc/os-release for ID=nixos.
func isNixOS() bool {
	id, _ := osReleaseIDs()
	return id == "nixos"
}
