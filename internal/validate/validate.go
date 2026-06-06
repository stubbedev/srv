// Package validate holds the input validators shared between the CLI (cmd) and
// the internal packages that consume hand-editable config files. Keeping them
// here — rather than in cmd — lets internal/traefik re-validate values it reads
// back from YAML on disk (where the CLI boundary check no longer protects it)
// without an import cycle.
package validate

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/stubbedev/srv/internal/constants"
)

// Pre-compiled validation regexes.
var (
	// domainRegex matches valid hostnames: alphanumeric labels separated by
	// dots, hyphens allowed internally. Examples: example.com, my-app.test.
	domainRegex = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$`)

	// siteNameRegex matches site names: alphanumeric with hyphens and underscores.
	siteNameRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

	// containerNameRegex matches Docker container/compose service names:
	// alphanumeric, underscores, hyphens, periods.
	containerNameRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)
)

// Domain validates a domain/hostname format, returning an error if invalid.
// Beyond format, it rejects empty input, over-length domains, and over-length
// labels. Critically it also blocks anything that could break out of a
// single-token context (whitespace, slashes, dnsmasq `address=/.../` directives)
// because callers feed validated domains straight into dnsmasq and Traefik
// config lines.
func Domain(domain string) error {
	if domain == "" {
		return fmt.Errorf("domain cannot be empty")
	}
	if len(domain) > constants.MaxDomainLength {
		return fmt.Errorf("domain is too long (max %d characters)", constants.MaxDomainLength)
	}
	if !domainRegex.MatchString(domain) {
		return fmt.Errorf("invalid domain format: %s (use alphanumeric characters, hyphens, and dots)", domain)
	}
	for label := range strings.SplitSeq(domain, ".") {
		if len(label) > constants.MaxDomainLabelLength {
			return fmt.Errorf("domain label '%s' is too long (max %d characters)", label, constants.MaxDomainLabelLength)
		}
	}
	return nil
}

// NoTraversal rejects a string that could escape a directory when used as a
// path element: empty, containing a path separator, or a ".." component. It is
// a belt-and-suspenders guard for synthetic identifiers (e.g. the "_proxy-foo"
// cert site names) that do not pass the stricter SiteName check but are still
// joined into filesystem paths.
func NoTraversal(s string) error {
	if s == "" {
		return fmt.Errorf("path element cannot be empty")
	}
	if strings.ContainsAny(s, `/\`) || s == ".." || strings.Contains(s, "..") {
		return fmt.Errorf("path element %q contains illegal path characters", s)
	}
	return nil
}

// Port validates a port number is within the legal 1-65535 range.
func Port(port int) error {
	if port < constants.PortMin || port > constants.PortMax {
		return fmt.Errorf("port %d is out of range (must be %d-%d)", port, constants.PortMin, constants.PortMax)
	}
	return nil
}

// PortString validates a port number supplied as a string.
func PortString(port string) error {
	if port == "" {
		return fmt.Errorf("port cannot be empty")
	}
	portNum, err := strconv.Atoi(port)
	if err != nil {
		return fmt.Errorf("invalid port: %s (must be a number)", port)
	}
	return Port(portNum)
}

// SiteName validates a site name.
func SiteName(name string) error {
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

// ContainerName validates a Docker container or compose service name.
func ContainerName(name string) error {
	if name == "" {
		return fmt.Errorf("container name cannot be empty")
	}
	if !containerNameRegex.MatchString(name) {
		return fmt.Errorf("invalid container name: %s (use alphanumeric characters, hyphens, underscores, and periods)", name)
	}
	return nil
}

// ProxyName validates a proxy name. Proxy names may contain periods because
// they are often derived from domain names (e.g. "myapp.com").
func ProxyName(name string) error {
	if name == "" {
		return fmt.Errorf("proxy name cannot be empty")
	}
	if !domainRegex.MatchString(name) {
		return fmt.Errorf("invalid proxy name: %s (use alphanumeric characters, hyphens, periods, and underscores)", name)
	}
	if len(name) > constants.MaxDomainLength {
		return fmt.Errorf("proxy name is too long (max %d characters)", constants.MaxDomainLength)
	}
	return nil
}
