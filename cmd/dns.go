package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/stubbedev/srv/internal/traefik"
	"github.com/stubbedev/srv/internal/ui"
)

// =============================================================================
// dns command
// =============================================================================

var dnsCmd = &cobra.Command{
	Use:   "dns [command]",
	Short: "Manage local DNS for *.test domains",
	Long: `Manage the local DNS server that resolves *.test, *.local, and *.localhost
domains to 127.0.0.1, eliminating the need to edit /etc/hosts.

Without a subcommand, shows current DNS status.`,
	RunE: runDNSStatus,
}

var dnsSetupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Configure system to use local DNS",
	Long: `Configure your system's DNS resolver to use the local DNS server
for *.test, *.local, and *.localhost domains.

This command requires sudo privileges to modify system DNS configuration.`,
	RunE: runDNSSetup,
}

var dnsRemoveCmd = &cobra.Command{
	Use:   "remove",
	Short: "Remove local DNS configuration",
	Long:  `Remove the DNS configuration that was set up by 'srv dns setup'.`,
	RunE:  runDNSRemove,
}

func init() {
	dnsCmd.AddCommand(dnsSetupCmd)
	dnsCmd.AddCommand(dnsRemoveCmd)
	RootCmd.AddCommand(dnsCmd)
}

func runDNSStatus(cmd *cobra.Command, args []string) error {
	// Check if DNS container is running
	if traefik.IsDNSRunning() {
		ui.Success("DNS server is running (srv-dns)")
	} else {
		ui.Warn("DNS server is not running")
		ui.Dim("Run 'srv init' to start the DNS server")
		ui.Blank()
	}

	// Check if system DNS is configured
	if traefik.CheckSystemDNS() {
		ui.Success("System DNS is configured")
		ui.Dim("*.test, *.local, *.localhost %s 127.0.0.1", ui.SymbolArrow)
		return nil
	}

	// Check if local DNS server responds
	if traefik.CheckDNS() {
		ui.Warn("DNS server works but system is not configured to use it")
		ui.Dim("Resolver: %s", traefik.GetResolverName())
		ui.Blank()
		ui.Info("Run 'srv dns setup' to configure automatically")
	} else {
		ui.Warn("DNS is not configured")
		ui.Dim("Run 'srv init' first, then 'srv dns setup'")
	}

	return nil
}

func runDNSSetup(cmd *cobra.Command, args []string) error {
	// Check if DNS container is running
	if !traefik.IsDNSRunning() {
		return fmt.Errorf("DNS server is not running. Run 'srv init' first")
	}

	ui.Info("Configuring system DNS (%s)...", traefik.GetResolverName())
	ui.Dim("This may require your sudo password")
	ui.Blank()

	if err := traefik.SetupDNS(); err != nil {
		return err
	}

	// Verify it worked
	if traefik.CheckSystemDNS() {
		ui.Success("DNS configured successfully!")
		ui.Dim("*.test, *.local, *.localhost %s 127.0.0.1", ui.SymbolArrow)
	} else {
		ui.Warn("Configuration was applied but DNS resolution not yet working")
		ui.Dim("You may need to restart your browser or wait a few seconds")
	}

	return nil
}

func runDNSRemove(cmd *cobra.Command, args []string) error {
	ui.Info("Removing DNS configuration...")

	if err := traefik.RemoveDNS(); err != nil {
		return err
	}

	ui.Success("DNS configuration removed")
	return nil
}
