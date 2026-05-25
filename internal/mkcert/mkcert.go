// Package mkcert shells out to the system `mkcert` binary. Users must have
// it on $PATH (`brew install mkcert`, `nix profile install nixpkgs#mkcert`,
// distro package manager) — srv no longer vendors / embeds it.
package mkcert

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrNotInstalled is returned when `mkcert` is not on $PATH.
var ErrNotInstalled = errors.New("mkcert not installed (`brew install mkcert` / `nix profile install nixpkgs#mkcert` / distro package)")

// CommandRunner runs the mkcert binary. The signature mirrors the three
// production call shapes (stream / output-only / output+stderr) so a test
// can stub all of them with one struct.
type CommandRunner interface {
	// Stream runs mkcert with stdin/stdout/stderr attached.
	Stream(args ...string) error
	// Output runs mkcert and returns stdout. Stderr is ignored.
	Output(args ...string) ([]byte, error)
	// Combined runs mkcert and returns the merged stdout+stderr along with
	// the run error.
	Combined(args ...string) ([]byte, error)
}

// defaultRunner is the production CommandRunner; it shells out to the
// system `mkcert` binary via os/exec.
type defaultRunner struct{}

func (defaultRunner) Stream(args ...string) error {
	path, err := mkcertPath()
	if err != nil {
		return err
	}
	cmd := exec.Command(path, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func (defaultRunner) Output(args ...string) ([]byte, error) {
	path, err := mkcertPath()
	if err != nil {
		return nil, err
	}
	return exec.Command(path, args...).Output()
}

func (defaultRunner) Combined(args ...string) ([]byte, error) {
	path, err := mkcertPath()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(path, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	runErr := cmd.Run()
	return buf.Bytes(), runErr
}

// Runner is the active CommandRunner. Tests can replace this via SwapRunner.
var Runner CommandRunner = defaultRunner{}

// SwapRunner installs r and returns a function that restores the previous
// runner. Intended for use with t.Cleanup.
func SwapRunner(r CommandRunner) func() {
	prev := Runner
	Runner = r
	return func() { Runner = prev }
}

// mkcertPath returns the resolved $PATH location of the mkcert binary, or
// ErrNotInstalled if it isn't on the host.
func mkcertPath() (string, error) {
	path, err := lookPath("mkcert")
	if err != nil {
		return "", ErrNotInstalled
	}
	return path, nil
}

// Run executes mkcert with the given arguments. stdout/stderr are inherited.
func Run(args ...string) error {
	return Runner.Stream(args...)
}

// RunQuiet executes mkcert suppressing its stderr. mkcert prints advisory
// warnings to stderr (e.g. "the local CA is not installed in the system
// trust store") that are stale or misleading when srv has already handled
// CA installation. Only a non-zero exit code is treated as an error.
func RunQuiet(args ...string) error {
	_, err := Runner.Output(args...)
	return err
}

// Output executes mkcert and returns its stdout.
func Output(args ...string) ([]byte, error) {
	return Runner.Output(args...)
}

// lookPath is the exec.LookPath indirection so tests can fake the host's PATH.
var lookPath = exec.LookPath

// SwapLookPath replaces the lookPath used by Available + mkcertPath. Returns a
// restore func; tests use it via t.Cleanup.
func SwapLookPath(fn func(string) (string, error)) func() {
	prev := lookPath
	lookPath = fn
	return func() { lookPath = prev }
}

// Available reports whether mkcert is on $PATH.
func Available() bool {
	_, err := lookPath("mkcert")
	return err == nil
}

// InstallResult describes the outcome of running `mkcert -install`. mkcert
// prints multiple status lines covering the local CA, system trust store, and
// NSS (Firefox/Chrome) database. We parse these so the caller can render a
// single clean message instead of leaking mkcert's raw output.
type InstallResult struct {
	CARootPath         string // Path to rootCA.pem (only set when known)
	SystemTrustOK      bool   // CA installed in OS trust store
	BrowserTrustOK     bool   // CA installed in NSS DB (Firefox/Chrome)
	SystemUnsupported  bool   // System trust store install not supported on this platform
	BrowserUnavailable bool   // Firefox/Chrome NSS support unavailable (no profiles or no certutil)
	CertutilMissing    bool   // certutil binary not found, needed for browser trust
	NewCA              bool   // A fresh local CA was created during this run
	SudoDenied         bool   // sudo password prompt failed or was refused; CA install aborted
	RawOutput          string // Captured combined output (for debugging)
}

// Install runs `mkcert -install` and parses its output into an InstallResult.
// stdout/stderr are captured rather than streamed to the user.
func Install() (InstallResult, error) {
	out, runErr := Runner.Combined("-install")
	res := parseInstallOutput(string(out))
	if caRoot, cerr := caRootDir(); cerr == nil {
		res.CARootPath = filepath.Join(caRoot, "rootCA.pem")
	}
	return res, runErr
}

// parseInstallOutput is the pure-logic half of Install — given the combined
// stdout/stderr of `mkcert -install`, it returns the populated result struct
// (without the CARootPath, which requires another mkcert invocation).
func parseInstallOutput(out string) InstallResult {
	res := InstallResult{RawOutput: out}
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.Contains(line, "Created a new local CA"):
			res.NewCA = true
		case strings.Contains(line, "now installed in the system trust store"):
			res.SystemTrustOK = true
		case strings.Contains(line, "Installing to the system store is not yet supported"):
			res.SystemUnsupported = true
		case strings.Contains(line, "support is not available on your platform"):
			res.BrowserUnavailable = true
		case strings.Contains(line, "now installed in the Firefox") ||
			strings.Contains(line, "now installed in the Chrome") ||
			(strings.Contains(line, "trust store") && strings.Contains(line, "browser restart")):
			res.BrowserTrustOK = true
		case strings.Contains(line, "no \"certutil\" tool") ||
			strings.Contains(line, "warning: \"certutil\" is not available"):
			res.CertutilMissing = true
		case strings.Contains(line, "Authentication failed") ||
			strings.Contains(line, "incorrect authentication attempts") ||
			strings.Contains(line, "sudo: a password is required") ||
			strings.Contains(line, "sudo-rs:"):
			res.SudoDenied = true
		}
	}
	return res
}

// caRootDir returns the mkcert CAROOT directory by invoking the binary.
// Falls back to the empty string on error.
func caRootDir() (string, error) {
	out, err := Runner.Output("-CAROOT")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

