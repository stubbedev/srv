package ui

import (
	"bytes"
	"strings"
	"testing"
)

func TestStripAnsiBasic(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "hello", "hello"},
		{"empty", "", ""},
		{"sgr-color", "\x1b[31mred\x1b[0m", "red"},
		{"sgr-bold-color", "\x1b[1;32mok\x1b[0m", "ok"},
		{"multiple", "\x1b[31ma\x1b[0mb\x1b[32mc\x1b[0m", "abc"},
		{"cursor-move", "x\x1b[2Ay", "xy"},
		{"erase", "x\x1b[Ky", "xy"},
		{"unterminated-passes-through", "\x1b[31m", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stripAnsi(tt.in); got != tt.want {
				t.Errorf("stripAnsi(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestStripAnsiPreservesRunes(t *testing.T) {
	in := "\x1b[31mæblegrød\x1b[0m"
	if got := stripAnsi(in); got != "æblegrød" {
		t.Errorf("stripAnsi unicode = %q", got)
	}
}

func TestUsageError(t *testing.T) {
	err := UsageError("srv add PATH", "domain %q invalid", "x.y")
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "domain") {
		t.Errorf("missing message body: %q", msg)
	}
	if !strings.Contains(msg, "srv add PATH") {
		t.Errorf("missing usage line: %q", msg)
	}
}

func TestIndent(t *testing.T) {
	tests := []struct {
		level int
		in    string
		args  []any
		want  string
	}{
		{0, "x", nil, "x"},
		{1, "x", nil, "  x"},
		{2, "x", nil, "    x"},
		{1, "n=%d", []any{42}, "  n=42"},
	}
	for _, tt := range tests {
		if got := Indent(tt.level, tt.in, tt.args...); got != tt.want {
			t.Errorf("Indent(%d,%q) = %q, want %q", tt.level, tt.in, got, tt.want)
		}
	}
}

func TestNewStepsStartsZero(t *testing.T) {
	s := NewSteps(5)
	if s.total != 5 || s.current != 0 {
		t.Errorf("Steps init = (%d,%d), want (5,0)", s.total, s.current)
	}
}

func TestStatusColorRendersForKnownStates(t *testing.T) {
	cases := []string{"running", "valid", "stopped", "auto", "broken", "expired", "missing", "expiring", "unknown", "partial-extras"}
	for _, c := range cases {
		out := StatusColor(c)
		if !strings.Contains(stripAnsi(out), c) && !strings.Contains(stripAnsi(out), strings.TrimPrefix(c, "partial-")) {
			t.Errorf("StatusColor(%q) lost text: %q", c, stripAnsi(out))
		}
	}
}

func TestTypeColor(t *testing.T) {
	if !strings.Contains(stripAnsi(TypeColor(true)), "local") {
		t.Error("TypeColor(true) missing 'local'")
	}
	if !strings.Contains(stripAnsi(TypeColor(false)), "production") {
		t.Error("TypeColor(false) missing 'production'")
	}
}

func TestTextHelpersStripToSameText(t *testing.T) {
	for _, f := range []func(string) string{Highlight, SuccessText, ErrorText, WarnText, InfoText, DimText} {
		out := stripAnsi(f("hello"))
		if out != "hello" {
			t.Errorf("text helper changed text: %q", out)
		}
	}
}

func TestStepsAdvances(t *testing.T) {
	s := NewSteps(3)
	s.Next("a")
	s.Next("b")
	if s.current != 2 {
		t.Errorf("after 2 Next, current=%d", s.current)
	}
	s.Done("ok")
	s.Skip("skip")
	if s.current != 2 {
		t.Errorf("Done/Skip should not advance counter, got %d", s.current)
	}
}

func TestPrintTableNoCrashOnEmpty(t *testing.T) {
	PrintTable([]string{"a", "b"}, nil)
	PrintTable([]string{"a"}, [][]string{{"row1"}})
}

func TestVerboseLogToggle(t *testing.T) {
	prev := Verbose
	defer func() { Verbose = prev }()
	Verbose = false
	VerboseLog("x %d", 1)
	Verbose = true
	VerboseLog("x %d", 2)
}

func TestSuccessErrorWarnInfoSmoke(t *testing.T) {
	Success("ok %d", 1)
	Error("fail %d", 2)
	Warn("warn %d", 3)
	Info("info %d", 4)
	Dim("dim")
	Bold("bold")
	Code("code")
	Print("plain %d", 5)
	Blank()
}

func TestIndentedHelpersSmoke(t *testing.T) {
	IndentedSuccess(1, "ok")
	IndentedError(1, "err")
	IndentedWarn(1, "warn")
	IndentedInfo(1, "info")
	IndentedDim(2, "dim")
}

func TestSafeHelpersSmoke(t *testing.T) {
	SafeIndentedDim(1, "x")
	SafeError("e")
	SafeWarn("w")
}

// Quiet mode suppresses Info/Warn/Dim/Success but lets Error and Print through.
func TestQuietSuppressesDiagnostics(t *testing.T) {
	prev := Quiet
	defer func() { Quiet = prev }()

	var stdout, stderr bytes.Buffer
	swapStdout, swapStderr := outStdout, outStderr
	defer func() { outStdout, outStderr = swapStdout, swapStderr }()
	outStdout = &stdout
	outStderr = &stderr

	Quiet = true
	Info("info")
	Warn("warn")
	Dim("dim")
	Success("ok")
	Print("result")
	Error("err")

	if stderr.String() == "" {
		t.Error("Error should still write under --quiet")
	}
	if !strings.Contains(stderr.String(), "err") {
		t.Errorf("stderr missing error: %q", stderr.String())
	}
	if strings.Contains(stderr.String(), "info") || strings.Contains(stderr.String(), "warn") || strings.Contains(stderr.String(), "dim") || strings.Contains(stderr.String(), "ok") {
		t.Errorf("Quiet leaked diagnostic: %q", stderr.String())
	}
	if !strings.Contains(stdout.String(), "result") {
		t.Errorf("Print result missing from stdout: %q", stdout.String())
	}
}

// Print writes results to stdout, never stderr.
func TestPrintGoesToStdout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	swapStdout, swapStderr := outStdout, outStderr
	defer func() { outStdout, outStderr = swapStdout, swapStderr }()
	outStdout = &stdout
	outStderr = &stderr

	Print("data")
	if !strings.Contains(stdout.String(), "data") {
		t.Errorf("stdout missing 'data': %q", stdout.String())
	}
	if strings.Contains(stderr.String(), "data") {
		t.Errorf("Print leaked to stderr: %q", stderr.String())
	}
}

// Info/Warn/Dim/Success/Error all go to stderr — Print stays untouched.
func TestDiagnosticsGoToStderr(t *testing.T) {
	var stdout, stderr bytes.Buffer
	swapStdout, swapStderr := outStdout, outStderr
	defer func() { outStdout, outStderr = swapStdout, swapStderr }()
	outStdout = &stdout
	outStderr = &stderr

	Info("hello-info")
	Warn("hello-warn")
	Dim("hello-dim")
	Success("hello-ok")
	Error("hello-err")

	for _, want := range []string{"hello-info", "hello-warn", "hello-dim", "hello-ok", "hello-err"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr missing %q: %q", want, stderr.String())
		}
		if strings.Contains(stdout.String(), want) {
			t.Errorf("diagnostic %q leaked to stdout: %q", want, stdout.String())
		}
	}
}
