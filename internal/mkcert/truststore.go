// Copyright 2018 The mkcert Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mkcert

import (
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"slices"
	"strings"

	"github.com/stubbedev/srv/internal/shell"
)

// errSudoDenied reports that a privileged command failed because sudo could
// not authenticate (no TTY, refused or wrong password).
var errSudoDenied = errors.New("sudo authentication failed")

// note appends a line to the result's human-readable log, keeping the
// debugging role RawOutput played when srv captured mkcert's console output.
func (res *InstallResult) note(format string, args ...any) {
	res.RawOutput += fmt.Sprintf(format+"\n", args...)
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func binaryExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// install enrolls the local CA into the system and NSS trust stores,
// translating what upstream printed to the console into structured fields.
func (ca *localCA) install() (InstallResult, error) {
	res := InstallResult{CARootPath: ca.path(rootName)}
	if ca.created {
		res.NewCA = true
		res.note("Created a new local CA")
	}

	if storeEnabled("system") {
		if ca.checkPlatform() {
			res.SystemTrustOK = true
			res.note("The local CA is already installed in the system trust store")
		} else {
			installed, err := ca.installPlatform(&res)
			if err != nil {
				res.SudoDenied = errors.Is(err, errSudoDenied)
				return res, err
			}
			res.SystemTrustOK = installed
			if installed {
				res.note("The local CA is now installed in the system trust store")
			} else {
				res.SystemUnsupported = true
			}
		}
	}

	if storeEnabled("nss") && nssAvailable() {
		if ca.checkNSS() {
			res.BrowserTrustOK = true
			res.note("The local CA is already installed in the %s trust store", nssBrowsers)
		} else if _, ok := certutilPath(); !ok {
			if certutilInstallHelp == "" {
				res.BrowserUnavailable = true
				res.note("%s support is not available on your platform", nssBrowsers)
			} else {
				res.CertutilMissing = true
				res.note("%q is not available, so the CA can't be automatically installed in %s", "certutil", nssBrowsers)
				res.note("Install %q with %q and re-run `srv install`", "certutil", certutilInstallHelp)
			}
		} else {
			ca.installNSS(&res)
			if ca.checkNSS() {
				res.BrowserTrustOK = true
				res.note("The local CA is now installed in the %s trust store (requires browser restart)", nssBrowsers)
			}
		}
	}

	return res, nil
}

// uninstall removes the local CA from the trust stores.
func (ca *localCA) uninstall() error {
	if storeEnabled("nss") && nssAvailable() {
		if _, ok := certutilPath(); ok {
			if err := ca.uninstallNSS(); err != nil {
				return err
			}
		}
	}
	if storeEnabled("system") {
		if _, err := ca.uninstallPlatform(); err != nil {
			return err
		}
	}
	return nil
}

// checkPlatform reports whether the CA already verifies against the system
// trust pool.
func (ca *localCA) checkPlatform() bool {
	_, err := ca.caCert.Verify(x509.VerifyOptions{})
	return err == nil
}

// storeEnabled honors upstream's $TRUST_STORES filter ("system", "nss",
// "java"); by default every store the platform supports is attempted.
func storeEnabled(name string) bool {
	stores := os.Getenv("TRUST_STORES")
	if stores == "" {
		return true
	}
	for store := range strings.SplitSeq(stores, ",") {
		if store == name {
			return true
		}
	}
	return false
}

// privileged runs name as root, capturing combined output. It skips sudo when
// already running as root or when sudo is unavailable, mirrors srv's
// non-interactive mode (surfaces without a TTY must fail fast, not hang on a
// password prompt), and classifies authentication failures as errSudoDenied.
// Failing command output is embedded in the returned error.
func privileged(stdin io.Reader, name string, args ...string) error {
	if !runningAsRoot() && binaryExists("sudo") {
		sudoArgs := []string{"--prompt=Sudo password:", "--"}
		if shell.IsNonInteractive() {
			sudoArgs = append(sudoArgs, "-n")
		}
		command := make([]string, 0, len(sudoArgs)+len(args)+1)
		command = append(command, sudoArgs...)
		command = append(command, name)
		command = append(command, args...)
		args, name = command, "sudo"
	}

	cmd := exec.CommandContext(context.Background(), name, args...)
	if stdin != nil {
		cmd.Stdin = stdin
	}
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	if isSudoDenied(out) {
		err = errSudoDenied
	}
	return fmt.Errorf("failed to execute %q: %w\n\n%s", commandString(name, args), err, out)
}

func commandString(name string, args []string) string {
	return strings.Join(append([]string{name}, args...), " ")
}

func runningAsRoot() bool {
	u, err := user.Current()
	return err == nil && u.Uid == "0"
}

// isSudoDenied matches the sudo error strings the previous exec-based
// integration classified, covering sudo and sudo-rs.
func isSudoDenied(out []byte) bool {
	markers := []string{
		"Authentication failed",
		"incorrect authentication attempts",
		"a password is required",
		"sudo-rs:",
	}
	return slices.ContainsFunc(markers, func(marker string) bool {
		return bytes.Contains(out, []byte(marker))
	})
}
