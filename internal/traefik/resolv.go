// Package traefik — resolv.go handles the narrow case where /etc/resolv.conf
// points only at a loopback DNS server that no longer answers (e.g. a
// previously-installed Valet's dnsmasq the user has just stopped). With no
// working resolver, the docker image pulls that follow during `srv install`
// fail before srv's own dnsmasq can take over. EnsureBootstrapResolution
// detects that situation, sudo-writes a temporary resolv.conf pointing at
// public DNS, and returns a restore callback the caller defers to put the
// original back once srv's containers are healthy and 127.0.0.1:53 again
// resolves.
package traefik

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"net"
	"os"
	"strings"
	"time"

	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/shell"
)

// publicBootstrapResolvConf is the contents we drop in place when the user's
// resolv.conf is unusable. Cloudflare + Google as a belt-and-braces pair.
const publicBootstrapResolvConf = "nameserver 1.1.1.1\nnameserver 8.8.8.8\n"

// resolvConfPath and resolvedStubPath are vars so tests can point them at
// scratch files.
var (
	resolvConfPath   = constants.ResolvConfPath
	resolvedStubPath = constants.SystemdResolvePath
)

// EnsureBootstrapResolution checks /etc/resolv.conf. A loopback-only file
// whose nameserver still answers queries is a working resolver —
// systemd-resolved's stub (127.0.0.53) and a running srv dnsmasq both look
// like that and must be left alone. Only when the loopback nameserver is
// silent does it stash the current state and sudo-replace resolv.conf with
// public DNS so docker image pulls work; the returned restore function puts
// the original file (or symlink) back.
//
// On macOS the file model is different and the function is a no-op (returns
// (nil, nil)). On Linux without a dead loopback-only situation it also
// returns (nil, nil).
func EnsureBootstrapResolution() (restore func(), err error) {
	data, err := os.ReadFile(resolvConfPath)
	if err != nil || !loopbackOnlyResolvConf(string(data)) {
		return nil, nil //nolint:nilerr // unreadable or healthy — caller can do nothing useful here
	}

	if probeResolvConf(data) {
		return nil, nil
	}

	// Capture the original. If it's a symlink we save the target so we can
	// recreate the symlink later; if it's a regular file we save its contents.
	li, lerr := os.Lstat(resolvConfPath)
	if lerr != nil {
		return nil, nil //nolint:nilerr // file gone — caller can do nothing useful here
	}

	var savedTarget string
	var savedContents []byte
	if li.Mode()&os.ModeSymlink != 0 {
		t, terr := os.Readlink(resolvConfPath)
		if terr != nil {
			return nil, fmt.Errorf("read symlink %s: %w", resolvConfPath, terr)
		}
		savedTarget = t
	} else {
		c, rerr := os.ReadFile(resolvConfPath)
		if rerr != nil {
			return nil, fmt.Errorf("read %s: %w", resolvConfPath, rerr)
		}
		savedContents = c
	}

	if err := sudoInstallBootstrapResolvConf(); err != nil {
		return nil, err
	}

	restore = func() {
		// Undo only srv's own write. Once anything else has replaced the
		// bootstrap file — install re-pointing resolv.conf at resolved's
		// stub, say — putting the original back would revert that fix.
		if data, err := os.ReadFile(resolvConfPath); err != nil || !isBootstrapResolvConf(string(data)) {
			return
		}
		if savedTarget != "" {
			// ln -f replaces the bootstrap file we installed with the
			// original symlink.
			_ = sudoSymlink(savedTarget, resolvConfPath)
			return
		}
		_ = shell.Default.SudoWrite(resolvConfPath, string(savedContents))
	}
	return restore, nil
}

// RepairClobberedResolvedStub restarts systemd-resolved when its own stub
// resolv.conf carries srv's bootstrap contents — the damage srv ≤ 0.4.28 did
// by writing the bootstrap through /etc/resolv.conf's symlink. resolved
// rewrites the file from its link configuration on restart; srv cannot
// reconstruct those per-link servers itself. Reports whether it restarted.
func RepairClobberedResolvedStub() bool {
	data, err := os.ReadFile(resolvedStubPath)
	if err != nil || !isBootstrapResolvConf(string(data)) {
		return false
	}
	return shell.Default.SudoSystemctl("restart", "systemd-resolved") == nil
}

