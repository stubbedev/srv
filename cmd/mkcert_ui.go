package cmd

import (
	"bytes"
	"os"
	"runtime"

	"github.com/stubbedev/srv/internal/mkcert"
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

func printBrowserTrustHelp() {
	ui.Dim("Browser (Firefox/Chrome) trust unavailable — certutil is not installed")
	ui.Dim("or no NSS profile was found. Install nss-tools and re-run if needed.")
}

// isNixOS reports whether the host is running NixOS by checking
// /etc/os-release for ID=nixos.
func isNixOS() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return false
	}
	return bytes.Contains(data, []byte("ID=nixos"))
}
