// Copyright 2018 The mkcert Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mkcert

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
)

// nssDBs are the well-known Chrome/Chromium NSS databases; firefoxPaths the
// browser installs whose profiles are scanned for cert9.db/cert8.db.
var (
	nssDBs = []string{
		filepath.Join(os.Getenv("HOME"), ".pki/nssdb"),
		filepath.Join(os.Getenv("HOME"), "snap/chromium/current/.pki/nssdb"), // Snapcraft
		"/etc/pki/nssdb", // CentOS 7
	}
	firefoxPaths = []string{
		"/usr/bin/firefox",
		"/usr/bin/firefox-nightly",
		"/usr/bin/firefox-developer-edition",
		"/snap/firefox",
		"/Applications/Firefox.app",
		"/Applications/FirefoxDeveloperEdition.app",
		"/Applications/Firefox Developer Edition.app",
		"/Applications/Firefox Nightly.app",
		`C:\Program Files\Mozilla Firefox`,
	}
)

var (
	nssAvailableOnce  sync.Once
	nssAvailableState bool
	certutilOnce      sync.Once
	certutilFound     string
)

// nssAvailable reports whether any NSS database or NSS-based browser install
// was found on this host.
func nssAvailable() bool {
	nssAvailableOnce.Do(func() {
		allPaths := append(append([]string{}, nssDBs...), firefoxPaths...)
		nssAvailableState = slices.ContainsFunc(allPaths, pathExists)
	})
	return nssAvailableState
}

// certutilPath locates the NSS certutil binary, deferred to first use so a
// plain `srv start` does no subprocess probing. On macOS it also checks the
// default Homebrew path to save executing Ruby (#135).
func certutilPath() (string, bool) {
	certutilOnce.Do(func() {
		switch runtime.GOOS {
		case "darwin":
			switch {
			case binaryExists("certutil"):
				certutilFound, _ = exec.LookPath("certutil")
			case binaryExists("/usr/local/opt/nss/bin/certutil"):
				certutilFound = "/usr/local/opt/nss/bin/certutil"
			default:
				out, err := exec.Command("brew", "--prefix", "nss").Output() //nolint:noctx // one-shot, cached for the process lifetime
				if err == nil {
					certutilFound = filepath.Join(strings.TrimSpace(string(out)), "bin", "certutil")
				}
			}
		case "linux":
			if binaryExists("certutil") {
				certutilFound, _ = exec.LookPath("certutil")
			}
		}
	})
	return certutilFound, certutilFound != ""
}

func (ca *localCA) checkNSS() bool {
	path, ok := certutilPath()
	if !ok {
		return false
	}
	success := true
	found := ca.forEachNSSProfile(func(profile string) {
		cmd := exec.CommandContext(context.Background(), path, "-V", "-d", profile, "-u", "L", "-n", ca.caUniqueName())
		if err := cmd.Run(); err != nil {
			success = false
		}
	})
	return found > 0 && success
}

func (ca *localCA) installNSS(res *InstallResult) {
	path, ok := certutilPath()
	if !ok {
		res.note("ERROR: certutil disappeared mid-install")
		return
	}
	found := ca.forEachNSSProfile(func(profile string) {
		cmd := exec.CommandContext(context.Background(), path, "-A", "-d", profile, "-t", "C,,", "-n", ca.caUniqueName(), "-i", ca.path(rootName))
		out, err := execCertutil(cmd)
		if err != nil {
			res.note("ERROR: failed to execute %q: %v\n\n%s", "certutil -A -d "+profile, err, out)
		}
	})
	if found == 0 {
		res.note("ERROR: no %s security databases found", nssBrowsers)
		return
	}
	if !ca.checkNSS() {
		res.note("Installing in %s failed. If you never started %s, you need to do that at least once.", nssBrowsers, nssBrowsers)
	}
}

func (ca *localCA) uninstallNSS() error {
	path, ok := certutilPath()
	if !ok {
		return nil
	}
	var firstErr error
	ca.forEachNSSProfile(func(profile string) {
		probe := exec.CommandContext(context.Background(), path, "-V", "-d", profile, "-u", "L", "-n", ca.caUniqueName())
		if err := probe.Run(); err != nil {
			return
		}
		cmd := exec.CommandContext(context.Background(), path, "-D", "-d", profile, "-n", ca.caUniqueName())
		if _, err := execCertutil(cmd); err != nil && firstErr == nil {
			firstErr = err
		}
	})
	return firstErr
}

// execCertutil will execute a "certutil" command and if needed re-execute
// the command with sudo to work around file permissions.
func execCertutil(cmd *exec.Cmd) ([]byte, error) {
	out, err := cmd.CombinedOutput()
	if err != nil && bytes.Contains(out, []byte("SEC_ERROR_READ_ONLY")) && runtime.GOOS != "windows" {
		origArgs := cmd.Args[1:]
		cmd = exec.CommandContext(context.Background(), "sudo", append([]string{"--", cmd.Path}, origArgs...)...)
		out, err = cmd.CombinedOutput()
	}
	return out, err
}

func (ca *localCA) forEachNSSProfile(f func(profile string)) (found int) {
	var profiles []string
	profiles = append(profiles, nssDBs...)
	for _, ff := range firefoxProfiles {
		pp, _ := filepath.Glob(ff)
		profiles = append(profiles, pp...)
	}
	for _, profile := range profiles {
		if stat, err := os.Stat(profile); err != nil || !stat.IsDir() {
			continue
		}
		if pathExists(filepath.Join(profile, "cert9.db")) {
			f("sql:" + profile)
			found++
		} else if pathExists(filepath.Join(profile, "cert8.db")) {
			f("dbm:" + profile)
			found++
		}
	}
	return
}
