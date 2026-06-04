// Package traefik — dns_avahi.go publishes srv's `.local` domains to Avahi's
// static-hosts file so they resolve through the system mDNS path.
//
// `.local` is reserved for mDNS (RFC 6762). On a box running Avahi with
// nss-mdns ahead of systemd-resolved in nsswitch (the common Linux default),
// `.local` lookups go to Avahi/multicast — NOT to srv's dnsmasq — so a srv
// `*.local` site silently fails to resolve in apps even though dnsmasq answers
// it directly. Writing the names to /etc/avahi/hosts makes Avahi itself answer
// them, so nss-mdns returns them; any `.local` name srv does NOT register stays
// available for normal mDNS discovery.
package traefik

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/platform"
	"github.com/stubbedev/srv/internal/shell"
)

const (
	avahiManagedStart = "# >>> srv managed — do not edit (srv publishes its .local domains here) >>>"
	avahiManagedEnd   = "# <<< srv managed <<<"
)

// avahiAvailable reports whether Avahi is present so srv should publish its
// `.local` names there. Linux-only; macOS uses /etc/resolver and Windows has no
// mDNS interception of this kind.
func avahiAvailable() bool {
	if !platform.IsLinux() {
		return false
	}
	if shell.Exists("avahi-daemon") {
		return true
	}
	_, err := os.Stat(constants.AvahiHostsPath)
	return err == nil
}

// dotLocalDomains returns the exact `.local` hostnames among the registry
// entries (wildcards reduced to their apex). The bare TLD "local" is skipped.
func dotLocalDomains(domains []string) []string {
	var out []string
	for _, d := range domains {
		bare := BareDomain(d)
		if bare == "local" {
			continue
		}
		if strings.HasSuffix(bare, ".local") {
			out = append(out, bare)
		}
	}
	sort.Strings(out)
	return out
}

// updateAvahiHosts syncs srv's `.local` domains into /etc/avahi/hosts inside a
// managed block (preserving any user-authored entries) and reloads Avahi. It is
// a no-op when the rendered file is unchanged. Writing the file and reloading
// need root, so under non-interactive sudo (the MCP server) a failure surfaces
// as a warning to the caller rather than blocking.
func updateAvahiHosts(domains []string) error {
	names := dotLocalDomains(domains)
	existing, err := os.ReadFile(constants.AvahiHostsPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", constants.AvahiHostsPath, err)
	}
	next := renderAvahiHosts(string(existing), names)
	if next == string(existing) {
		return nil
	}
	if err := shell.SudoWrite(constants.AvahiHostsPath, next); err != nil {
		return fmt.Errorf("write %s: %w", constants.AvahiHostsPath, err)
	}
	return reloadAvahi()
}

// renderAvahiHosts strips srv's existing managed block from the file, keeps all
// other lines verbatim, and appends a fresh managed block for names (omitting
// the block entirely when names is empty).
func renderAvahiHosts(existing string, names []string) string {
	var kept []string
	inBlock := false
	for _, line := range strings.Split(existing, "\n") {
		switch {
		case strings.TrimSpace(line) == avahiManagedStart:
			inBlock = true
		case strings.TrimSpace(line) == avahiManagedEnd:
			inBlock = false
		case !inBlock:
			kept = append(kept, line)
		}
	}
	// Drop trailing blank lines left behind by removing the block.
	for len(kept) > 0 && strings.TrimSpace(kept[len(kept)-1]) == "" {
		kept = kept[:len(kept)-1]
	}

	var b strings.Builder
	for _, line := range kept {
		b.WriteString(line)
		b.WriteString("\n")
	}
	if len(names) > 0 {
		b.WriteString(avahiManagedStart)
		b.WriteString("\n")
		for _, n := range names {
			fmt.Fprintf(&b, "%s %s\n", constants.LocalhostIP, n)
		}
		b.WriteString(avahiManagedEnd)
		b.WriteString("\n")
	}
	return b.String()
}

// reloadAvahi tells Avahi to re-read its static hosts without dropping mDNS.
func reloadAvahi() error {
	if shell.Exists("avahi-daemon") {
		return shell.SudoRun("avahi-daemon", "--reload")
	}
	return shell.SudoSystemctl("reload", "avahi-daemon")
}
