// Package traefik — resolv.go handles the narrow case where /etc/resolv.conf
// points only at a loopback DNS server (e.g. a previously-installed Valet's
// dnsmasq the user has just stopped). With no working resolver, the docker
// image pulls that follow during `srv install` fail before srv's own dnsmasq
// can take over. EnsureBootstrapResolution detects that situation, sudo-writes
// a temporary resolv.conf pointing at public DNS, and returns a restore
// callback the caller defers to put the original back once srv's containers
// are healthy and 127.0.0.1:53 again resolves.
package traefik

import (
	"fmt"
	"os"
	"strings"

	"github.com/stubbedev/srv/internal/shell"
)

// publicBootstrapResolvConf is the contents we drop in place when the user's
// resolv.conf is unusable. Cloudflare + Google as a belt-and-braces pair.
const publicBootstrapResolvConf = "nameserver 1.1.1.1\nnameserver 8.8.8.8\n"

const resolvConfPath = "/etc/resolv.conf"

// EnsureBootstrapResolution checks /etc/resolv.conf. When every nameserver
// entry points at a loopback address, it stashes the current state and
// sudo-overwrites resolv.conf with public DNS so docker image pulls work.
// The returned restore function puts the original file (or symlink) back.
//
// On macOS the file model is different and the function is a no-op (returns
// (nil, nil)). On Linux without any loopback-only situation it also returns
// (nil, nil).
func EnsureBootstrapResolution() (restore func(), err error) {
	if !needsBootstrapSwap(resolvConfPath) {
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

	if err := shell.Default.SudoWrite(resolvConfPath, publicBootstrapResolvConf); err != nil {
		return nil, fmt.Errorf("sudo-write %s: %w", resolvConfPath, err)
	}

	restore = func() {
		if savedTarget != "" {
			// Replace the regular file we just wrote with the original symlink.
			_ = shell.Default.SudoRemove(resolvConfPath)
			_ = shell.Default.SudoRun("ln", "-sf", savedTarget, resolvConfPath)
			return
		}
		_ = shell.Default.SudoWrite(resolvConfPath, string(savedContents))
	}
	return restore, nil
}

// needsBootstrapSwap is the pure-logic half of EnsureBootstrapResolution: it
// reads the file at path and returns true when every nameserver entry is a
// loopback address. Empty / unreadable / no-nameservers files return false.
func needsBootstrapSwap(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return loopbackOnlyResolvConf(string(data))
}

// loopbackOnlyResolvConf parses resolv.conf-style contents and returns true
// when at least one `nameserver` entry exists and every one is a loopback
// address (127.0.0.0/8 IPv4 or ::1 IPv6).
func loopbackOnlyResolvConf(contents string) bool {
	seen := false
	for _, line := range strings.Split(contents, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if !strings.HasPrefix(t, "nameserver") {
			continue
		}
		fields := strings.Fields(t)
		if len(fields) < 2 {
			continue
		}
		addr := fields[1]
		seen = true
		if !isLoopback(addr) {
			return false
		}
	}
	return seen
}

func isLoopback(addr string) bool {
	if strings.HasPrefix(addr, "127.") {
		return true
	}
	return addr == "::1"
}
