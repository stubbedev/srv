// Package traefik — resolved.go checks that a systemd-resolved host actually
// sends its lookups through resolved. srv's routing on such hosts is a
// resolved drop-in (`~domain` → srv's DNS server), which only takes effect for
// clients that ask resolved. glibc's dns module, Go, Chrome's built-in
// resolver and containers all read /etc/resolv.conf and ignore nsswitch, so
// the one thing that decides it is where that file sends them: resolved's
// stub (127.0.0.53), or — in resolved's "uplink" mode — straight to the
// upstream servers, where srv's domains resolve to the real internet.
package traefik

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/platform"
)

// Vars so tests can point them at scratch files.
var (
	nsswitchPath    = constants.NsswitchPath
	nixosMarkerPath = constants.NixOSMarkerPath
)

// ResolvedBypassError reports that srv's systemd-resolved routing is in place
// but /etc/resolv.conf sends lookups past resolved, so it has no effect.
type ResolvedBypassError struct {
	// Nameserver is where /etc/resolv.conf sends lookups first ("" if none).
	Nameserver string
	// Target is the /etc/resolv.conf symlink target ("" for a regular file).
	Target string
	// Fixable reports that srv may re-point /etc/resolv.conf itself: it links
	// to resolved's own uplink file, so resolved already owns it and only the
	// mode changes. Anything else belongs to another manager (or is NixOS's
	// generated /etc), and rewriting it would fight that manager.
	Fixable bool
	// NixOS reports a declarative /etc, where the fix is configuration.nix.
	NixOS bool
}

func (e *ResolvedBypassError) Error() string {
	return e.Summary() + "; fix: " + e.Fix()
}

// Summary says what is wrong, without the fix.
func (e *ResolvedBypassError) Summary() string {
	where := "is a regular file"
	if e.Target != "" {
		where = "links to " + e.Target
	}
	ns := e.Nameserver
	if ns == "" {
		ns = "no nameserver"
	}
	return fmt.Sprintf("/etc/resolv.conf %s and sends lookups to %s, bypassing systemd-resolved, so srv's DNS routing has no effect", where, ns)
}

// Fix returns the remedy to show the user.
func (e *ResolvedBypassError) Fix() string {
	if e.NixOS {
		return "set services.resolved.enable = true in configuration.nix and rebuild (it points /etc/resolv.conf at resolved's stub)"
	}
	return "sudo " + strings.Join(repointArgs(), " ")
}

// repointArgs is the command that points /etc/resolv.conf at resolved's stub.
func repointArgs() []string {
	return []string{"ln", "-sfn", constants.SystemdResolvePath, resolvConfPath}
}

// RepointResolvConf links /etc/resolv.conf to systemd-resolved's stub, so
// every client asks resolved and resolved's per-domain routing applies. The
// upstream servers do not change: resolved keeps forwarding to each link's
// servers exactly as the uplink file listed them. Callers only do this for a
// Fixable bypass, with the user's consent.
func RepointResolvConf() error {
	if err := sudoSymlink(constants.SystemdResolvePath, resolvConfPath); err != nil {
		return fmt.Errorf("re-point %s at %s: %w", resolvConfPath, constants.SystemdResolvePath, err)
	}
	return nil
}

// resolvedBypass reports how /etc/resolv.conf skips systemd-resolved, or nil
// when lookups reach it. data is the file's current contents (nil when it
// could not be read); callers pass what they already read.
//
// While srv's own bootstrap swap is in place the file says nothing about the
// host, so there is no verdict: install re-checks after restoring it.
func resolvedBypass(data []byte) *ResolvedBypassError {
	contents := string(data)
	if isBootstrapResolvConf(contents) {
		return nil
	}
	ns := firstNameserver(contents)
	if ns == constants.SystemdResolvedStubIP {
		return nil
	}
	e := &ResolvedBypassError{Nameserver: ns}
	e.Target, _ = os.Readlink(resolvConfPath)
	if _, err := os.Stat(nixosMarkerPath); err == nil {
		e.NixOS = true
	}
	e.Fixable = !e.NixOS && filepath.Clean(e.Target) == constants.SystemdResolvedUplinkPath
	return e
}

// checkResolvedPath returns a *ResolvedBypassError when resolved is the
// detected resolver but /etc/resolv.conf routes past it.
func checkResolvedPath() error {
	data, _ := os.ReadFile(resolvConfPath)
	if b := resolvedBypass(data); b != nil {
		return b
	}
	return nil
}

