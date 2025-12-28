// Package firewall handles Linux firewall detection and configuration.
package firewall

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/shell"
)

// FirewallType represents the type of firewall on the system.
type FirewallType int

const (
	FirewallNone FirewallType = iota
	FirewallUFW
	FirewallFirewalld
	FirewallIPTables
)

// Status represents the firewall status for a port.
type Status struct {
	HTTPOpen  bool
	HTTPSOpen bool
	Firewall  FirewallType
}

// Detect detects which firewall is active on the system.
func Detect() FirewallType {
	// Check for UFW first (common on Ubuntu/Debian)
	if shell.Exists("ufw") && isUFWActive() {
		return FirewallUFW
	}

	// Check for firewalld (common on RHEL/Fedora/CentOS)
	if shell.Exists("firewall-cmd") && isFirewalldActive() {
		return FirewallFirewalld
	}

	// Check for iptables (fallback)
	if shell.Exists("iptables") {
		return FirewallIPTables
	}

	return FirewallNone
}

// Name returns a human-readable name for the firewall type.
func Name(fw FirewallType) string {
	switch fw {
	case FirewallUFW:
		return "ufw"
	case FirewallFirewalld:
		return "firewalld"
	case FirewallIPTables:
		return "iptables"
	default:
		return "none"
	}
}

// isUFWActive checks if UFW is active.
func isUFWActive() bool {
	output, err := exec.Command("ufw", "status").Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(output), constants.UFWActiveStatus)
}

// isFirewalldActive checks if firewalld is running.
func isFirewalldActive() bool {
	output, err := exec.Command("firewall-cmd", "--state").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(output)) == constants.FirewalldRunning
}

// CheckPorts checks if ports 80 and 443 are allowed through the firewall.
func CheckPorts() Status {
	fw := Detect()
	status := Status{Firewall: fw}

	switch fw {
	case FirewallUFW:
		status.HTTPOpen = checkUFWPort(constants.PortHTTPStr)
		status.HTTPSOpen = checkUFWPort(constants.PortHTTPSStr)
	case FirewallFirewalld:
		status.HTTPOpen = checkFirewalldService(constants.SchemeHTTP)
		status.HTTPSOpen = checkFirewalldService(constants.SchemeHTTPS)
	case FirewallIPTables:
		status.HTTPOpen = checkIPTablesPort(constants.PortHTTPStr)
		status.HTTPSOpen = checkIPTablesPort(constants.PortHTTPSStr)
	default:
		// No firewall detected, assume ports are open
		status.HTTPOpen = true
		status.HTTPSOpen = true
	}

	return status
}

// checkUFWPort checks if a port is allowed in UFW.
func checkUFWPort(port string) bool {
	output, err := exec.Command("ufw", "status").Output()
	if err != nil {
		return false
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		// UFW output format examples:
		// "80/tcp                     ALLOW       Anywhere"
		// "443                        ALLOW       Anywhere"
		// "Anywhere                   ALLOW       192.168.1.0/24"
		//
		// We need to check if this specific port is allowed, not just any rule.
		// Look for the port at the start of the line (the "To" column).
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		// The first field is the port/service
		toField := fields[0]

		// Check for exact port match or port/protocol match
		if toField == port || toField == port+"/tcp" || toField == port+"/udp" {
			// Check if this rule ALLOWs traffic
			if len(fields) >= 2 && fields[1] == "ALLOW" {
				return true
			}
		}
	}

	return false
}

// checkFirewalldService checks if a service is allowed in firewalld.
func checkFirewalldService(service string) bool {
	output, err := exec.Command("firewall-cmd", "--list-services").Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(output), service)
}

// checkIPTablesPort checks if a port is allowed in iptables.
func checkIPTablesPort(port string) bool {
	output, err := exec.Command("iptables", "-L", "INPUT", "-n").Output()
	if err != nil {
		return false
	}
	lines := string(output)
	// Check for ACCEPT rules on the port or a general ACCEPT policy
	return strings.Contains(lines, "dpt:"+port) ||
		strings.Contains(lines, "policy ACCEPT")
}

// OpenPorts opens ports 80 and 443 in the firewall.
func OpenPorts() error {
	fw := Detect()

	switch fw {
	case FirewallUFW:
		return openUFWPorts()
	case FirewallFirewalld:
		return openFirewalldPorts()
	case FirewallIPTables:
		return openIPTablesPorts()
	default:
		return nil // No firewall to configure
	}
}

// openUFWPorts opens HTTP and HTTPS ports in UFW.
func openUFWPorts() error {
	// Allow HTTP
	if err := shell.Sudo("ufw", "allow", constants.PortHTTPStr+"/tcp"); err != nil {
		return fmt.Errorf("failed to allow port %s: %w", constants.PortHTTPStr, err)
	}

	// Allow HTTPS
	if err := shell.Sudo("ufw", "allow", constants.PortHTTPSStr+"/tcp"); err != nil {
		return fmt.Errorf("failed to allow port %s: %w", constants.PortHTTPSStr, err)
	}

	return nil
}

// openFirewalldPorts opens HTTP and HTTPS services in firewalld.
func openFirewalldPorts() error {
	// Allow HTTP
	if err := shell.Sudo("firewall-cmd", "--permanent", "--add-service=http"); err != nil {
		return fmt.Errorf("failed to allow http service: %w", err)
	}

	// Allow HTTPS
	if err := shell.Sudo("firewall-cmd", "--permanent", "--add-service=https"); err != nil {
		return fmt.Errorf("failed to allow https service: %w", err)
	}

	// Reload firewall
	if err := shell.Sudo("firewall-cmd", "--reload"); err != nil {
		return fmt.Errorf("failed to reload firewall: %w", err)
	}

	return nil
}

// openIPTablesPorts opens HTTP and HTTPS ports in iptables.
func openIPTablesPorts() error {
	// Allow HTTP
	if err := shell.Sudo("iptables", "-A", "INPUT", "-p", "tcp", "--dport", constants.PortHTTPStr, "-j", "ACCEPT"); err != nil {
		return fmt.Errorf("failed to allow port %s: %w", constants.PortHTTPStr, err)
	}

	// Allow HTTPS
	if err := shell.Sudo("iptables", "-A", "INPUT", "-p", "tcp", "--dport", constants.PortHTTPSStr, "-j", "ACCEPT"); err != nil {
		return fmt.Errorf("failed to allow port %s: %w", constants.PortHTTPSStr, err)
	}

	// Try to persist rules (different methods for different distros)
	persistIPTablesRules()

	return nil
}

// persistIPTablesRules attempts to persist iptables rules.
// This is a best-effort operation - failure is not critical as rules are already applied.
func persistIPTablesRules() {
	// Try iptables-save (Debian/Ubuntu with iptables-persistent)
	if shell.Exists("netfilter-persistent") {
		// Best effort - rules are already applied, persistence is optional
		_ = shell.Sudo("netfilter-persistent", "save") //nolint:errcheck
		return
	}

	// Try saving to /etc/iptables/rules.v4 (Debian/Ubuntu)
	if shell.Exists("iptables-save") {
		// Best effort - rules are already applied, persistence is optional
		_ = exec.Command("sh", "-c", "sudo iptables-save > /etc/iptables/rules.v4").Run() //nolint:errcheck
		return
	}

	// Try service iptables save (RHEL/CentOS without firewalld)
	// Best effort - rules are already applied, persistence is optional
	_ = shell.Sudo("service", "iptables", "save") //nolint:errcheck
}

// IsActive returns true if any firewall is detected and active.
func IsActive() bool {
	return Detect() != FirewallNone
}
