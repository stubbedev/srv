package cmd

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/stubbedev/srv/internal/constants"
)

// Pre-compiled validation regexes
var (
	// Domain validation regex
	// Matches valid hostnames: alphanumeric, hyphens, dots
	// Examples: example.com, my-app.test, sub.domain.local
	domainRegex = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$`)

	// Site name validation regex
	// Site names should be alphanumeric with hyphens and underscores
	siteNameRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)
)

// ValidateDomain validates a domain/hostname format.
// Returns an error if the domain is invalid.
func ValidateDomain(domain string) error {
	if domain == "" {
		return fmt.Errorf("domain cannot be empty")
	}

	// Check length (max 253 characters for full domain)
	if len(domain) > constants.MaxDomainLength {
		return fmt.Errorf("domain is too long (max %d characters)", constants.MaxDomainLength)
	}

	// Check for valid domain format
	if !domainRegex.MatchString(domain) {
		return fmt.Errorf("invalid domain format: %s (use alphanumeric characters, hyphens, and dots)", domain)
	}

	// Check individual label lengths (max 63 characters each)
	labels := strings.Split(domain, ".")
	for _, label := range labels {
		if len(label) > constants.MaxDomainLabelLength {
			return fmt.Errorf("domain label '%s' is too long (max %d characters)", label, constants.MaxDomainLabelLength)
		}
	}

	return nil
}

// ValidatePort validates a port number.
// Returns an error if the port is invalid (not 1-65535).
func ValidatePort(port int) error {
	if port < constants.PortMin || port > constants.PortMax {
		return fmt.Errorf("port %d is out of range (must be %d-%d)", port, constants.PortMin, constants.PortMax)
	}

	return nil
}

// ValidatePortString validates a port number from a string.
// Returns an error if the port is invalid (not 1-65535).
func ValidatePortString(port string) error {
	if port == "" {
		return fmt.Errorf("port cannot be empty")
	}

	portNum, err := strconv.Atoi(port)
	if err != nil {
		return fmt.Errorf("invalid port: %s (must be a number)", port)
	}

	return ValidatePort(portNum)
}

// ValidateProxyURL validates a proxy target URL.
// Returns an error if the URL is invalid or uses an unsupported scheme.
func ValidateProxyURL(targetURL string) error {
	if targetURL == "" {
		return fmt.Errorf("URL cannot be empty")
	}

	parsed, err := url.Parse(targetURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Check scheme
	if parsed.Scheme != constants.SchemeHTTP && parsed.Scheme != constants.SchemeHTTPS {
		return fmt.Errorf("invalid URL scheme: %s (must be %s or %s)", parsed.Scheme, constants.SchemeHTTP, constants.SchemeHTTPS)
	}

	// Check host is present
	if parsed.Host == "" {
		return fmt.Errorf("URL must include a host")
	}

	// If port is specified in URL, validate it
	if parsed.Port() != "" {
		if err := ValidatePortString(parsed.Port()); err != nil {
			return fmt.Errorf("invalid port in URL: %w", err)
		}
	}

	return nil
}

// ValidateSiteName validates a site name.
// Returns an error if the name contains invalid characters.
func ValidateSiteName(name string) error {
	if name == "" {
		return fmt.Errorf("site name cannot be empty")
	}

	if !siteNameRegex.MatchString(name) {
		return fmt.Errorf("invalid site name: %s (use alphanumeric characters, hyphens, and underscores)", name)
	}

	if len(name) > constants.MaxSiteNameLength {
		return fmt.Errorf("site name is too long (max %d characters)", constants.MaxSiteNameLength)
	}

	return nil
}
