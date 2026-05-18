// Package shelltest provides a controllable Runner implementation used by
// other packages' tests to stub out shell command behaviour.
package shelltest

import (
	"context"
	"sync"

	"github.com/stubbedev/srv/internal/shell"
)

// Call captures one invocation of a Fake runner method. The Method field
// uses the verb name (Run, RunQuiet, SudoRun, …) for readability.
type Call struct {
	Method string
	Name   string
	Args   []string
	Stdin  string
}

// Response describes what Fake should return for a particular method
// invocation. Out is returned for *Quiet variants; Err is returned in all
// cases; for Exists, Exists controls the bool.
type Response struct {
	Out    []byte
	Err    error
	Exists bool
	// Process is the value returned by IdentifyPortProcess.
	Process string
	// InUse is the value returned by CheckPort/CheckPortOnAddr.
	InUse bool
}

// Fake is a controllable shell.Runner used by tests. It records every call
// and returns user-configured responses keyed by the command name.
//
// Zero value is ready to use: every call defaults to a zero Response.
type Fake struct {
	mu sync.Mutex

	// Calls is the in-order list of method invocations.
	Calls []Call

	// Responses keys the response for a given command name. For Run/RunQuiet
	// the key is the command name (e.g. "ufw"). For Sudo* it is "sudo:<head>"
	// where <head> is the first positional argument. For Exists it is the
	// argument. For CheckPort/CheckPortOnAddr the key is "port:<port>". For
	// IdentifyPortProcess it is "process:<port>".
	Responses map[string]Response

	// Handler, if non-nil, is consulted before Responses. Receive the full
	// method/name/args tuple and either returns a Response and ok=true to
	// override, or ok=false to fall through to the keyed lookup.
	Handler func(method, name string, args []string, stdin string) (Response, bool)

	// Default is used when no matching key exists in Responses.
	Default Response
}

var _ shell.Runner = (*Fake)(nil)

// New returns a fresh Fake with the given response map.
func New(responses map[string]Response) *Fake {
	return &Fake{Responses: responses}
}

func (f *Fake) lookup(key string) Response {
	if r, ok := f.Responses[key]; ok {
		return r
	}
	return f.Default
}

// resolve consults Handler first (if set), then the keyed Responses map.
// method, name, args, stdin describe the current call.
func (f *Fake) resolve(method, name string, args []string, stdin, key string) Response {
	if f.Handler != nil {
		if r, ok := f.Handler(method, name, args, stdin); ok {
			return r
		}
	}
	return f.lookup(key)
}

func (f *Fake) record(method, name string, args []string, stdin string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := append([]string(nil), args...)
	f.Calls = append(f.Calls, Call{Method: method, Name: name, Args: cp, Stdin: stdin})
}

// Snapshot returns a copy of the calls recorded so far.
func (f *Fake) Snapshot() []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Call, len(f.Calls))
	copy(out, f.Calls)
	return out
}

func (f *Fake) Run(name string, args ...string) error {
	f.record("Run", name, args, "")
	return f.resolve("Run", name, args, "", name).Err
}

func (f *Fake) RunWithContext(ctx context.Context, name string, args ...string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.record("RunWithContext", name, args, "")
	return f.resolve("RunWithContext", name, args, "", name).Err
}

func (f *Fake) RunQuiet(name string, args ...string) ([]byte, error) {
	f.record("RunQuiet", name, args, "")
	r := f.resolve("RunQuiet", name, args, "", name)
	return r.Out, r.Err
}

func (f *Fake) RunQuietWithContext(ctx context.Context, name string, args ...string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.record("RunQuietWithContext", name, args, "")
	r := f.resolve("RunQuietWithContext", name, args, "", name)
	return r.Out, r.Err
}

func (f *Fake) RunWithStdin(stdin string, name string, args ...string) error {
	f.record("RunWithStdin", name, args, stdin)
	return f.resolve("RunWithStdin", name, args, stdin, name).Err
}

func (f *Fake) SudoRun(args ...string) error {
	head := ""
	if len(args) > 0 {
		head = args[0]
	}
	f.record("SudoRun", "sudo", args, "")
	return f.resolve("SudoRun", "sudo", args, "", "sudo:"+head).Err
}

func (f *Fake) SudoRunQuiet(args ...string) ([]byte, error) {
	head := ""
	if len(args) > 0 {
		head = args[0]
	}
	f.record("SudoRunQuiet", "sudo", args, "")
	r := f.resolve("SudoRunQuiet", "sudo", args, "", "sudo:"+head)
	return r.Out, r.Err
}

func (f *Fake) SudoWrite(path, content string) error {
	args := []string{"tee", path}
	f.record("SudoWrite", "sudo", args, content)
	return f.resolve("SudoWrite", "sudo", args, content, "sudo:tee").Err
}

func (f *Fake) SudoMkdir(path string) error {
	args := []string{"mkdir", "-p", path}
	f.record("SudoMkdir", "sudo", args, "")
	return f.resolve("SudoMkdir", "sudo", args, "", "sudo:mkdir").Err
}

func (f *Fake) SudoRemove(path string) error {
	args := []string{"rm", "-f", path}
	f.record("SudoRemove", "sudo", args, "")
	return f.resolve("SudoRemove", "sudo", args, "", "sudo:rm").Err
}

func (f *Fake) SudoSystemctl(action, service string) error {
	args := []string{"systemctl", action, service}
	f.record("SudoSystemctl", "sudo", args, "")
	return f.resolve("SudoSystemctl", "sudo", args, "", "sudo:systemctl").Err
}

func (f *Fake) Exists(name string) bool {
	f.record("Exists", name, nil, "")
	if f.Handler != nil {
		if r, ok := f.Handler("Exists", name, nil, ""); ok {
			return r.Exists
		}
	}
	if r, ok := f.Responses[name]; ok {
		return r.Exists
	}
	return f.Default.Exists
}

func (f *Fake) CheckPort(port string) (bool, error) {
	f.record("CheckPort", port, nil, "")
	r := f.resolve("CheckPort", port, nil, "", "port:"+port)
	return r.InUse, r.Err
}

func (f *Fake) CheckPortOnAddr(addr, port string) (bool, error) {
	f.record("CheckPortOnAddr", port, []string{addr}, "")
	r := f.resolve("CheckPortOnAddr", port, []string{addr}, "", "port:"+port)
	return r.InUse, r.Err
}

func (f *Fake) IdentifyPortProcess(port string) string {
	f.record("IdentifyPortProcess", port, nil, "")
	return f.resolve("IdentifyPortProcess", port, nil, "", "process:"+port).Process
}
