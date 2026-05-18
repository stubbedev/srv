package firewall

import (
	"context"
	"errors"
	"testing"

	"github.com/stubbedev/srv/internal/shell"
	"github.com/stubbedev/srv/internal/shell/shelltest"
)

func TestFirewallTypeString(t *testing.T) {
	cases := []struct {
		fw   FirewallType
		want string
	}{
		{FirewallUFW, "ufw"},
		{FirewallFirewalld, "firewalld"},
		{FirewallIPTables, "iptables"},
		{FirewallNone, "none"},
		{FirewallType(99), "none"},
	}
	for _, c := range cases {
		if got := c.fw.String(); got != c.want {
			t.Errorf("FirewallType(%d).String() = %q, want %q", c.fw, got, c.want)
		}
	}
}

func swapShell(t *testing.T, r shell.Runner) {
	t.Helper()
	t.Cleanup(shell.SwapDefault(r))
}

func TestDetectUFW(t *testing.T) {
	fake := shelltest.New(map[string]shelltest.Response{
		"ufw":      {Exists: true},
		"sudo:ufw": {Out: []byte("Status: active"), Err: nil},
	})
	swapShell(t, fake)
	if got := Detect(); got != FirewallUFW {
		t.Errorf("Detect = %v, want UFW", got)
	}
}

func TestDetectFirewalld(t *testing.T) {
	fake := shelltest.New(map[string]shelltest.Response{
		"ufw":          {Exists: false},
		"firewall-cmd": {Exists: true, Out: []byte("running")},
	})
	swapShell(t, fake)
	if got := Detect(); got != FirewallFirewalld {
		t.Errorf("Detect = %v, want Firewalld", got)
	}
}

func TestDetectIPTables(t *testing.T) {
	fake := shelltest.New(map[string]shelltest.Response{
		"iptables": {Exists: true},
	})
	swapShell(t, fake)
	if got := Detect(); got != FirewallIPTables {
		t.Errorf("Detect = %v, want IPTables", got)
	}
}

func TestDetectNone(t *testing.T) {
	fake := shelltest.New(nil)
	swapShell(t, fake)
	if got := Detect(); got != FirewallNone {
		t.Errorf("Detect = %v, want None", got)
	}
}

func TestDetectUFWInactiveFallsThrough(t *testing.T) {
	fake := shelltest.New(map[string]shelltest.Response{
		"ufw":      {Exists: true},
		"sudo:ufw": {Out: []byte("Status: inactive")},
		"iptables": {Exists: true},
	})
	swapShell(t, fake)
	if got := Detect(); got != FirewallIPTables {
		t.Errorf("Detect = %v, want IPTables when UFW inactive", got)
	}
}

func TestDetectFirewalldErrorFallsThrough(t *testing.T) {
	fake := shelltest.New(map[string]shelltest.Response{
		"firewall-cmd": {Exists: true, Err: errors.New("not running")},
		"iptables":     {Exists: true},
	})
	swapShell(t, fake)
	if got := Detect(); got != FirewallIPTables {
		t.Errorf("Detect = %v, want IPTables when firewalld errors", got)
	}
}

func TestCheckPortsNoFirewall(t *testing.T) {
	swapShell(t, shelltest.New(nil))
	st := CheckPorts()
	if st.Firewall != FirewallNone || !st.HTTPOpen || !st.HTTPSOpen {
		t.Errorf("CheckPorts no firewall = %+v, want all open", st)
	}
}

func TestCheckPortsUFW(t *testing.T) {
	out := "Status: active\n80                         ALLOW       Anywhere\n443/tcp                    ALLOW       Anywhere\n"
	fake := shelltest.New(map[string]shelltest.Response{
		"ufw":      {Exists: true},
		"sudo:ufw": {Out: []byte(out)},
	})
	swapShell(t, fake)
	st := CheckPorts()
	if st.Firewall != FirewallUFW {
		t.Fatalf("firewall = %v", st.Firewall)
	}
	if !st.HTTPOpen {
		t.Error("HTTP should be open")
	}
	if !st.HTTPSOpen {
		t.Error("HTTPS should be open")
	}
}

func TestCheckPortsFirewalld(t *testing.T) {
	fake := &shelltest.Fake{
		Responses: map[string]shelltest.Response{
			"firewall-cmd": {Exists: true},
		},
		Handler: func(method, name string, args []string, _ string) (shelltest.Response, bool) {
			if name == "firewall-cmd" && len(args) > 0 && args[0] == "--state" {
				return shelltest.Response{Out: []byte("running")}, true
			}
			if name == "firewall-cmd" && len(args) > 0 && args[0] == "--list-services" {
				return shelltest.Response{Out: []byte("dhcpv6-client http https")}, true
			}
			return shelltest.Response{}, false
		},
	}
	swapShell(t, fake)
	st := CheckPorts()
	if st.Firewall != FirewallFirewalld {
		t.Fatalf("firewall = %v", st.Firewall)
	}
	if !st.HTTPOpen || !st.HTTPSOpen {
		t.Errorf("expected both open: %+v", st)
	}
}

