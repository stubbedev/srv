// Copyright 2018 The mkcert Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mkcert

import (
	"bytes"
	"fmt"
	"os"
	"strings"
)

var (
	firefoxProfiles = []string{
		os.Getenv("HOME") + "/.mozilla/firefox/*",
		os.Getenv("HOME") + "/snap/firefox/common/.mozilla/firefox/*",
	}
	nssBrowsers = "Firefox and/or Chrome/Chromium"

	certutilInstallHelp = detectCertutilInstallHelp()
)

// systemTrustCommandFn indirection exists so tests can force the
// "unsupported store" path regardless of the host filesystem.
var systemTrustCommandFn = systemTrustCommand

// SwapSystemTrustCommand replaces the store detector and returns a restore
// func. Intended for tests.
func SwapSystemTrustCommand(fn func() (string, []string)) func() {
	prev := systemTrustCommandFn
	systemTrustCommandFn = fn
	return func() { systemTrustCommandFn = prev }
}

// detectCertutilInstallHelp returns the canonical certutil install command
// for the host distro, or "" when none can be identified.
func detectCertutilInstallHelp() string {
	switch {
	case binaryExists("apt"):
		return "apt install libnss3-tools"
	case binaryExists("yum"):
		return "yum install nss-tools"
	case binaryExists("zypper"):
		return "zypper install mozilla-nss-tools"
	}
	return ""
}

// systemTrustCommand returns the anchor filename format (an fmt template for
// the sanitized CA name) and the command that rebuilds the system trust
// store, or ("", nil) when this Linux has no supported store.
func systemTrustCommand() (string, []string) {
	switch {
	case pathExists("/etc/pki/ca-trust/source/anchors/"):
		return "/etc/pki/ca-trust/source/anchors/%s.pem", []string{"update-ca-trust", "extract"}
	case pathExists("/usr/local/share/ca-certificates/"):
		return "/usr/local/share/ca-certificates/%s.crt", []string{"update-ca-certificates"}
	case pathExists("/etc/ca-certificates/trust-source/anchors/"):
		return "/etc/ca-certificates/trust-source/anchors/%s.crt", []string{"trust", "extract-compat"}
	case pathExists("/usr/share/pki/trust/anchors"):
		return "/usr/share/pki/trust/anchors/%s.pem", []string{"update-ca-certificates"}
	}
	return "", nil
}

func (ca *localCA) systemTrustFilename(format string) string {
	return fmt.Sprintf(format, strings.ReplaceAll(ca.caUniqueName(), " ", "_"))
}

func (ca *localCA) installPlatform(res *InstallResult) (bool, error) {
	format, command := systemTrustCommandFn()
	if command == nil {
		res.note("Installing to the system store is not yet supported on this Linux, but %s will still work.", nssBrowsers)
		res.note("You can also manually install the root certificate at %q.", ca.path(rootName))
		return false, nil
	}

	cert, err := os.ReadFile(ca.path(rootName))
	if err != nil {
		return false, fmt.Errorf("failed to read root certificate: %w", err)
	}

	if err := privileged(bytes.NewReader(cert), "tee", ca.systemTrustFilename(format)); err != nil {
		return false, err
	}
	if err := privileged(nil, command[0], command[1:]...); err != nil {
		return false, err
	}
	return true, nil
}

func (ca *localCA) uninstallPlatform() (bool, error) {
	format, command := systemTrustCommandFn()
	if command == nil {
		return false, nil
	}

	if err := privileged(nil, "rm", "-f", ca.systemTrustFilename(format)); err != nil {
		return false, err
	}

	// We used to install under non-unique filenames.
	legacyFilename := fmt.Sprintf(format, "mkcert-rootCA")
	if pathExists(legacyFilename) {
		if err := privileged(nil, "rm", "-f", legacyFilename); err != nil {
			return false, err
		}
	}

	if err := privileged(nil, command[0], command[1:]...); err != nil {
		return false, err
	}
	return true, nil
}
