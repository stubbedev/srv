// Package cmd — dnsd.go implements `srv dnsd`, the standalone form of srv's
// embedded DNS server. The daemon runs the same server in-process; this
// command exists for debugging (`srv dnsd --port 5053` next to a failing
// setup), for tests, and for users who deliberately run without the daemon.
package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/dnsd"
	"github.com/stubbedev/srv/internal/ui"
)

var dnsdFlags struct {
	bind  string
	port  int
	conf  string
	hosts string
}

var dnsdCmd = &cobra.Command{
	Use:    "dnsd",
	Short:  "Run srv's embedded DNS server in the foreground",
	Hidden: true,
	Long: `Runs the DNS server the srv daemon embeds, as a foreground process.

It serves the registered local domains from the generated dnsmasq-format
files and forwards everything else to the upstream resolvers configured
in config.yml (Google DNS by default). The zone files are re-read on
change and on SIGHUP.

The daemon runs this server automatically; use this command only to
debug DNS or to run without the daemon.`,
	RunE: runDNSD,
}

func init() {
	dnsdCmd.Flags().StringVar(&dnsdFlags.bind, "bind", constants.LocalhostIP, "Address to bind")
	dnsdCmd.Flags().IntVar(&dnsdFlags.port, "port", 53, "UDP port to listen on")
	dnsdCmd.Flags().StringVar(&dnsdFlags.conf, "conf", "", "dnsmasq-format conf file (default <traefik-dir>/dnsmasq.conf)")
	dnsdCmd.Flags().StringVar(&dnsdFlags.hosts, "hosts", "", "hosts-format file (default <traefik-dir>/dnsmasq.hosts)")
	RootCmd.AddCommand(dnsdCmd)
}

func runDNSD(cmd *cobra.Command, args []string) error {
	confPath, hostsPath, err := dnsdPaths()
	if err != nil {
		return err
	}

	server, err := dnsd.New(dnsdFlags.bind, dnsdFlags.port, confPath, hostsPath)
	if err != nil {
		if dnsd.IsBindPermissionErr(err) {
			return fmt.Errorf("%w\n  bind a port below 1024 requires privileges: 'sudo sysctl -w net.ipv4.ip_unprivileged_port_start=53' (or run via the daemon service, which has the capability)", err)
		}
		return err
	}

	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
		for sig := range sigChan {
			if sig == syscall.SIGHUP {
				if err := server.Reload(); err != nil {
					ui.Warn("DNS reload failed, keeping previous zones: %v", err)
				}
				continue
			}
			server.Shutdown()
			return
		}
	}()

	go func() {
		if err := server.Watch(); err != nil {
			ui.Warn("DNS file watcher stopped (zones stay as loaded): %v", err)
		}
	}()

	ui.Info("DNS server listening on %s (zones: %s)", server.Addr(), filepath.Dir(confPath))
	if err := server.Serve(); err != nil {
		return fmt.Errorf("DNS server: %w", err)
	}
	return nil
}

// dnsdPaths resolves the zone file paths: explicit flags, else the generated
// files under the traefik dir.
func dnsdPaths() (confPath, hostsPath string, err error) {
	if dnsdFlags.conf != "" && dnsdFlags.hosts != "" {
		return dnsdFlags.conf, dnsdFlags.hosts, nil
	}
	cfg, err := config.Load()
	if err != nil {
		return "", "", fmt.Errorf("resolve zone files: %w", err)
	}
	conf := dnsdFlags.conf
	if conf == "" {
		conf = filepath.Join(cfg.TraefikDir, constants.DnsmasqConfFile)
	}
	hosts := dnsdFlags.hosts
	if hosts == "" {
		hosts = filepath.Join(cfg.TraefikDir, constants.DnsmasqHostsDir, constants.DnsmasqHostsFile)
	}
	return conf, hosts, nil
}
