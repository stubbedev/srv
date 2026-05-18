package ui

import (
	"strings"
	"testing"
	"time"
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

func TestDetectUnicodeSupportLang(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	t.Setenv("LC_ALL", "")
	t.Setenv("TERM", "")
	t.Setenv("TERM_PROGRAM", "")
	if !detectUnicodeSupport() {
		t.Error("expected unicode support with UTF-8 LANG")
	}
}

func TestDetectUnicodeSupportLCAll(t *testing.T) {
	t.Setenv("LANG", "C")
	t.Setenv("LC_ALL", "en_US.UTF-8")
	t.Setenv("TERM", "")
	t.Setenv("TERM_PROGRAM", "")
	if !detectUnicodeSupport() {
		t.Error("expected unicode support via LC_ALL")
	}
}

func TestDetectUnicodeSupportTerm(t *testing.T) {
	t.Setenv("LANG", "C")
	t.Setenv("LC_ALL", "C")
	t.Setenv("TERM_PROGRAM", "")
	for _, term := range []string{"xterm-256color", "alacritty", "kitty", "wezterm", "tmux-256color", "screen-256color"} {
		t.Setenv("TERM", term)
		if !detectUnicodeSupport() {
			t.Errorf("expected unicode support for TERM=%q", term)
		}
	}
}

func TestDetectUnicodeSupportTermProgram(t *testing.T) {
	t.Setenv("LANG", "C")
	t.Setenv("LC_ALL", "C")
	t.Setenv("TERM", "dumb")
	t.Setenv("TERM_PROGRAM", "iTerm.app")
	if !detectUnicodeSupport() {
		t.Error("expected unicode support via TERM_PROGRAM")
	}
}

func TestDetectUnicodeSupportFallback(t *testing.T) {
	t.Setenv("LANG", "C")
	t.Setenv("LC_ALL", "C")
	t.Setenv("TERM", "dumb")
	t.Setenv("TERM_PROGRAM", "")
	if detectUnicodeSupport() {
		t.Error("expected no unicode support in plain C locale")
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

func TestNewSpinnerAttrs(t *testing.T) {
	s := NewSpinner("loading")
	if s == nil {
		t.Fatal("nil spinner")
	}
	if s.message != "loading" {
		t.Errorf("message = %q", s.message)
	}
	if len(s.frames) == 0 {
		t.Error("frames empty")
	}
	if s.done == nil {
		t.Error("done chan nil")
	}
}

func TestSpinnerStopIdempotent(t *testing.T) {
	s := NewSpinner("x")
	s.Stop()
	s.Stop() // must not panic
}

func TestStatusColorRendersForKnownStates(t *testing.T) {
	cases := []string{"running", "valid", "stopped", "auto", "broken", "expired", "missing", "expiring", "unknown", "partial-extras"}
	for _, c := range cases {
		out := StatusColor(c)
		// All branches return non-empty.
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
	// Steps writes to stdout — we only check counter mutation here.
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
	// Just confirm both branches don't panic.
	prev := Verbose
	defer func() { Verbose = prev }()
	Verbose = false
	VerboseLog("x %d", 1)
	Verbose = true
	VerboseLog("x %d", 2)
}

func TestSuccessErrorWarnInfoSmoke(t *testing.T) {
	// All write to stdout/stderr; just exercise to lift coverage.
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

func TestSpinnerStartStopWithSuccess(t *testing.T) {
	s := NewSpinner("loading")
	s.Start()
	// Don't wait long — animation interval is 100ms.
	s.StopWithSuccess("done")
}

func TestSpinnerStartStopWithError(t *testing.T) {
	s := NewSpinner("loading")
	s.Start()
	s.StopWithError("oops")
}

func TestSpinnerAnimationTick(t *testing.T) {
	s := NewSpinner("loading")
	s.Start()
	// Sleep just past one tick (100ms default) so the ticker branch runs.
	time.Sleep(150 * time.Millisecond)
	s.Stop()
}

func TestInitSymbolsTwiceNoChange(t *testing.T) {
	initSymbols()
	initSymbols() // sync.Once means second call is a no-op
}
