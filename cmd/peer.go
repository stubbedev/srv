package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/ops"
	"github.com/stubbedev/srv/internal/peer"
	"github.com/stubbedev/srv/internal/ui"
)

const peerDialTimeout = 10 * time.Second

var peerCmd = &cobra.Command{
	Use:   "peer",
	Short: "Find other srv machines on the LAN by name",
	Long: `Peer discovery lets srv machines on the same LAN reach each other by name.

Set peer_name in config.yml and the daemon announces this machine over
UDP broadcast on port ` + constants.PortPeerStr + `, answers every announce it
hears, and serves each peer it has heard from as <name>.` + peer.Zone + ` through
srv's DNS. Broadcast is used instead of mDNS multicast because Wi-Fi access
points that snoop IGMP silently stop delivering multicast after a roam.`,
}

var peerRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Announce and discover peers in the foreground, without the daemon",
	Long: `Runs peer discovery as a foreground process, for machines that do not run
the srv daemon (no container runtime, no service manager). It announces
peer_name from config.yml and keeps the peer table that 'srv peer list' and
'srv peer dial' read.`,
	Args: cobra.NoArgs,
	RunE: runPeerRun,
}

var peerListCmd = &cobra.Command{
	Use:   "list",
	Short: "List the peers heard on the LAN",
	Args:  cobra.NoArgs,
	RunE:  runPeerList,
}

var peerDialCmd = &cobra.Command{
	Use:   "dial <name> <port>",
	Short: "Connect stdin/stdout to a peer's TCP port (an ssh ProxyCommand)",
	Long: `Connects stdin and stdout to a TCP port on a peer, resolved from the peer
table instead of DNS. Use it as an ssh ProxyCommand on machines whose resolver
cannot be pointed at srv's DNS:

  ProxyCommand srv peer dial %h %p`,
	Args: cobra.ExactArgs(2),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return peerNames(), cobra.ShellCompDirectiveNoFileComp
	},
	RunE: runPeerDial,
}

func init() {
	peerCmd.GroupID = GroupSystem
	peerCmd.AddCommand(peerRunCmd, peerListCmd, peerDialCmd)
	RootCmd.AddCommand(peerCmd)
}

func runPeerRun(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	userCfg, err := ops.UserConfig()
	if err != nil {
		return err
	}
	if userCfg.PeerName == "" {
		return fmt.Errorf("peer_name is not set in %s", cfg.ConfigPath())
	}
	node, err := peer.Listen(userCfg.PeerName, constants.PortPeer, cfg.PeersPath(), func([]peer.Entry) {})
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ui.Info("Announcing %s on udp port %d", userCfg.PeerName.FQDN(), constants.PortPeer)
	return node.Run(ctx)
}

func runPeerList(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	entries, err := peer.Load(cfg.PeersPath(), now)
	if err != nil {
		return err
	}
	if jsonOutput() {
		return ui.PrintJSON(entries)
	}
	if len(entries) == 0 {
		ui.Dim("No peers heard in the last %s", peer.Expiry)
		return nil
	}
	rows := make([][]string, 0, len(entries))
	for _, e := range entries {
		rows = append(rows, []string{e.Name.FQDN(), e.Addr.String(), now.Sub(e.Seen).Truncate(time.Second).String() + " ago"})
	}
	ui.PrintTable([]string{"NAME", "ADDRESS", "LAST SEEN"}, rows)
	return nil
}

func runPeerDial(cmd *cobra.Command, args []string) error {
	name, err := peer.NameFromHost(args[0])
	if err != nil {
		return err
	}
	port, err := strconv.ParseUint(args[1], 10, 16)
	if err != nil || port == 0 {
		return fmt.Errorf("dial peer %s: %q is not a TCP port", name, args[1])
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	addr, err := peer.Resolve(cfg.PeersPath(), name, time.Now().UTC())
	if err != nil {
		return err
	}
	target := net.JoinHostPort(addr.String(), args[1])
	ctx, cancel := context.WithTimeout(context.Background(), peerDialTimeout)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", target)
	if err != nil {
		return fmt.Errorf("dial peer %s at %s: %w", name, target, err)
	}
	tcp, ok := conn.(*net.TCPConn)
	if !ok {
		_ = conn.Close()
		return fmt.Errorf("dial peer %s at %s: got %T, want *net.TCPConn", name, target, conn)
	}
	defer func() { _ = tcp.Close() }()
	return relay(tcp, os.Stdin, os.Stdout)
}

func relay(conn *net.TCPConn, in io.Reader, out io.Writer) error {
	sent := make(chan error, 1)
	go func() {
		_, err := io.Copy(conn, in)
		if cwErr := conn.CloseWrite(); err == nil && cwErr != nil && !errors.Is(cwErr, net.ErrClosed) {
			err = cwErr
		}
		sent <- err
	}()
	if _, err := io.Copy(out, conn); err != nil {
		return fmt.Errorf("relay from %s: %w", conn.RemoteAddr(), err)
	}
	select {
	case err := <-sent:
		if err != nil {
			return fmt.Errorf("relay to %s: %w", conn.RemoteAddr(), err)
		}
	default:
	}
	return nil
}

func peerNames() []string {
	cfg, err := config.Load()
	if err != nil {
		return nil
	}
	entries, err := peer.Load(cfg.PeersPath(), time.Now().UTC())
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, string(e.Name))
	}
	return names
}
