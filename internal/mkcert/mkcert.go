// Package mkcert embeds the mkcert binary and provides a way to run it
// without requiring mkcert to be installed on the host system.
package mkcert

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

// ErrUnsupported is returned when the current platform has no embedded binary.
var ErrUnsupported = errors.New("mkcert is not available on this platform")

var (
	extractOnce   sync.Once
	extractedPath string
	extractErr    error
)

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

// RunQuiet executes the embedded mkcert binary suppressing its stderr.
// mkcert prints advisory warnings to stderr (e.g. "the local CA is not
// installed in the system trust store") that are stale or misleading when
// srv has already handled CA installation. Only a non-zero exit code is
// treated as an error.
func RunQuiet(args ...string) error {
	path, err := extractBinary()
	if err != nil {
		return err
	}
	out, err := exec.Command(path, args...).Output()
	_ = out // stdout intentionally discarded for cert generation
	return err
}

// Output executes the embedded mkcert binary and returns combined output.
func Output(args ...string) ([]byte, error) {
	path, err := extractBinary()
	if err != nil {
		return nil, err
	}
	return exec.Command(path, args...).Output()
}

// Available reports whether the embedded mkcert binary is available on this
// platform.
func Available() bool {
	return len(binary) > 0
}

// Cleanup removes the extracted binary from the temp directory.
// Safe to call multiple times; intended for use in process shutdown hooks.
func Cleanup() {
	if extractedPath != "" {
		os.RemoveAll(filepath.Dir(extractedPath))
	}
}
