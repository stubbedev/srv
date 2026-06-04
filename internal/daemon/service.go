package daemon

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	sd "github.com/sergeymakinen/go-systemdconf/v2"
	"howett.net/plist"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/platform"
	"github.com/stubbedev/srv/internal/shell"
)

// Service file paths
const (
	// SystemdUserDir is the user systemd directory
	SystemdUserDir = ".config/systemd/user"
	// SystemdServiceName is the name of the systemd service
	SystemdServiceName = "srv-daemon.service"
	// LaunchdPlistName is the name of the launchd plist
	LaunchdPlistName = "dev.stubbe.srv-daemon.plist"
	// LaunchAgentsDir is the user LaunchAgents directory
	LaunchAgentsDir = "Library/LaunchAgents"
)

// IsInstalled checks if the daemon service is installed.
func IsInstalled() bool {
	switch {
	case platform.IsLinux():
		return isSystemdInstalled()
	case platform.IsDarwin():
		return isLaunchdInstalled()
	default:
		return false
	}
}

// Install installs the daemon as a system service.
func Install() error {
	switch {
	case platform.IsLinux():
		return installSystemd()
	case platform.IsDarwin():
		return installLaunchd()
	default:
		return platform.UnsupportedError("daemon installation")
	}
}

// Uninstall removes the daemon system service.
func Uninstall() error {
	switch {
	case platform.IsLinux():
		return uninstallSystemd()
	case platform.IsDarwin():
		return uninstallLaunchd()
	default:
		return platform.UnsupportedError("daemon uninstall")
	}
}

// Restart restarts the daemon service.
func Restart() error {
	switch {
	case platform.IsLinux():
		return restartSystemd()
	case platform.IsDarwin():
		return restartLaunchd()
	default:
		return platform.UnsupportedError("daemon restart")
	}
}

// stopService stops the daemon service without uninstalling it.
func stopService() error {
	switch {
	case platform.IsLinux():
		return shell.Run("systemctl", "--user", "stop", SystemdServiceName)
	case platform.IsDarwin():
		plistPath, err := launchdPlistPath()
		if err != nil {
			return err
		}
		return shell.Run("launchctl", "unload", plistPath)
	default:
		return platform.UnsupportedError("daemon stop")
	}
}

// =============================================================================
// Systemd (Linux)
// =============================================================================

// systemdUnitFile models srv-daemon.service. It is marshalled to systemd unit
// syntax by go-systemdconf rather than built as a string, so section/key
// structure and the repeated Environment= entries (which a struct-per-section
// model with unique fields could not express) are handled by the library.
// Field names map directly to systemd key names.
type systemdUnitFile struct {
	sd.File
	Unit struct {
		sd.Section
		Description   sd.Value
		Documentation sd.Value
		After         sd.Value
		Wants         sd.Value
	}
	Service struct {
		sd.Section
		Type        sd.Value
		ExecStart   sd.Value
		Restart     sd.Value
		RestartSec  sd.Value
		Environment sd.Value
	}
	Install struct {
		sd.Section
		WantedBy sd.Value
	}
}

// renderSystemdUnit builds the srv-daemon.service unit. HOME and
// XDG_CONFIG_HOME are pinned because a systemd service context may start with
// an empty environment.
func renderSystemdUnit(executable, homeDir string) (string, error) {
	var u systemdUnitFile
	u.Unit.Description = sd.Value{"srv daemon - Docker container network connector"}
	u.Unit.Documentation = sd.Value{"https://github.com/stubbedev/srv"}
	u.Unit.After = sd.Value{"docker.service"}
	u.Unit.Wants = sd.Value{"docker.service"}
	u.Service.Type = sd.Value{"simple"}
	u.Service.ExecStart = sd.Value{executable + " daemon start --foreground"}
	u.Service.Restart = sd.Value{"on-failure"}
	u.Service.RestartSec = sd.Value{"5"}
	u.Service.Environment = sd.Value{"HOME=" + homeDir, "XDG_CONFIG_HOME=" + homeDir + "/.config"}
	u.Install.WantedBy = sd.Value{"default.target"}

	data, err := sd.Marshal(&u)
	if err != nil {
		return "", fmt.Errorf("marshal systemd unit: %w", err)
	}
	return string(data), nil
}

func systemdServicePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, SystemdUserDir, SystemdServiceName), nil
}

func isSystemdInstalled() bool {
	path, err := systemdServicePath()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

func installSystemd() error {
	// Get paths
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Resolve symlinks to get the real path
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return fmt.Errorf("failed to resolve executable path: %w", err)
	}

	servicePath, err := systemdServicePath()
	if err != nil {
		return err
	}

	// Ensure directory exists
	serviceDir := filepath.Dir(servicePath)
	if err := os.MkdirAll(serviceDir, constants.DirPermDefault); err != nil {
		return fmt.Errorf("failed to create systemd user directory: %w", err)
	}

	// Get the user's home directory reliably (os.Getenv("HOME") can be empty
	// in a service context).
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to determine home directory: %w", err)
	}

	serviceContent, err := renderSystemdUnit(executable, homeDir)
	if err != nil {
		return err
	}

	if err := os.WriteFile(servicePath, []byte(serviceContent), constants.FilePermDefault); err != nil {
		return fmt.Errorf("failed to write service file: %w", err)
	}

	// Reload systemd and enable service
	if err := shell.Run("systemctl", "--user", "daemon-reload"); err != nil {
		return fmt.Errorf("failed to reload systemd: %w", err)
	}

	if err := shell.Run("systemctl", "--user", "enable", SystemdServiceName); err != nil {
		return fmt.Errorf("failed to enable service: %w", err)
	}

	// Restart if already running, otherwise start
	// Using restart is idempotent - it starts if not running, restarts if running
	if err := shell.Run("systemctl", "--user", "restart", SystemdServiceName); err != nil {
		return fmt.Errorf("failed to start service: %w", err)
	}

	// Enable lingering so user services run without login
	currentUser, err := user.Current()
	if err == nil {
		_ = shell.Run("loginctl", "enable-linger", currentUser.Username)
	}

	return nil
}

func uninstallSystemd() error {
	servicePath, err := systemdServicePath()
	if err != nil {
		return err
	}

	// Stop and disable service (ignore errors if not running)
	_ = shell.Run("systemctl", "--user", "stop", SystemdServiceName)
	_ = shell.Run("systemctl", "--user", "disable", SystemdServiceName)

	// Remove service file
	if err := os.Remove(servicePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove service file: %w", err)
	}

	// Reload systemd
	_ = shell.Run("systemctl", "--user", "daemon-reload")

	return nil
}

func restartSystemd() error {
	if err := shell.Run("systemctl", "--user", "restart", SystemdServiceName); err != nil {
		return fmt.Errorf("failed to restart service: %w", err)
	}
	return nil
}

// GetSystemdStatus returns the status of the systemd service.
func GetSystemdStatus() (string, error) {
	output, err := shell.RunQuiet("systemctl", "--user", "is-active", SystemdServiceName)
	if err != nil {
		return "inactive", nil //nolint:nilerr // exit code from systemctl signals state, not a failure to query
	}
	return strings.TrimSpace(string(output)), nil
}

// =============================================================================
// Launchd (macOS)
// =============================================================================

// launchdPlist models the LaunchAgent property list. It is marshalled to XML
// by howett.net/plist rather than built as a string, so the document structure
// and escaping are handled by the library. Struct field order is preserved in
// the output; the two nested dicts have a single key each, so map ordering is
// deterministic.
type launchdPlist struct {
	Label                string            `plist:"Label"`
	ProgramArguments     []string          `plist:"ProgramArguments"`
	RunAtLoad            bool              `plist:"RunAtLoad"`
	KeepAlive            map[string]bool   `plist:"KeepAlive"`
	StandardOutPath      string            `plist:"StandardOutPath"`
	StandardErrorPath    string            `plist:"StandardErrorPath"`
	EnvironmentVariables map[string]string `plist:"EnvironmentVariables"`
}

