// Package cmd — httpd.go implements `srv httpd`, the standalone form of srv's
// embedded static file server. The daemon runs the same server in-process;
// this command exists for debugging daemon-served sites and for tests, the
// way `srv dnsd` does for the DNS server.
package cmd

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/httpd"
	"github.com/stubbedev/srv/internal/ui"
)

var httpdFlags struct {
	bind string
	port int
}

var httpdCmd = &cobra.Command{
	Use:    "httpd",
	Short:  "Run srv's embedded static file server in the foreground",
	Hidden: true,
	Long: `Runs the static file server the srv daemon embeds, as a foreground process.

It serves every daemon-served static site ("srv add --daemon"), multiplexed
by Host header from the site metadata. The host table is re-read on SIGHUP.

The daemon runs this server automatically; use this command only to debug
daemon-served sites or to run without the daemon.`,
	RunE: runHTTPD,
}

func init() {
	httpdCmd.Flags().StringVar(&httpdFlags.bind, "bind", constants.LocalhostIP, "Address to bind")
	httpdCmd.Flags().IntVar(&httpdFlags.port, "port", constants.PortStatic, "TCP port to listen on (the daemon serves this port too)")
	RootCmd.AddCommand(httpdCmd)
}

func runHTTPD(cmd *cobra.Command, args []string) error {
	server := httpd.New(net.JoinHostPort(httpdFlags.bind, strconv.Itoa(httpdFlags.port)))
	if err := server.Reload(); err != nil {
		return fmt.Errorf("load daemon-served sites: %w", err)
	}

	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
		for sig := range sigChan {
			if sig == syscall.SIGHUP {
				if err := server.Reload(); err != nil {
					ui.Warn("Static server reload failed, keeping previous sites: %v", err)
				}
				continue
			}
			server.Shutdown()
			return
		}
	}()

	ui.Info("Static file server listening on %s", server.Addr())
	if err := server.Serve(); err != nil {
		return fmt.Errorf("static file server: %w", err)
	}
	return nil
}