// SystemResolution is how the host's resolver treats srv's domains.
type SystemResolution struct {
	// Unresolved lists the domains (bare) the system resolver path does not
	// answer with 127.0.0.1.
	Unresolved []string
	// Bypass is non-nil when systemd-resolved runs but /etc/resolv.conf
	// sends lookups past it: the cause of every Unresolved non-mDNS name.
	Bypass *ResolvedBypassError
	// MDNSLocal lists registered .local domains glibc hands to mDNS and
	// never to DNS: nsswitch puts an mdns module with [NOTFOUND=return]
	// ahead of dns/resolve. Go and Chrome's own resolver still reach srv, so
	// these can look fine in a probe and still fail in curl or Firefox.
	MDNSLocal []string
}

// OK reports whether every domain resolves through the system path.
func (r SystemResolution) OK() bool {
	return len(r.Unresolved) == 0 && r.Bypass == nil && len(r.MDNSLocal) == 0
}

// DiagnoseSystemResolution probes every domain through the path clients use
// and explains failures. Each config file is read once for the whole set,
// and the probes run concurrently (each is bounded by a 2s timeout, and a
// broken setup is exactly when a serial loop over many domains would stall).
func DiagnoseSystemResolution(domains []string) SystemResolution {
	var r SystemResolution
	resolv, _ := os.ReadFile(resolvConfPath)
	if DetectResolver() == ResolverSystemdResolved {
		r.Bypass = resolvedBypass(resolv)
	}
	mdns := nsswitchMDNSInterceptsLocal()

	resolver := systemResolver(resolv)
	ok := make([]bool, len(domains))
	const maxWorkers = 8
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range min(maxWorkers, len(domains)) {
		wg.Go(func() {
			for i := range jobs {
				ok[i] = resolvesToLoopback(resolver, BareDomain(domains[i]))
			}
		})
	}
	for i := range domains {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	for i, d := range domains {
		bare := BareDomain(d)
		if !ok[i] {
			r.Unresolved = append(r.Unresolved, bare)
		}
		if mdns && strings.HasSuffix(bare, ".local") {
			r.MDNSLocal = append(r.MDNSLocal, bare)
		}
	}
	return r
}

// systemResolver returns the resolver that follows the system's path for a
// probe. On Linux that is resolv.conf's first nameserver, taken from the
// contents just read: Go's own resolver re-reads the file at most every five
// seconds, which would hide a re-point made moments earlier. Elsewhere it is
// the platform resolver — macOS routes per domain through /etc/resolver,
// which only the system API honours.
func systemResolver(resolv []byte) *net.Resolver {
	ns := firstNameserver(string(resolv))
	if !platform.IsLinux() || ns == "" {
		return net.DefaultResolver
	}
	addr := net.JoinHostPort(ns, "53")
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}
}

func resolvesToLoopback(r *net.Resolver, domain string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	addrs, err := r.LookupHost(ctx, domain)
	return err == nil && slices.Contains(addrs, constants.LocalhostIP)
}

// nsswitchMDNSInterceptsLocal reports whether glibc answers .local from mDNS
// alone: an mdns module followed by [NOTFOUND=return] ahead of the first dns
// or resolve source, so an unknown .local name stops there.
func nsswitchMDNSInterceptsLocal() bool {
	data, err := os.ReadFile(nsswitchPath)
	if err != nil {
		return false
	}
	return mdnsInterceptsLocal(string(data))
}

// nsswitchHosts returns the sources and actions of the hosts: line.
func nsswitchHosts(contents string) (string, bool) {
	for line := range strings.SplitSeq(contents, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "hosts:"); ok {
			if i := strings.IndexByte(rest, '#'); i >= 0 {
				rest = rest[:i]
			}
			return rest, true
		}
	}
	return "", false
}

func mdnsInterceptsLocal(contents string) bool {
	hosts, ok := nsswitchHosts(contents)
	if !ok {
		return false
	}
	mdns := false
	for tok := range strings.FieldsSeq(hosts) {
		switch {
		case tok == "dns" || tok == "resolve":
			return false
		case strings.HasPrefix(tok, "mdns"):
			mdns = true
		case strings.HasPrefix(tok, "["):
			// An action applies to the source just before it.
			if mdns && strings.Contains(strings.ToUpper(tok), "NOTFOUND=RETURN") {
				return true
			}
		default:
			mdns = false
		}
	}
	return false
}