// renderLaunchdPlist builds the LaunchAgent plist XML. The daemon is kept alive
// across non-zero exits (KeepAlive/SuccessfulExit=false) and runs with a PATH
// that includes the binary's own dir plus the usual Homebrew/system locations.
func renderLaunchdPlist(executable, logPath string) (string, error) {
	doc := launchdPlist{
		Label:             "dev.stubbe.srv-daemon",
		ProgramArguments:  []string{executable, "daemon", "start", "--foreground"},
		RunAtLoad:         true,
		KeepAlive:         map[string]bool{"SuccessfulExit": false},
		StandardOutPath:   logPath,
		StandardErrorPath: logPath,
		EnvironmentVariables: map[string]string{
			"PATH": filepath.Dir(executable) + ":/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin",
		},
	}
	data, err := plist.MarshalIndent(doc, plist.XMLFormat, "    ")
	if err != nil {
		return "", fmt.Errorf("marshal launchd plist: %w", err)
	}
	return string(data) + "\n", nil
}

func launchdPlistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, LaunchAgentsDir, LaunchdPlistName), nil
}

func isLaunchdInstalled() bool {
	path, err := launchdPlistPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

func installLaunchd() error {
	// Get paths
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Resolve symlinks to get the real path
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return fmt.Errorf("failed to resolve executable path: %w", err)
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	plistPath, err := launchdPlistPath()
	if err != nil {
		return err
	}

	// Ensure directory exists
	plistDir := filepath.Dir(plistPath)
	if err := os.MkdirAll(plistDir, constants.DirPermDefault); err != nil {
		return fmt.Errorf("failed to create LaunchAgents directory: %w", err)
	}

	logPath := LogPath(cfg)

	plistContent, err := renderLaunchdPlist(executable, logPath)
	if err != nil {
		return err
	}

	// Write plist before unloading so a write failure leaves the old service intact.
	if err := os.WriteFile(plistPath, []byte(plistContent), constants.FilePermACME); err != nil {
		return fmt.Errorf("failed to write plist file: %w", err)
	}

	// Unload any previously running instance (ignore errors if not loaded)
	_ = shell.Run("launchctl", "unload", plistPath)

	// Load the service
	if err := shell.Run("launchctl", "load", plistPath); err != nil {
		return fmt.Errorf("failed to load service: %w", err)
	}

	return nil
}

func uninstallLaunchd() error {
	plistPath, err := launchdPlistPath()
	if err != nil {
		return err
	}

	// Unload service (ignore errors if not loaded)
	_ = shell.Run("launchctl", "unload", plistPath)

	// Remove plist file
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove plist file: %w", err)
	}

	return nil
}

func restartLaunchd() error {
	plistPath, err := launchdPlistPath()
	if err != nil {
		return err
	}

	// Unload and reload
	_ = shell.Run("launchctl", "unload", plistPath)
	if err := shell.Run("launchctl", "load", plistPath); err != nil {
		return fmt.Errorf("failed to restart service: %w", err)
	}
	return nil
}

// GetLaunchdStatus returns the status of the launchd service.
func GetLaunchdStatus() (string, error) {
	output, err := shell.RunQuiet("launchctl", "list", "dev.stubbe.srv-daemon")
	if err != nil {
		return "not loaded", nil //nolint:nilerr // launchctl errors when the service isn't loaded
	}
	// A loaded-but-stopped service appears in launchctl list without a PID entry.
	// Only report "running" when the process is actually alive.
	if strings.Contains(string(output), `"PID" = `) {
		return "running", nil
	}
	return "loaded", nil
}

// ServicePath returns the path to the service file for the current OS.
// Returns an error on unsupported platforms or when the home directory
// cannot be resolved — callers should not blindly pass an empty string to os.Stat.
func ServicePath() (string, error) {
	switch {
	case platform.IsLinux():
		return systemdServicePath()
	case platform.IsDarwin():
		return launchdPlistPath()
	default:
		return "", platform.UnsupportedError("service management")
	}
}

// ServiceStatus returns the status of the daemon service.
func ServiceStatus() (string, error) {
	switch {
	case platform.IsLinux():
		return GetSystemdStatus()
	case platform.IsDarwin():
		return GetLaunchdStatus()
	default:
		return "unsupported", nil
	}
}
