package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/daemon"
	"github.com/stubbedev/srv/internal/ui"
)

// =============================================================================
// daemon command (parent)
// =============================================================================

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Manage the srv daemon",
	Long: `The srv daemon watches for Docker container start events and automatically
connects registered site containers to the srv network.

This ensures that containers are properly connected even when started
outside of srv (e.g., via docker compose up directly).`,
}

func init() {
	daemonCmd.GroupID = GroupSystem
	RootCmd.AddCommand(daemonCmd)
}

// =============================================================================
// daemon start command
// =============================================================================

var daemonStartFlags struct {
	foreground bool
}

var daemonStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the srv daemon",
	Long: `Start the srv daemon.

The daemon watches Docker events and automatically connects containers
from registered sites to the srv network when they start.

Use --foreground to run in the foreground (useful for debugging).`,
	RunE: runDaemonStart,
}

func init() {
	daemonStartCmd.Flags().BoolVarP(&daemonStartFlags.foreground, "foreground", "f", false, "Run in foreground (don't daemonize)")
	daemonCmd.AddCommand(daemonStartCmd)
}

func runDaemonStart(cmd *cobra.Command, args []string) error {
	if daemonStartFlags.foreground {
		// Run in foreground (used by systemd/launchd)
		ui.Info("Starting daemon in foreground...")
		d, err := daemon.New()
		if err != nil {
			return err
		}
		return d.Run()
	}

	// For non-foreground, we require the service to be installed
	if !daemon.IsInstalled() {
		ui.Warn("Daemon service is not installed")
		ui.Dim("Run 'srv daemon install' to install the daemon service")
		return nil
	}

	if daemon.IsRunning() {
		ui.Warn("Daemon is already running")
		return nil
	}

	ui.Info("Starting daemon service...")
	if err := daemon.Restart(); err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}

	ui.Success("Daemon started")
	return nil
}

// =============================================================================
// daemon stop command
// =============================================================================

var daemonStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the srv daemon",
	RunE:  runDaemonStop,
}

func init() {
	daemonCmd.AddCommand(daemonStopCmd)
}

func runDaemonStop(cmd *cobra.Command, args []string) error {
	if !daemon.IsInstalled() {
		ui.Dim("Daemon service is not installed")
		return nil
	}

	if !daemon.IsRunning() {
		ui.Dim("Daemon is not running")
		return nil
	}

	ui.Info("Stopping daemon...")
	if err := daemon.Stop(); err != nil {
		return err
	}

	ui.Success("Daemon stopped")
	return nil
}

// =============================================================================
// daemon status command
// =============================================================================

var daemonStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show daemon status",
	RunE:  runDaemonStatus,
}

func init() {
	daemonCmd.AddCommand(daemonStatusCmd)
}

func runDaemonStatus(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Check if installed as service
	if daemon.IsInstalled() {
		status, _ := daemon.ServiceStatus()
		ui.Print("Service:  installed (%s)", status)
		ui.Print("Location: %s", daemon.ServicePath())
	} else {
		ui.Print("Service:  not installed")
	}

	ui.Blank()

	// Check if running via service manager
	if daemon.IsRunning() {
		ui.Success("Daemon is running")
	} else {
		ui.Dim("Daemon is not running")
	}

	ui.Blank()
	ui.Print("Log file: %s", daemon.LogPath(cfg))

	return nil
}

// =============================================================================
// daemon logs command
// =============================================================================

var daemonLogsFlags struct {
	follow bool
	tail   int
}

var daemonLogsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Show daemon logs",
	RunE:  runDaemonLogs,
}

func init() {
	daemonLogsCmd.Flags().BoolVarP(&daemonLogsFlags.follow, "follow", "f", false, "Follow log output")
	daemonLogsCmd.Flags().IntVarP(&daemonLogsFlags.tail, "tail", "n", 50, "Number of lines to show")
	daemonCmd.AddCommand(daemonLogsCmd)
}

func runDaemonLogs(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logPath := daemon.LogPath(cfg)
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		ui.Dim("No log file found")
		return nil
	}

	var tailCmd *exec.Cmd
	if daemonLogsFlags.follow {
		tailCmd = exec.Command("tail", "-f", "-n", fmt.Sprintf("%d", daemonLogsFlags.tail), logPath)
	} else {
		tailCmd = exec.Command("tail", "-n", fmt.Sprintf("%d", daemonLogsFlags.tail), logPath)
	}

	tailCmd.Stdout = os.Stdout
	tailCmd.Stderr = os.Stderr

	return tailCmd.Run()
}

// =============================================================================
// daemon install command
// =============================================================================

var daemonInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install daemon as a system service",
	Long: `Install the srv daemon as a system service that starts automatically.

On Linux, this creates a systemd user service.
On macOS, this creates a launchd agent.

The daemon will start automatically on login and restart if it crashes.`,
	RunE: runDaemonInstall,
}

func init() {
	daemonCmd.AddCommand(daemonInstallCmd)
}

func runDaemonInstall(cmd *cobra.Command, args []string) error {
	if daemon.IsInstalled() {
		ui.Warn("Daemon service is already installed")
		ui.Dim("Use 'srv daemon uninstall' to remove it first")
		return nil
	}

	ui.Info("Installing daemon service...")

	if err := daemon.Install(); err != nil {
		return fmt.Errorf("failed to install daemon service: %w", err)
	}

	ui.Success("Daemon service installed and started")
	ui.Dim("Service file: %s", daemon.ServicePath())
	ui.Dim("The daemon will start automatically on login")

	return nil
}

// =============================================================================
// daemon uninstall command
// =============================================================================

var daemonUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Uninstall daemon system service",
	Long:  `Remove the srv daemon system service. The daemon will no longer start automatically.`,
	RunE:  runDaemonUninstall,
}

func init() {
	daemonCmd.AddCommand(daemonUninstallCmd)
}

func runDaemonUninstall(cmd *cobra.Command, args []string) error {
	if !daemon.IsInstalled() {
		ui.Dim("Daemon service is not installed")
		return nil
	}

	ui.Info("Uninstalling daemon service...")

	if err := daemon.Uninstall(); err != nil {
		return fmt.Errorf("failed to uninstall daemon service: %w", err)
	}

	ui.Success("Daemon service uninstalled")
	return nil
}

// =============================================================================
// daemon restart command
// =============================================================================

var daemonRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the daemon",
	Long:  `Restart the srv daemon service.`,
	RunE:  runDaemonRestart,
}

func init() {
	daemonCmd.AddCommand(daemonRestartCmd)
}

func runDaemonRestart(cmd *cobra.Command, args []string) error {
	if !daemon.IsInstalled() {
		ui.Warn("Daemon service is not installed")
		ui.Dim("Run 'srv daemon install' to install the daemon service")
		return nil
	}

	ui.Info("Restarting daemon service...")
	if err := daemon.Restart(); err != nil {
		return fmt.Errorf("failed to restart daemon service: %w", err)
	}
	ui.Success("Daemon service restarted")
	return nil
}
