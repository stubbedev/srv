package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
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
	switch runtime.GOOS {
	case "linux":
		return isSystemdInstalled()
	case "darwin":
		return isLaunchdInstalled()
	default:
		return false
	}
}

// Install installs the daemon as a system service.
func Install() error {
	switch runtime.GOOS {
	case "linux":
		return installSystemd()
	case "darwin":
		return installLaunchd()
	default:
		return fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
	}
}

// Uninstall removes the daemon system service.
func Uninstall() error {
	switch runtime.GOOS {
	case "linux":
		return uninstallSystemd()
	case "darwin":
		return uninstallLaunchd()
	default:
		return fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
	}
}

// Restart restarts the daemon service.
func Restart() error {
	switch runtime.GOOS {
	case "linux":
		return restartSystemd()
	case "darwin":
		return restartLaunchd()
	default:
		return fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
	}
}

// =============================================================================
// Systemd (Linux)
// =============================================================================

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

	// Generate service file
	serviceContent := fmt.Sprintf(`[Unit]
Description=srv daemon - Docker container network connector
Documentation=https://github.com/stubbedev/srv
After=docker.service
Wants=docker.service

[Service]
Type=simple
ExecStart=%s daemon start --foreground
Restart=on-failure
RestartSec=5
Environment=HOME=%s
Environment=XDG_CONFIG_HOME=%s/.config

[Install]
WantedBy=default.target
`, executable, os.Getenv("HOME"), os.Getenv("HOME"))

	if err := os.WriteFile(servicePath, []byte(serviceContent), constants.FilePermDefault); err != nil {
		return fmt.Errorf("failed to write service file: %w", err)
	}

	// Reload systemd and enable service
	if err := exec.Command("systemctl", "--user", "daemon-reload").Run(); err != nil {
		return fmt.Errorf("failed to reload systemd: %w", err)
	}

	if err := exec.Command("systemctl", "--user", "enable", SystemdServiceName).Run(); err != nil {
		return fmt.Errorf("failed to enable service: %w", err)
	}

	// Restart if already running, otherwise start
	// Using restart is idempotent - it starts if not running, restarts if running
	if err := exec.Command("systemctl", "--user", "restart", SystemdServiceName).Run(); err != nil {
		return fmt.Errorf("failed to start service: %w", err)
	}

	// Enable lingering so user services run without login
	currentUser, err := user.Current()
	if err == nil {
		exec.Command("loginctl", "enable-linger", currentUser.Username).Run()
	}

	return nil
}

func uninstallSystemd() error {
	servicePath, err := systemdServicePath()
	if err != nil {
		return err
	}

	// Stop and disable service (ignore errors if not running)
	exec.Command("systemctl", "--user", "stop", SystemdServiceName).Run()
	exec.Command("systemctl", "--user", "disable", SystemdServiceName).Run()

	// Remove service file
	if err := os.Remove(servicePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove service file: %w", err)
	}

	// Reload systemd
	exec.Command("systemctl", "--user", "daemon-reload").Run()

	return nil
}

func restartSystemd() error {
	if err := exec.Command("systemctl", "--user", "restart", SystemdServiceName).Run(); err != nil {
		return fmt.Errorf("failed to restart service: %w", err)
	}
	return nil
}

// GetSystemdStatus returns the status of the systemd service.
func GetSystemdStatus() (string, error) {
	output, err := exec.Command("systemctl", "--user", "is-active", SystemdServiceName).Output()
	if err != nil {
		return "inactive", nil
	}
	return strings.TrimSpace(string(output)), nil
}

// =============================================================================
// Launchd (macOS)
// =============================================================================

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

	// Unload existing service first (ignore errors if not loaded)
	exec.Command("launchctl", "unload", plistPath).Run()

	logPath := LogPath(cfg)

	// Generate plist file
	plistContent := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>dev.stubbe.srv-daemon</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>daemon</string>
        <string>start</string>
        <string>--foreground</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <dict>
        <key>SuccessfulExit</key>
        <false/>
    </dict>
    <key>StandardOutPath</key>
    <string>%s</string>
    <key>StandardErrorPath</key>
    <string>%s</string>
    <key>EnvironmentVariables</key>
    <dict>
        <key>PATH</key>
        <string>/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin</string>
    </dict>
</dict>
</plist>
`, executable, logPath, logPath)

	if err := os.WriteFile(plistPath, []byte(plistContent), constants.FilePermDefault); err != nil {
		return fmt.Errorf("failed to write plist file: %w", err)
	}

	// Load the service
	if err := exec.Command("launchctl", "load", plistPath).Run(); err != nil {
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
	exec.Command("launchctl", "unload", plistPath).Run()

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
	exec.Command("launchctl", "unload", plistPath).Run()
	if err := exec.Command("launchctl", "load", plistPath).Run(); err != nil {
		return fmt.Errorf("failed to restart service: %w", err)
	}
	return nil
}

// GetLaunchdStatus returns the status of the launchd service.
func GetLaunchdStatus() (string, error) {
	output, err := exec.Command("launchctl", "list", "dev.stubbe.srv-daemon").Output()
	if err != nil {
		return "not loaded", nil
	}
	if strings.Contains(string(output), "dev.stubbe.srv-daemon") {
		return "running", nil
	}
	return "not loaded", nil
}

// ServicePath returns the path to the service file for the current OS.
func ServicePath() string {
	switch runtime.GOOS {
	case "linux":
		path, _ := systemdServicePath()
		return path
	case "darwin":
		path, _ := launchdPlistPath()
		return path
	default:
		return ""
	}
}

// ServiceStatus returns the status of the daemon service.
func ServiceStatus() (string, error) {
	switch runtime.GOOS {
	case "linux":
		return GetSystemdStatus()
	case "darwin":
		return GetLaunchdStatus()
	default:
		return "unsupported", nil
	}
}
