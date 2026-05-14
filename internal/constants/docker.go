package constants

// =============================================================================
// Docker Status Formats
// =============================================================================

const (
	// ComposeStatusFormat is the format string for docker compose status.
	ComposeStatusFormat = "{{.Status}}"
	// InspectRunningFormat is the format string for inspecting container running state.
	InspectRunningFormat = "{{.State.Running}}"
	// TrueString is the string representation of true.
	TrueString = "true"
)

// =============================================================================
// Site Types
// =============================================================================

const (
	// SiteTypeCompose is the Docker compose site type.
	SiteTypeCompose = "compose"
	// SiteTypeStatic is the static site type.
	SiteTypeStatic = "static"
	// SiteTypePHP is the PHP/FPM site type.
	SiteTypePHP = "php"
	// SiteTypeNode is the Node.js site type.
	SiteTypeNode = "node"
	// SiteTypeRuby is the Ruby site type.
	SiteTypeRuby = "ruby"
	// SiteTypePython is the Python site type.
	SiteTypePython = "python"
	// SiteTypeDockerfile is the Dockerfile site type.
	SiteTypeDockerfile = "dockerfile"
)

// =============================================================================
// Proxy Types
// =============================================================================

const (
	// ProxyTypeLocalhost indicates proxy to localhost.
	ProxyTypeLocalhost = "localhost"
	// ProxyTypeContainer indicates proxy to a container.
	ProxyTypeContainer = "container"
)

// =============================================================================
// Static Site Container Constants
// =============================================================================

const (
	// StaticContainerPrefix is the prefix for static site container names.
	StaticContainerPrefix = "srv_static_"
	// StaticContainerHashLength is the hash length in static container names.
	StaticContainerHashLength = 8
)
