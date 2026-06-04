package cmd

import (
	"github.com/stubbedev/srv/internal/validate"
)

// The validators live in internal/validate so internal packages can re-use them
// (notably internal/traefik, which re-validates hostnames it reads back from
// hand-editable redirect YAML). These thin wrappers keep the cmd-package call
// sites unchanged.

// ValidateDomain validates a domain/hostname format.
func ValidateDomain(domain string) error { return validate.Domain(domain) }

// ValidatePort validates a port number is within the legal range.
func ValidatePort(port int) error { return validate.Port(port) }

// ValidatePortString validates a port number supplied as a string.
func ValidatePortString(port string) error { return validate.PortString(port) }

// ValidateSiteName validates a site name.
func ValidateSiteName(name string) error { return validate.SiteName(name) }

// ValidateContainerName validates a Docker container or compose service name.
func ValidateContainerName(name string) error { return validate.ContainerName(name) }

// ValidateProxyName validates a proxy name.
func ValidateProxyName(name string) error { return validate.ProxyName(name) }