// sudoInstallBootstrapResolvConf puts the public-DNS bootstrap in place as a
// regular file. `sudo tee` would follow a symlinked /etc/resolv.conf and
// overwrite the file it points at (systemd-resolved's runtime resolv.conf);
// staging the contents in a temp file and running `sudo install` replaces
// the link itself, leaving its target untouched.
func sudoInstallBootstrapResolvConf() error {
	tmp, err := os.CreateTemp("", "srv-resolv.conf-*")
	if err != nil {
		return fmt.Errorf("stage bootstrap resolv.conf: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(publicBootstrapResolvConf); err != nil {
		tmp.Close()
		return fmt.Errorf("stage bootstrap resolv.conf: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("stage bootstrap resolv.conf: %w", err)
	}
	if err := shell.Default.SudoRun("install", "-m", "0644", tmp.Name(), resolvConfPath); err != nil {
		return fmt.Errorf("sudo-install %s: %w", resolvConfPath, err)
	}
	return nil
}

// probeResolvConf reports whether the first nameserver in resolv.conf
// contents answers a query. Var so tests can pin the verdict without a
// live DNS server on the host.
var probeResolvConf = loopbackResolvConfAnswers

// loopbackResolvConfAnswers resolves a name reserved to never exist (RFC
// 6761 .invalid) through the contents' first nameserver. Any reply —
// including the expected NXDOMAIN — proves a live resolver, so only silence
// (a timeout or a refused connection) reads as dead.
func loopbackResolvConfAnswers(contents []byte) bool {
	ns := firstNameserver(string(contents))
	if ns == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := resolverPinnedTo(ns).LookupHost(ctx, "srv-probe.invalid")
	var dnsErr *net.DNSError
	return err == nil || (errors.As(err, &dnsErr) && dnsErr.IsNotFound)
}

// isBootstrapResolvConf reports whether resolv.conf contents are srv's own
// temporary swap rather than the host's configuration.
func isBootstrapResolvConf(contents string) bool {
	return contents == publicBootstrapResolvConf
}

// sudoSymlink points link at target, replacing whatever link is now (-f),
// and treating an existing symlink as the entry to replace rather than a
// directory to descend into (-n).
func sudoSymlink(target, link string) error {
	return shell.Default.SudoRun("ln", "-sfn", target, link)
}

// loopbackOnlyResolvConf parses resolv.conf-style contents and returns true
// when at least one `nameserver` entry exists and every one is a loopback
// address (127.0.0.0/8 IPv4 or ::1 IPv6).
func loopbackOnlyResolvConf(contents string) bool {
	seen := false
	for addr := range nameservers(contents) {
		seen = true
		if !isLoopback(addr) {
			return false
		}
	}
	return seen
}

// firstNameserver returns the nameserver resolv.conf contents send lookups
// to first, or "" when none is listed.
func firstNameserver(contents string) string {
	for addr := range nameservers(contents) {
		return addr
	}
	return ""
}

// nameservers yields every `nameserver` address in resolv.conf contents, in
// file order. It slices the input rather than splitting fields, so walking a
// file allocates nothing.
func nameservers(contents string) iter.Seq[string] {
	return func(yield func(string) bool) {
		for line := range strings.SplitSeq(contents, "\n") {
			rest, ok := strings.CutPrefix(strings.TrimSpace(line), "nameserver")
			if !ok || rest == "" || (rest[0] != ' ' && rest[0] != '\t') {
				continue
			}
			addr := strings.TrimSpace(rest)
			if i := strings.IndexAny(addr, " \t"); i >= 0 {
				addr = addr[:i]
			}
			if addr != "" && !yield(addr) {
				return
			}
		}
	}
}

func isLoopback(addr string) bool {
	if strings.HasPrefix(addr, "127.") {
		return true
	}
	return addr == "::1"
}
