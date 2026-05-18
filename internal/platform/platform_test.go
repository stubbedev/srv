package platform

import (
	"runtime"
	"strings"
	"testing"
)

func TestIsLinux(t *testing.T) {
	if got, want := IsLinux(), runtime.GOOS == "linux"; got != want {
		t.Errorf("IsLinux() = %v, want %v", got, want)
	}
}

func TestIsDarwin(t *testing.T) {
	if got, want := IsDarwin(), runtime.GOOS == "darwin"; got != want {
		t.Errorf("IsDarwin() = %v, want %v", got, want)
	}
}

func TestOS(t *testing.T) {
	if OS() != runtime.GOOS {
		t.Errorf("OS() = %q, want %q", OS(), runtime.GOOS)
	}
}

func TestUnsupportedError(t *testing.T) {
	err := UnsupportedError("widget")
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "widget") {
		t.Errorf("error %q missing feature name", msg)
	}
	if !strings.Contains(msg, runtime.GOOS) {
		t.Errorf("error %q missing GOOS", msg)
	}
	if !strings.Contains(msg, "not supported") {
		t.Errorf("error %q missing 'not supported'", msg)
	}
}

func TestExactlyOneOfLinuxOrDarwinOrOther(t *testing.T) {
	count := 0
	if IsLinux() {
		count++
	}
	if IsDarwin() {
		count++
	}
	if count > 1 {
		t.Errorf("IsLinux and IsDarwin both true for GOOS=%q", runtime.GOOS)
	}
}
