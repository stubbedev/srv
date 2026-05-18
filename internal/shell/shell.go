// Package shell provides helpers for executing shell commands.
//
// The package exposes free functions for the common cases (Run, RunQuiet, …)
// that delegate to a swappable Runner. Production code uses the OS-backed
// Default runner; tests inject fakes via SwapDefault.
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

// Runner is the abstraction over the OS shell that srv uses.
// Tests can implement this interface (or use shelltest.Fake) and inject it
// via SwapDefault to assert on command invocations and stub return values.
type Runner interface {
	Run(name string, args ...string) error
	RunWithContext(ctx context.Context, name string, args ...string) error
	RunQuiet(name string, args ...string) ([]byte, error)
	RunQuietWithContext(ctx context.Context, name string, args ...string) ([]byte, error)
	RunWithStdin(stdin string, name string, args ...string) error
	SudoRun(args ...string) error
	SudoRunQuiet(args ...string) ([]byte, error)
	SudoWrite(path, content string) error
	SudoMkdir(path string) error
	SudoRemove(path string) error
	SudoSystemctl(action, service string) error
	Exists(name string) bool
	CheckPort(port string) (bool, error)
	CheckPortOnAddr(addr, port string) (bool, error)
	IdentifyPortProcess(port string) string
}

// Default is the runner used by every package-level helper. Tests swap this
// via SwapDefault. Direct assignment is permitted but SwapDefault is preferred
// because it returns a restore func.
var Default Runner = OSRunner{}

// SwapDefault replaces Default with r and returns a function that restores
// the previous value. Intended for use with t.Cleanup in tests:
//
//	t.Cleanup(shell.SwapDefault(fake))
func SwapDefault(r Runner) func() {
	prev := Default
	Default = r
	return func() { Default = prev }
}

// ---- package-level conveniences (production callers) -----------------------

// Run executes a command with stdout/stderr attached.
func Run(name string, args ...string) error { return Default.Run(name, args...) }

// RunWithContext executes a command with a context for timeout/cancellation.
func RunWithContext(ctx context.Context, name string, args ...string) error {
	return Default.RunWithContext(ctx, name, args...)
}

// RunQuiet executes a command and returns its output.
func RunQuiet(name string, args ...string) ([]byte, error) {
	return Default.RunQuiet(name, args...)
}

// RunQuietWithContext executes a command with context and returns its output.
func RunQuietWithContext(ctx context.Context, name string, args ...string) ([]byte, error) {
	return Default.RunQuietWithContext(ctx, name, args...)
}

// RunWithStdin executes a command with the given stdin content.
func RunWithStdin(stdin string, name string, args ...string) error {
	return Default.RunWithStdin(stdin, name, args...)
}

// SudoRun executes a command with sudo, stdout/stderr attached.
func SudoRun(args ...string) error { return Default.SudoRun(args...) }

// SudoRunQuiet executes a command with sudo and returns its output.
func SudoRunQuiet(args ...string) ([]byte, error) { return Default.SudoRunQuiet(args...) }

// SudoWrite writes content to a file using sudo tee.
func SudoWrite(path, content string) error { return Default.SudoWrite(path, content) }

// SudoMkdir creates a directory with sudo.
func SudoMkdir(path string) error { return Default.SudoMkdir(path) }

// SudoRemove removes a file with sudo.
func SudoRemove(path string) error { return Default.SudoRemove(path) }

// SudoSystemctl runs a systemctl command with sudo.
func SudoSystemctl(action, service string) error { return Default.SudoSystemctl(action, service) }

// Exists checks if a command exists in PATH.
func Exists(name string) bool { return Default.Exists(name) }

// CheckPort checks if a port is in use.
func CheckPort(port string) (bool, error) { return Default.CheckPort(port) }

// CheckPortOnAddr checks whether a specific addr:port is in use.
func CheckPortOnAddr(addr, port string) (bool, error) {
	return Default.CheckPortOnAddr(addr, port)
}

// IdentifyPortProcess returns the name of the process listening on the port.
func IdentifyPortProcess(port string) string { return Default.IdentifyPortProcess(port) }

// ---- OS-backed implementation ---------------------------------------------

// OSRunner is the production implementation of Runner — it really executes
// processes against the host.
type OSRunner struct{}

