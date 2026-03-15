// Package shell provides helpers for executing shell commands.
package shell

import (
	"context"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Default timeouts for shell commands.
const (
	// DefaultTimeout is the default timeout for shell commands.
	DefaultTimeout = 30 * time.Second
	// LongTimeout is the timeout for longer operations like mkcert.
	LongTimeout = 2 * time.Minute
)

// Run executes a command with stdout/stderr attached.
func Run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// RunWithContext executes a command with a context for timeout/cancellation.
func RunWithContext(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// RunQuiet executes a command and returns its output.
func RunQuiet(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}

// RunQuietWithContext executes a command with context and returns its output.
func RunQuietWithContext(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

// RunWithStdin executes a command with the given stdin content.
func RunWithStdin(stdin string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Sudo executes a command with sudo, stdout/stderr attached.
func Sudo(args ...string) error {
	return Run("sudo", args...)
}

// SudoWrite writes content to a file using sudo tee.
func SudoWrite(path, content string) error {
	return RunWithStdin(content, "sudo", "tee", path)
}

// SudoMkdir creates a directory with sudo.
func SudoMkdir(path string) error {
	return Sudo("mkdir", "-p", path)
}

// SudoRemove removes a file with sudo.
func SudoRemove(path string) error {
	return Sudo("rm", "-f", path)
}

// SudoSystemctl runs a systemctl command with sudo.
func SudoSystemctl(action, service string) error {
	return Sudo("systemctl", action, service)
}

// Exists checks if a command exists in PATH.
func Exists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// Mkcert runs mkcert with the given arguments.
func Mkcert(args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), LongTimeout)
	defer cancel()
	return RunWithContext(ctx, "mkcert", args...)
}

// MkcertQuiet runs mkcert and returns its output.
func MkcertQuiet(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), LongTimeout)
	defer cancel()
	return RunQuietWithContext(ctx, "mkcert", args...)
}

// CheckPort checks if a port is in use.
// Uses net.Listen for reliable cross-platform port checking.
// Falls back to ss/netstat if net.Listen fails due to permissions.
func CheckPort(port string) (bool, error) {
	// First, try direct port binding - most reliable method
	listener, err := net.Listen("tcp", ":"+port)
	if err != nil {
		// If we get an error, check if it's because the port is in use
		// or if it's a permission error (ports < 1024 require root)
		if isPortInUseError(err) {
			return true, nil
		}
		// Permission denied or other error - fall back to ss/netstat
	} else {
		listener.Close()
		return false, nil // Port is available
	}

	// Fallback to ss/netstat for privileged ports or when binding fails
	ctx, cancel := context.WithTimeout(context.Background(), DefaultTimeout)
	defer cancel()

	var output []byte

	if Exists("ss") {
		output, err = RunQuietWithContext(ctx, "ss", "-tuln")
	} else if Exists("netstat") {
		output, err = RunQuietWithContext(ctx, "netstat", "-tuln")
	} else {
		return false, nil // Can't check, assume available
	}

	if err != nil {
		return false, err
	}

	// Parse output line by line for more accurate matching
	// Look for patterns like ":80 " or ":80\t" or "]:80 " (IPv6)
	lines := strings.Split(string(output), "\n")
	portSuffix := ":" + port
	for _, line := range lines {
		// Check for port at end of address (before whitespace)
		// Handles both IPv4 (0.0.0.0:80) and IPv6 ([::]:80)
		fields := strings.FieldsSeq(line)
		for field := range fields {
			if strings.HasSuffix(field, portSuffix) {
				return true, nil
			}
		}
	}

	return false, nil
}

// isPortInUseError checks if the error indicates the port is already in use.
func isPortInUseError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "address already in use") ||
		strings.Contains(errStr, "bind: address already in use")
}

// IdentifyPortProcess returns the name of the process listening on the given
// port, or an empty string if it cannot be determined.
// It tries ss(8) first (Linux), then lsof(8) (macOS/Linux fallback).
// Special case: systemd-resolved is detected by its well-known stub listener
// addresses (127.0.0.53, 127.0.0.54) even when running without sudo, since ss
// hides process names for other users' processes without elevated privileges.
func IdentifyPortProcess(port string) string {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultTimeout)
	defer cancel()

	// ss -tlnp gives lines like:
	//   LISTEN 0 128 0.0.0.0:80 ... users:(("nginx",pid=1234,fd=6))
	if Exists("ss") {
		out, err := RunQuietWithContext(ctx, "ss", "-tlnp")
		if err == nil {
			portSuffix := ":" + port
			for line := range strings.SplitSeq(string(out), "\n") {
				fields := strings.Fields(line)
				// Check if this line is for our port (field index 3 is local address)
				if len(fields) < 4 {
					continue
				}
				if !strings.HasSuffix(fields[3], portSuffix) {
					continue
				}
				// Extract process name from users:(("name",...)) when visible.
				for _, f := range fields {
					if strings.HasPrefix(f, "users:") {
						name := extractProcessName(f)
						if name != "" {
							return name
						}
					}
				}
				// Process name not visible (no sudo). Detect systemd-resolved by
				// its stub listener addresses — it only ever binds on 127.0.0.53
				// and 127.0.0.54, never on 0.0.0.0 or ::.
				addr := fields[3]
				if strings.HasPrefix(addr, "127.0.0.53:") || strings.HasPrefix(addr, "127.0.0.54:") {
					return "systemd-resolved"
				}
			}
		}
	}

	// lsof -i :<port> -sTCP:LISTEN -n -P -F p c
	// Produces lines like: cnginx\n or capache2\n
	if Exists("lsof") {
		out, err := RunQuietWithContext(ctx, "lsof", "-i", ":"+port, "-sTCP:LISTEN", "-n", "-P", "-F", "c")
		if err == nil {
			for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
				// lsof -F c lines are prefixed with 'c'
				if after, ok := strings.CutPrefix(line, "c"); ok {
					name := after
					if name != "" {
						return name
					}
				}
			}
		}
	}

	return ""
}

// extractProcessName parses the process name out of an ss users field.
// Input looks like: users:(("nginx",pid=1234,fd=6))
func extractProcessName(field string) string {
	// Find the opening quote after users:((\"
	_, after, ok := strings.Cut(field, `"`)
	if !ok {
		return ""
	}
	rest := after
	before, _, ok := strings.Cut(rest, `"`)
	if !ok {
		return ""
	}
	return before
}
