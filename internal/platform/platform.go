// Package platform centralises the runtime.GOOS branches that srv needs.
//
// Most callers care about three questions: "am I on Linux?", "am I on macOS?",
// and "what should I tell the user when neither applies?" — the helpers below
// answer those without leaking runtime.GOOS into business logic.
package platform

import (
	"fmt"
	"runtime"
)

// IsLinux reports whether srv is running on Linux.
func IsLinux() bool {
	return runtime.GOOS == "linux"
}

// IsDarwin reports whether srv is running on macOS.
func IsDarwin() bool {
	return runtime.GOOS == "darwin"
}

// OS returns the current operating system identifier (runtime.GOOS).
// Use this when you need the raw value for an error message or log line.
func OS() string {
	return runtime.GOOS
}

// UnsupportedError returns a uniform "operation X is not supported on this OS"
// error. Pass the name of the feature that the platform cannot provide.
func UnsupportedError(feature string) error {
	return fmt.Errorf("%s is not supported on %s", feature, runtime.GOOS)
}
