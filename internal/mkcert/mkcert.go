// Package mkcert embeds the mkcert binary and provides a way to run it
// without requiring mkcert to be installed on the host system.
package mkcert

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// ErrUnsupported is returned when the current platform has no embedded binary.
var ErrUnsupported = errors.New("mkcert is not available on this platform")

var (
	extractOnce   sync.Once
	extractedPath string
	extractErr    error
)

// CommandRunner runs the embedded mkcert binary. The signature mirrors the
// three production call shapes (stream / output-only / output+stderr) so a
// test can stub all of them with one struct.
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
// extracted binary via os/exec.
type defaultRunner struct{}

func (defaultRunner) Stream(args ...string) error {
	path, err := extractBinary()
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
	path, err := extractBinary()
	if err != nil {
		return nil, err
	}
	return exec.Command(path, args...).Output()
}

func (defaultRunner) Combined(args ...string) ([]byte, error) {
	path, err := extractBinary()
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

// extractBinary writes the embedded binary to a temp file once and returns
// its path. Subsequent calls return the cached path.
func extractBinary() (string, error) {
	extractOnce.Do(func() {
		if len(binary) == 0 {
			extractErr = ErrUnsupported
			return
		}

		dir := filepath.Join(os.TempDir(), fmt.Sprintf("srv-mkcert-%d", os.Getpid()))
		if err := os.MkdirAll(dir, 0o700); err != nil {
			extractErr = fmt.Errorf("mkcert: failed to create temp dir: %w", err)
			return
		}

		path := filepath.Join(dir, "mkcert")
		if err := os.WriteFile(path, binary, 0o700); err != nil {
			extractErr = fmt.Errorf("mkcert: failed to write binary: %w", err)
			return
		}

		extractedPath = path
	})
	return extractedPath, extractErr
}

// Run executes the embedded mkcert binary with the given arguments.
// stdout and stderr are inherited from the current process.
func Run(args ...string) error {
	return Runner.Stream(args...)
}

// RunQuiet executes the embedded mkcert binary suppressing its stderr.
// mkcert prints advisory warnings to stderr (e.g. "the local CA is not
// installed in the system trust store") that are stale or misleading when
// srv has already handled CA installation. Only a non-zero exit code is
// treated as an error.
func RunQuiet(args ...string) error {
	_, err := Runner.Output(args...)
	return err
}

// Output executes the embedded mkcert binary and returns combined output.
func Output(args ...string) ([]byte, error) {
	return Runner.Output(args...)
}

// Available reports whether the embedded mkcert binary is available on this
// platform.
func Available() bool {
	return len(binary) > 0
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
		}
	}
	return res
}

// caRootDir returns the mkcert CAROOT directory by invoking the embedded
// binary. Falls back to the empty string on error.
func caRootDir() (string, error) {
	out, err := Runner.Output("-CAROOT")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// Cleanup removes the extracted binary from the temp directory.
// Safe to call multiple times; intended for use in process shutdown hooks.
func Cleanup() {
	if extractedPath != "" {
		_ = os.RemoveAll(filepath.Dir(extractedPath))
	}
}
