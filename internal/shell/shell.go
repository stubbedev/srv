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

// SudoRun executes a command with sudo, stdout/stderr attached.
func SudoRun(args ...string) error {
	return Run("sudo", args...)
}

// SudoRunQuiet executes a command with sudo and returns its output.
func SudoRunQuiet(args ...string) ([]byte, error) {
	return RunQuiet("sudo", args...)
}

// SudoWrite writes content to a file using sudo tee.
func SudoWrite(path, content string) error {
	return RunWithStdin(content, "sudo", "tee", path)
}

// SudoMkdir creates a directory with sudo.
func SudoMkdir(path string) error {
	return SudoRun("mkdir", "-p", path)
}

// SudoRemove removes a file with sudo.
func SudoRemove(path string) error {
	return SudoRun("rm", "-f", path)
}

// SudoSystemctl runs a systemctl command with sudo.
func SudoSystemctl(action, service string) error {
	return SudoRun("systemctl", action, service)
}

// Exists checks if a command exists in PATH.
func Exists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// CheckPort checks if a port is in use.
// Uses net.Listen for reliable cross-platform port checking.
// Falls back to ss/netstat if net.Listen fails due to permissions.
func CheckPort(port string) (bool, error) {
	return CheckPortOnAddr("", port)
}

// CheckPortOnAddr checks whether a specific addr:port is in use.
// addr may be empty to check all interfaces (0.0.0.0), or a specific IP
// such as "127.0.0.1" to check only that binding address.
func CheckPortOnAddr(addr, port string) (bool, error) {
	bindAddr := addr
	if bindAddr == "" {
		bindAddr = "0.0.0.0"
	}

	// First, try direct port binding — most reliable method.
	listener, err := net.Listen("tcp", bindAddr+":"+port)
	if err != nil {
		if isPortInUseError(err) {
			return true, nil
		}
		// Permission denied or other error — fall back to ss/netstat.
	} else {
		_ = listener.Close()
		return false, nil // Port is available
	}

	// Fallback to ss/netstat for privileged ports or when binding fails.
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

	portSuffix := ":" + port
	for line := range strings.SplitSeq(string(output), "\n") {
		fields := strings.FieldsSeq(line)
		for field := range fields {
			if !strings.HasSuffix(field, portSuffix) {
				continue
			}
			// When checking a specific addr, only count it as in-use if the
			// listening address matches. Addresses that bind on a *different*
			// IP (e.g. 127.0.0.53:53) do not block binding on addr:port.
			if addr != "" {
				// field is "ip:port"; extract the IP part.
				host := strings.TrimSuffix(field, portSuffix)
				// Wildcard listeners (0.0.0.0 / ::) conflict with any addr.
				if host != "0.0.0.0" && host != "[::]" && host != addr {
					continue // Different specific IP — not a conflict.
				}
			}
			return true, nil
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
