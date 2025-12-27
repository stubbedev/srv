package cmd

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
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
	if len(domain) > 253 {
		return fmt.Errorf("domain is too long (max 253 characters)")
	}

	// Check for valid domain format
	if !domainRegex.MatchString(domain) {
		return fmt.Errorf("invalid domain format: %s (use alphanumeric characters, hyphens, and dots)", domain)
	}

	// Check individual label lengths (max 63 characters each)
	labels := strings.Split(domain, ".")
	for _, label := range labels {
		if len(label) > 63 {
			return fmt.Errorf("domain label '%s' is too long (max 63 characters)", label)
		}
	}

	return nil
}

// ValidatePort validates a port number.
// Returns an error if the port is invalid (not 1-65535).
func ValidatePort(port string) error {
	if port == "" {
		return fmt.Errorf("port cannot be empty")
	}

	portNum, err := strconv.Atoi(port)
	if err != nil {
		return fmt.Errorf("invalid port: %s (must be a number)", port)
	}

	if portNum < 1 || portNum > 65535 {
		return fmt.Errorf("port %d is out of range (must be 1-65535)", portNum)
	}

	return nil
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
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("invalid URL scheme: %s (must be http or https)", parsed.Scheme)
	}

	// Check host is present
	if parsed.Host == "" {
		return fmt.Errorf("URL must include a host")
	}

	// If port is specified in URL, validate it
	if parsed.Port() != "" {
		if err := ValidatePort(parsed.Port()); err != nil {
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

	if len(name) > 63 {
		return fmt.Errorf("site name is too long (max 63 characters)")
	}

	return nil
}