func (OSRunner) Run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (OSRunner) RunWithContext(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (OSRunner) RunQuiet(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}

func (OSRunner) RunQuietWithContext(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

func (OSRunner) RunWithStdin(stdin string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (r OSRunner) SudoRun(args ...string) error { return r.Run("sudo", args...) }

func (r OSRunner) SudoRunQuiet(args ...string) ([]byte, error) { return r.RunQuiet("sudo", args...) }

func (r OSRunner) SudoWrite(path, content string) error {
	return r.RunWithStdin(content, "sudo", "tee", path)
}

func (r OSRunner) SudoMkdir(path string) error { return r.SudoRun("mkdir", "-p", path) }

func (r OSRunner) SudoRemove(path string) error { return r.SudoRun("rm", "-f", path) }

func (r OSRunner) SudoSystemctl(action, service string) error {
	return r.SudoRun("systemctl", action, service)
}

func (OSRunner) Exists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func (r OSRunner) CheckPort(port string) (bool, error) { return r.CheckPortOnAddr("", port) }

func (r OSRunner) CheckPortOnAddr(addr, port string) (bool, error) {
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

	switch {
	case r.Exists("ss"):
		output, err = r.RunQuietWithContext(ctx, "ss", "-tuln")
	case r.Exists("netstat"):
		output, err = r.RunQuietWithContext(ctx, "netstat", "-tuln")
	default:
		return false, nil // Can't check, assume available
	}

	if err != nil {
		return false, err
	}

	return parsePortListing(string(output), addr, port), nil
}

func (r OSRunner) IdentifyPortProcess(port string) string {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultTimeout)
	defer cancel()

	// ss -tlnp gives lines like:
	//   LISTEN 0 128 0.0.0.0:80 ... users:(("nginx",pid=1234,fd=6))
	if r.Exists("ss") {
		out, err := r.RunQuietWithContext(ctx, "ss", "-tlnp")
		if err == nil {
			if name := parseSSProcessName(string(out), port); name != "" {
				return name
			}
		}
	}

	// lsof -i :<port> -sTCP:LISTEN -n -P -F p c
	// Produces lines like: cnginx\n or capache2\n
	if r.Exists("lsof") {
		out, err := r.RunQuietWithContext(ctx, "lsof", "-i", ":"+port, "-sTCP:LISTEN", "-n", "-P", "-F", "c")
		if err == nil {
			if name := parseLsofProcessName(string(out)); name != "" {
				return name
			}
		}
	}

	return ""
}

// ---- pure parsers (testable without exec) ----------------------------------

// parsePortListing checks ss/netstat output for a listener on the given
// addr:port. When addr is empty the match is "any address ending in :port".
func parsePortListing(output, addr, port string) bool {
	portSuffix := ":" + port
	for line := range strings.SplitSeq(output, "\n") {
		fields := strings.FieldsSeq(line)
		for field := range fields {
			if !strings.HasSuffix(field, portSuffix) {
				continue
			}
			// When checking a specific addr, only count it as in-use if the
			// listening address matches. Addresses that bind on a *different*
			// IP (e.g. 127.0.0.53:53) do not block binding on addr:port.
			if addr != "" {
				host := strings.TrimSuffix(field, portSuffix)
				// Wildcard listeners (0.0.0.0 / ::) conflict with any addr.
				if host != "0.0.0.0" && host != "[::]" && host != addr {
					continue // Different specific IP — not a conflict.
				}
			}
			return true
		}
	}
	return false
}

// parseSSProcessName extracts the listening process name from `ss -tlnp`
// output for a given port. Returns "" when not found.
func parseSSProcessName(output, port string) string {
	portSuffix := ":" + port
	for line := range strings.SplitSeq(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		if !strings.HasSuffix(fields[3], portSuffix) {
			continue
		}
		for _, f := range fields {
			if strings.HasPrefix(f, "users:") {
				if name := extractProcessName(f); name != "" {
					return name
				}
			}
		}
	}
	return ""
}

// parseLsofProcessName extracts the process name from `lsof -F c` output —
// each command line is prefixed with `c`.
func parseLsofProcessName(output string) string {
	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		if after, ok := strings.CutPrefix(line, "c"); ok {
			if after != "" {
				return after
			}
		}
	}
	return ""
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

// extractProcessName parses the process name out of an ss users field.
// Input looks like: users:(("nginx",pid=1234,fd=6))
func extractProcessName(field string) string {
	_, after, ok := strings.Cut(field, `"`)
	if !ok {
		return ""
	}
	before, _, ok := strings.Cut(after, `"`)
	if !ok {
		return ""
	}
	return before
}