func TestCheckPortsIPTables(t *testing.T) {
	fake := shelltest.New(map[string]shelltest.Response{
		"iptables":      {Exists: true},
		"sudo:iptables": {Out: []byte("ACCEPT tcp dpt:80\nACCEPT tcp dpt:443\n")},
	})
	swapShell(t, fake)
	st := CheckPorts()
	if st.Firewall != FirewallIPTables {
		t.Fatalf("firewall = %v", st.Firewall)
	}
	if !st.HTTPOpen || !st.HTTPSOpen {
		t.Errorf("expected both open: %+v", st)
	}
}

func TestCheckUFWPortDenied(t *testing.T) {
	out := "Status: active\n80                         DENY        Anywhere\n"
	fake := shelltest.New(map[string]shelltest.Response{
		"sudo:ufw": {Out: []byte(out)},
	})
	swapShell(t, fake)
	if checkUFWPort("80") {
		t.Error("DENY rule should not count as open")
	}
}

func TestCheckUFWPortErr(t *testing.T) {
	fake := shelltest.New(map[string]shelltest.Response{
		"sudo:ufw": {Err: errors.New("nope")},
	})
	swapShell(t, fake)
	if checkUFWPort("80") {
		t.Error("err should yield false")
	}
}

func TestCheckUFWPortMissing(t *testing.T) {
	fake := shelltest.New(map[string]shelltest.Response{
		"sudo:ufw": {Out: []byte("Status: active\n")},
	})
	swapShell(t, fake)
	if checkUFWPort("80") {
		t.Error("missing port -> false")
	}
}

func TestCheckFirewalldServiceErr(t *testing.T) {
	fake := shelltest.New(map[string]shelltest.Response{
		"firewall-cmd": {Err: errors.New("x")},
	})
	swapShell(t, fake)
	if checkFirewalldService("http") {
		t.Error("err should yield false")
	}
}

func TestCheckIPTablesAcceptPolicy(t *testing.T) {
	fake := shelltest.New(map[string]shelltest.Response{
		"sudo:iptables": {Out: []byte("Chain INPUT (policy ACCEPT)\n")},
	})
	swapShell(t, fake)
	if !checkIPTablesPort("80") {
		t.Error("ACCEPT policy should yield true")
	}
}

func TestCheckIPTablesPortErr(t *testing.T) {
	fake := shelltest.New(map[string]shelltest.Response{
		"sudo:iptables": {Err: errors.New("x")},
	})
	swapShell(t, fake)
	if checkIPTablesPort("80") {
		t.Error("err should yield false")
	}
}

func TestOpenPortsNone(t *testing.T) {
	swapShell(t, shelltest.New(nil))
	if err := OpenPorts(); err != nil {
		t.Errorf("OpenPorts none -> err: %v", err)
	}
}

func TestOpenPortsUFWSuccess(t *testing.T) {
	fake := shelltest.New(map[string]shelltest.Response{
		"ufw":      {Exists: true},
		"sudo:ufw": {Out: []byte("Status: active")},
	})
	swapShell(t, fake)
	if err := OpenPorts(); err != nil {
		t.Errorf("OpenPorts UFW err: %v", err)
	}
}

func TestOpenPortsUFWErr(t *testing.T) {
	calls := 0
	swapShell(t, &errAfter{n: 2, calls: &calls, fake: shelltest.New(map[string]shelltest.Response{
		"ufw":      {Exists: true},
		"sudo:ufw": {Out: []byte("Status: active")},
	})})
	if err := OpenPorts(); err == nil {
		t.Error("expected err when SudoRun for ufw fails")
	}
}

func TestOpenPortsFirewalldSuccess(t *testing.T) {
	fake := shelltest.New(map[string]shelltest.Response{
		"firewall-cmd": {Exists: true, Out: []byte("running")},
	})
	swapShell(t, fake)
	if err := OpenPorts(); err != nil {
		t.Errorf("OpenPorts firewalld err: %v", err)
	}
}

func TestOpenPortsIPTablesSuccess(t *testing.T) {
	fake := shelltest.New(map[string]shelltest.Response{
		"iptables": {Exists: true},
	})
	swapShell(t, fake)
	if err := OpenPorts(); err != nil {
		t.Errorf("OpenPorts iptables err: %v", err)
	}
}

