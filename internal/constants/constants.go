// Package constants provides shared constants used across the srv application.
//
// Constants are organised by domain:
//   - constants.go — app metadata, exit codes, status, timeouts, UI, defaults
//   - network.go   — IPs, ports, schemes, validation limits
//   - files.go     — file permissions, names, directories, env vars, system paths
//   - traefik.go   — Traefik, certificates, DNS, mkcert, firewall, SSL types
//   - docker.go    — Docker status, site types, proxy types, static containers
//   - runtime.go   — PHP, Node, Nginx, Ruby, Python, Dockerfile
package constants

import "time"

// =============================================================================
// Application Metadata
// =============================================================================

const (
	// AppName is the name of the application.
	AppName = "srv"
	// DefaultVersion is the default version when not set by build.
	DefaultVersion = "dev"
	// DefaultCommit is the default commit hash when not set by build.
	DefaultCommit = "none"
	// DefaultBuildDate is the default build date when not set by build.
	DefaultBuildDate = "unknown"
)

// =============================================================================
// Exit Codes
// =============================================================================

const (
	// ExitCodeSuccess indicates successful execution.
	ExitCodeSuccess = 0
	// ExitCodeError indicates an error occurred.
	ExitCodeError = 1
)

// =============================================================================
// Status Strings
// =============================================================================

const (
	// StatusRunning indicates a container/service is running.
	StatusRunning = "running"
	// StatusStopped indicates a container/service is stopped.
	StatusStopped = "stopped"
	// StatusBroken indicates a site is broken.
	StatusBroken = "broken"
	// StatusPartial indicates partial status.
	StatusPartial = "partial"
)

// Container status strings.
const (
	// StatusPrefixUp is the prefix for running container status.
	StatusPrefixUp = "Up"
)

// =============================================================================
// Date Formats
// =============================================================================

const (
	// DateFormat is the standard date format.
	DateFormat = "2006-01-02"
)

// =============================================================================
// Timeout Constants
// =============================================================================

const (
	// SpinnerTimeout is the maximum duration for a spinner before auto-stop.
	SpinnerTimeout = 10 * time.Minute
	// SpinnerInterval is the animation interval for spinners.
	SpinnerInterval = 100 * time.Millisecond
)

// =============================================================================
// Batch Operation Constants
// =============================================================================

const (
	// MaxWorkers is the maximum number of parallel workers for batch operations.
	MaxWorkers = 4
	// MaxStatusWorkers is the maximum number of workers for status checks.
	MaxStatusWorkers = 8
)

// =============================================================================
// UI Constants
// =============================================================================

const (
	// IndentString is the standard indentation string.
	IndentString = "  "
	// TableSeparator is the Unicode table separator character.
	TableSeparator = "─"
	// AccessLogBufferSize is the buffer size for access logs.
	AccessLogBufferSize = 100
)

// =============================================================================
// Random String Constants
// =============================================================================

const (
	// DNSUserLength is the length of the DNS HTTP user string.
	DNSUserLength = 16
	// DNSPassLength is the length of the DNS HTTP password string.
	DNSPassLength = 32
)

// =============================================================================
// Default Values
// =============================================================================

const (
	// DefaultHostname is the default hostname when hostname cannot be determined.
	DefaultHostname = "default"
	// DefaultContainerPort is the default container port.
	DefaultContainerPort = 80
)

// =============================================================================
// Initialization Steps
// =============================================================================

const (
	// InitBaseSteps is the base number of initialization steps.
	// network + config + start traefik + dashboard proxy
	InitBaseSteps = 4
)
