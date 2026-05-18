package shell

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestIsPortInUseError(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{errors.New("listen tcp :80: bind: address already in use"), true},
		{errors.New("dial tcp: address already in use"), true},
		{errors.New("permission denied"), false},
	}
	for _, tt := range tests {
		if got := isPortInUseError(tt.err); got != tt.want {
			t.Errorf("isPortInUseError(%v) = %v, want %v", tt.err, got, tt.want)
		}
	}
}

func TestExtractProcessName(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{`users:(("nginx",pid=1234,fd=6))`, "nginx"},
		{`users:(("php-fpm",pid=1,fd=2))`, "php-fpm"},
		{`users:`, ""},
		{`users:(("`, ""},
		{`no-quotes`, ""},
	}
	for _, tt := range tests {
		if got := extractProcessName(tt.in); got != tt.want {
			t.Errorf("extractProcessName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestParseSSProcessName(t *testing.T) {
	out := `State Recv-Q Send-Q Local Address:Port Peer Address:Port Process
LISTEN 0      128         0.0.0.0:80        0.0.0.0:*    users:(("nginx",pid=1234,fd=6))
LISTEN 0      128         0.0.0.0:443       0.0.0.0:*    users:(("traefik",pid=2,fd=3))
`
	if got := parseSSProcessName(out, "80"); got != "nginx" {
		t.Errorf("port 80 -> %q, want nginx", got)
	}
	if got := parseSSProcessName(out, "443"); got != "traefik" {
		t.Errorf("port 443 -> %q, want traefik", got)
	}
	if got := parseSSProcessName(out, "9999"); got != "" {
		t.Errorf("missing port -> %q, want empty", got)
	}
	if got := parseSSProcessName("", "80"); got != "" {
		t.Errorf("empty output -> %q", got)
	}
}

func TestParseSSProcessNameSkipsShortLines(t *testing.T) {
	out := "short\nshort line"
	if got := parseSSProcessName(out, "80"); got != "" {
		t.Errorf("short lines -> %q, want empty", got)
	}
}

func TestParseLsofProcessName(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"p1234\ncnginx", "nginx"},
		{"cphp-fpm", "php-fpm"},
		{"", ""},
		{"\n", ""},
		{"x\ny", ""},
		{"c", ""},
	}
	for _, tt := range tests {
		if got := parseLsofProcessName(tt.in); got != tt.want {
			t.Errorf("parseLsofProcessName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestParsePortListingPortSuffixMatch(t *testing.T) {
	ss := strings.Join([]string{
		"LISTEN 0 128 0.0.0.0:80 0.0.0.0:*",
		"LISTEN 0 128 127.0.0.1:53 0.0.0.0:*",
		"LISTEN 0 128 [::]:443 [::]:*",
	}, "\n")

	if !parsePortListing(ss, "", "80") {
		t.Error("expected port 80 in use (wildcard addr)")
	}
	if parsePortListing(ss, "", "8080") {
		t.Error("expected port 8080 not in use")
	}
}

func TestParsePortListingSpecificAddr(t *testing.T) {
	// 127.0.0.53 listener should NOT block 127.0.0.1:53.
	ss := "LISTEN 0 128 127.0.0.53:53 0.0.0.0:*"
	if parsePortListing(ss, "127.0.0.1", "53") {
		t.Error("specific addr should not match different specific IP")
	}
	if !parsePortListing(ss, "127.0.0.53", "53") {
		t.Error("same specific addr should match")
	}
}

func TestParsePortListingWildcardConflicts(t *testing.T) {
	ss := "LISTEN 0 128 0.0.0.0:80 0.0.0.0:*"
	if !parsePortListing(ss, "127.0.0.1", "80") {
		t.Error("0.0.0.0 listener must conflict with any specific addr")
	}
	ss6 := "LISTEN 0 128 [::]:80 [::]:*"
	if !parsePortListing(ss6, "127.0.0.1", "80") {
		t.Error("[::] listener must conflict with any specific addr")
	}
}

func TestSwapDefaultRestores(t *testing.T) {
	prev := Default
	// Use a pointer receiver so the value is comparable.
	fake := &stubRunner{}
	restore := SwapDefault(fake)
	if Default != fake {
		t.Fatalf("SwapDefault did not install fake")
	}
	restore()
	if Default != prev {
		t.Errorf("restore did not revert Default")
	}
}

func TestPackageContextShims(t *testing.T) {
	fake := &stubRunner{out: []byte("data")}
	t.Cleanup(SwapDefault(fake))
	ctx := context.Background()
	if err := RunWithContext(ctx, "x"); err != nil {
		t.Errorf("RunWithContext err: %v", err)
	}
	if _, err := RunQuietWithContext(ctx, "x"); err != nil {
		t.Errorf("RunQuietWithContext err: %v", err)
	}
}

// OSRunner sudo methods shell out to real sudo. Pointing at a non-existent
// binary makes them fail without prompting. We don't assert anything; the
// call just exercises the seam.
func TestOSRunnerSudoMethodsExercise(t *testing.T) {
	r := OSRunner{}
	_ = r.SudoRun("false-binary-srv-12345")
	_, _ = r.SudoRunQuiet("false-binary-srv-12345")
	_ = r.SudoWrite("/tmp/srv-sudo-test-12345", "data")
	_ = r.SudoMkdir("/tmp/srv-sudo-mkdir-12345")
	_ = r.SudoRemove("/tmp/srv-sudo-rm-12345")
	_ = r.SudoSystemctl("status", "false-service-srv-12345")
}

func TestPackageHelpersDelegate(t *testing.T) {
	fake := &stubRunner{out: []byte("hello"), err: nil, exists: true}
	t.Cleanup(SwapDefault(fake))

	if err := Run("echo", "x"); err != nil {
		t.Errorf("Run err: %v", err)
	}
	if out, _ := RunQuiet("echo"); string(out) != "hello" {
		t.Errorf("RunQuiet = %q", out)
	}
	if !Exists("anything") {
		t.Error("Exists should delegate to fake")
	}
	if err := SudoRun("a"); err != nil {
		t.Errorf("SudoRun err: %v", err)
	}
	if _, err := SudoRunQuiet("a"); err != nil {
		t.Errorf("SudoRunQuiet err: %v", err)
	}
	if err := SudoWrite("/x", "data"); err != nil {
		t.Errorf("SudoWrite err: %v", err)
	}
	if err := SudoMkdir("/x"); err != nil {
		t.Errorf("SudoMkdir err: %v", err)
	}
	if err := SudoRemove("/x"); err != nil {
		t.Errorf("SudoRemove err: %v", err)
	}
	if err := SudoSystemctl("start", "x"); err != nil {
		t.Errorf("SudoSystemctl err: %v", err)
	}
	if err := RunWithStdin("in", "cmd"); err != nil {
		t.Errorf("RunWithStdin err: %v", err)
	}
	if _, err := CheckPort("80"); err != nil {
		t.Errorf("CheckPort err: %v", err)
	}
	if _, err := CheckPortOnAddr("127.0.0.1", "80"); err != nil {
		t.Errorf("CheckPortOnAddr err: %v", err)
	}
	if got := IdentifyPortProcess("80"); got != "" {
		t.Errorf("IdentifyPortProcess = %q", got)
	}
}

func TestOSRunnerRun(t *testing.T) {
	r := OSRunner{}
	if err := r.Run("true"); err != nil {
		t.Errorf("err: %v", err)
	}
	if err := r.Run("false"); err == nil {
		t.Error("expected non-nil err from `false`")
	}
}

func TestOSRunnerRunWithContext(t *testing.T) {
	r := OSRunner{}
	ctx := context.Background()
	if err := r.RunWithContext(ctx, "true"); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestOSRunnerRunQuiet(t *testing.T) {
	r := OSRunner{}
	out, err := r.RunQuiet("echo", "hello")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "hello") {
		t.Errorf("got %q", out)
	}
}

func TestOSRunnerRunQuietWithContext(t *testing.T) {
	r := OSRunner{}
	ctx := context.Background()
	if _, err := r.RunQuietWithContext(ctx, "echo", "x"); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestOSRunnerRunWithStdin(t *testing.T) {
	r := OSRunner{}
	if err := r.RunWithStdin("data", "cat"); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestOSRunnerExists(t *testing.T) {
	r := OSRunner{}
	if !r.Exists("sh") {
		t.Error("sh should exist")
	}
	if r.Exists("definitely-not-a-binary-12345") {
		t.Error("bogus binary should not exist")
	}
}

func TestOSRunnerCheckPortAvailable(t *testing.T) {
	r := OSRunner{}
	if _, err := r.CheckPort("65432"); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestOSRunnerCheckPortOnAddrAvailable(t *testing.T) {
	r := OSRunner{}
	if _, err := r.CheckPortOnAddr("127.0.0.1", "65431"); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestOSRunnerIdentifyPortProcessEmptyPort(t *testing.T) {
	r := OSRunner{}
	if got := r.IdentifyPortProcess("65430"); got != "" {
		t.Logf("got %q (may be set on some hosts)", got)
	}
}

// stubRunner implements Runner with predictable returns; not for general
// reuse — that's what shelltest.Fake is for.
type stubRunner struct {
	out    []byte
	err    error
	exists bool
}

func (s stubRunner) Run(string, ...string) error                                       { return s.err }
func (s stubRunner) RunWithContext(context.Context, string, ...string) error           { return s.err }
func (s stubRunner) RunQuiet(string, ...string) ([]byte, error)                        { return s.out, s.err }
func (s stubRunner) RunQuietWithContext(context.Context, string, ...string) ([]byte, error) { return s.out, s.err }
func (s stubRunner) RunWithStdin(string, string, ...string) error                      { return s.err }
func (s stubRunner) SudoRun(...string) error                                           { return s.err }
func (s stubRunner) SudoRunQuiet(...string) ([]byte, error)                            { return s.out, s.err }
func (s stubRunner) SudoWrite(string, string) error                                    { return s.err }
func (s stubRunner) SudoMkdir(string) error                                            { return s.err }
func (s stubRunner) SudoRemove(string) error                                           { return s.err }
func (s stubRunner) SudoSystemctl(string, string) error                                { return s.err }
func (s stubRunner) Exists(string) bool                                                { return s.exists }
func (s stubRunner) CheckPort(string) (bool, error)                                    { return false, s.err }
func (s stubRunner) CheckPortOnAddr(string, string) (bool, error)                      { return false, s.err }
func (s stubRunner) IdentifyPortProcess(string) string                                 { return "" }