func TestIsActiveTrue(t *testing.T) {
	fake := shelltest.New(map[string]shelltest.Response{
		"ufw":      {Exists: true},
		"sudo:ufw": {Out: []byte("Status: active")},
	})
	swapShell(t, fake)
	if !IsActive() {
		t.Error("IsActive should be true")
	}
}

func TestIsActiveFalse(t *testing.T) {
	swapShell(t, shelltest.New(nil))
	if IsActive() {
		t.Error("IsActive should be false")
	}
}

func TestOpenFirewalldErr1(t *testing.T) {
	calls := 0
	swapShell(t, &errAfter{n: 1, calls: &calls, fake: shelltest.New(map[string]shelltest.Response{
		"firewall-cmd": {Exists: true, Out: []byte("running")},
	})})
	if err := OpenPorts(); err == nil {
		t.Error("expected err on first sudo")
	}
}

func TestOpenFirewalldErr2(t *testing.T) {
	calls := 0
	swapShell(t, &errAfter{n: 2, calls: &calls, fake: shelltest.New(map[string]shelltest.Response{
		"firewall-cmd": {Exists: true, Out: []byte("running")},
	})})
	if err := OpenPorts(); err == nil {
		t.Error("expected err on second sudo")
	}
}

func TestOpenFirewalldErr3(t *testing.T) {
	calls := 0
	swapShell(t, &errAfter{n: 3, calls: &calls, fake: shelltest.New(map[string]shelltest.Response{
		"firewall-cmd": {Exists: true, Out: []byte("running")},
	})})
	if err := OpenPorts(); err == nil {
		t.Error("expected err on reload")
	}
}

func TestOpenIPTablesErr2(t *testing.T) {
	calls := 0
	swapShell(t, &errAfter{n: 2, calls: &calls, fake: shelltest.New(map[string]shelltest.Response{
		"iptables": {Exists: true},
	})})
	if err := OpenPorts(); err == nil {
		t.Error("expected err on second sudo")
	}
}

func TestPersistIPTablesRulesAllBranches(t *testing.T) {
	// netfilter-persistent branch.
	swapShell(t, shelltest.New(map[string]shelltest.Response{
		"netfilter-persistent": {Exists: true},
	}))
	persistIPTablesRules()

	// iptables-save branch.
	swapShell(t, shelltest.New(map[string]shelltest.Response{
		"iptables-save":      {Exists: true},
		"sudo:iptables-save": {Out: []byte("rules")},
	}))
	persistIPTablesRules()

	// Fallback `service iptables save`.
	swapShell(t, shelltest.New(nil))
	persistIPTablesRules()
}

// errAfter is a runner that returns an error on the Nth SudoRun call. Used to
// force OpenPortsUFW's second allow command to fail.
type errAfter struct {
	fake  *shelltest.Fake
	n     int
	calls *int
}

func (e *errAfter) Run(name string, args ...string) error { return e.fake.Run(name, args...) }
func (e *errAfter) RunWithContext(ctx context.Context, name string, args ...string) error {
	return e.fake.RunWithContext(ctx, name, args...)
}
func (e *errAfter) RunQuiet(name string, args ...string) ([]byte, error) {
	return e.fake.RunQuiet(name, args...)
}
func (e *errAfter) RunQuietWithContext(ctx context.Context, name string, args ...string) ([]byte, error) {
	return e.fake.RunQuietWithContext(ctx, name, args...)
}
func (e *errAfter) RunWithStdin(stdin string, name string, args ...string) error {
	return e.fake.RunWithStdin(stdin, name, args...)
}
func (e *errAfter) SudoRun(args ...string) error {
	*e.calls++
	if *e.calls >= e.n {
		return errors.New("forced")
	}
	return e.fake.SudoRun(args...)
}
func (e *errAfter) SudoRunQuiet(args ...string) ([]byte, error) {
	return e.fake.SudoRunQuiet(args...)
}
func (e *errAfter) SudoWrite(path, content string) error { return e.fake.SudoWrite(path, content) }
func (e *errAfter) SudoMkdir(path string) error          { return e.fake.SudoMkdir(path) }
func (e *errAfter) SudoRemove(path string) error         { return e.fake.SudoRemove(path) }
func (e *errAfter) SudoSystemctl(action, service string) error {
	return e.fake.SudoSystemctl(action, service)
}
func (e *errAfter) Exists(name string) bool                                    { return e.fake.Exists(name) }
func (e *errAfter) CheckPort(port string) (bool, error)                        { return e.fake.CheckPort(port) }
func (e *errAfter) CheckPortOnAddr(addr, port string) (bool, error)            { return e.fake.CheckPortOnAddr(addr, port) }
func (e *errAfter) IdentifyPortProcess(port string) string                     { return e.fake.IdentifyPortProcess(port) }
