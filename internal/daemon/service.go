package daemon

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strings"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/platform"
	"github.com/stubbedev/srv/internal/shell"
)

// Service file paths.
const (
	// SystemdUserDir is the user systemd directory.
	SystemdUserDir = ".config/systemd/user"
	// SystemdServiceName is the name of the systemd service.
	SystemdServiceName = "srv-daemon.service"
	// LaunchdLabel is the label of the launchd agent.
	LaunchdLabel = "dev.stubbe.srv-daemon"
	// LaunchdPlistName is the name of the launchd plist.
	LaunchdPlistName = LaunchdLabel + ".plist"
	// LaunchAgentsDir is the user LaunchAgents directory.
	LaunchAgentsDir = "Library/LaunchAgents"
	// BrewSystemdServiceName is the systemd unit `brew services start srv`
	// writes on Linux.
	BrewSystemdServiceName = "homebrew.srv.service"
	// BrewLaunchdLabel is the launchd label `brew services start srv` writes
	// on macOS.
	BrewLaunchdLabel = "homebrew.mxcl.srv"
)

// Supervisor is the service-manager unit that runs the daemon. srv writes its
// own unit (`srv install`, `srv daemon install`); the Homebrew formula
// declares another (`brew services start srv`). Both run the same daemon, so
// exactly one may be installed: two would race over the single-instance lock.
type Supervisor int

const (
	// SupervisorNone means no unit is installed.
	SupervisorNone Supervisor = iota
	// SupervisorSrv is the unit srv installs itself.
	SupervisorSrv
	// SupervisorBrew is the unit `brew services start srv` installs.
	SupervisorBrew
)

func (s Supervisor) String() string {
	switch s {
	case SupervisorSrv:
		return "srv"
	case SupervisorBrew:
		return "brew services"
	default:
		return "none"
	}
}

// ErrSupervisedByBrew reports that `brew services` owns the daemon, so srv
// must not install or remove a unit of its own in its place.
var ErrSupervisedByBrew = errors.New("the srv daemon runs under 'brew services'")

// DoubleSupervisedError reports that both srv's unit and the brew services
// unit are installed. Each starts the daemon, and the loser crash-loops
// against the single-instance lock.
type DoubleSupervisedError struct {
	SrvUnit  string
	BrewUnit string
}

func (e *DoubleSupervisedError) Error() string {
	return fmt.Sprintf("the srv daemon is installed twice, by srv (%s) and by brew services (%s), "+
		"and the two race over the same daemon: remove one with 'srv daemon uninstall' or 'brew services stop srv'",
		e.SrvUnit, e.BrewUnit)
}

// unit identifies one service-manager unit: its file, and the name systemd
// or the label launchd knows it by.
type unit struct {
	path string
	name string
}

func (u unit) installed() bool {
	_, err := os.Stat(u.path)
	return err == nil
}

// unitFor returns the unit of supervisor s on this platform.
func unitFor(s Supervisor) (unit, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return unit{}, fmt.Errorf("determine home directory: %w", err)
	}
	brew := s == SupervisorBrew
	switch {
	case platform.IsLinux():
		name := SystemdServiceName
		if brew {
			name = BrewSystemdServiceName
		}
		return unit{path: filepath.Join(home, SystemdUserDir, name), name: name}, nil
	case platform.IsDarwin():
		label := LaunchdLabel
		if brew {
			label = BrewLaunchdLabel
		}
		return unit{path: filepath.Join(home, LaunchAgentsDir, label+".plist"), name: label}, nil
	default:
		return unit{}, platform.UnsupportedError("service management")
	}
}

// ActiveSupervisor reports which unit runs the daemon. It returns a
// *DoubleSupervisedError when both are installed.
func ActiveSupervisor() (Supervisor, error) {
	if !platform.IsLinux() && !platform.IsDarwin() {
		return SupervisorNone, nil
	}
	srvUnit, err := unitFor(SupervisorSrv)
	if err != nil {
		return SupervisorNone, err
	}
	brewUnit, err := unitFor(SupervisorBrew)
	if err != nil {
		return SupervisorNone, err
	}
	switch srv, brew := srvUnit.installed(), brewUnit.installed(); {
	case srv && brew:
		return SupervisorNone, &DoubleSupervisedError{SrvUnit: srvUnit.path, BrewUnit: brewUnit.path}
	case srv:
		return SupervisorSrv, nil
	case brew:
		return SupervisorBrew, nil
	default:
		return SupervisorNone, nil
	}
}

// activeUnit returns the unit that runs the daemon, failing when none is
// installed or when two are.
func activeUnit() (unit, error) {
	sup, err := ActiveSupervisor()
	if err != nil {
		return unit{}, err
	}
	if sup == SupervisorNone {
		return unit{}, errors.New("daemon service is not installed")
	}
	return unitFor(sup)
}

// IsInstalled reports whether any unit runs the daemon, srv's own or the
// brew services one.
func IsInstalled() bool {
	sup, err := ActiveSupervisor()
	return err != nil || sup != SupervisorNone
}

// Install installs srv's own unit and starts the daemon. It refuses when
// brew services already supervises the daemon, since a second unit would
// race it.
func Install() error {
	brewUnit, err := unitFor(SupervisorBrew)
	if err != nil {
		return err
	}
	if brewUnit.installed() {
		return fmt.Errorf("%w (%s); not installing a second unit that would race it", ErrSupervisedByBrew, brewUnit.path)
	}
	switch {
	case platform.IsLinux():
		return installSystemd()
	default:
		return installLaunchd()
	}
}

// Uninstall removes srv's own unit. A daemon that only brew services runs is
// not srv's to remove, so that case returns ErrSupervisedByBrew; with both
// installed, removing srv's unit resolves the conflict.
func Uninstall() error {
	srvUnit, err := unitFor(SupervisorSrv)
	if err != nil {
		return err
	}
	if !srvUnit.installed() {
		brewUnit, err := unitFor(SupervisorBrew)
		if err != nil {
			return err
		}
		if brewUnit.installed() {
			return fmt.Errorf("%w (%s); remove it with 'brew services stop srv'", ErrSupervisedByBrew, brewUnit.path)
		}
	}
	switch {
	case platform.IsLinux():
		return uninstallSystemd()
	default:
		return uninstallLaunchd()
	}
}

// Restart restarts the daemon through whichever unit runs it.
func Restart() error {
	u, err := activeUnit()
	if err != nil {
		return err
	}
	if platform.IsLinux() {
		return restartSystemd(u)
	}
	return restartLaunchd(u)
}

// stopService stops the daemon without uninstalling its unit.
func stopService() error {
	u, err := activeUnit()
	if err != nil {
		return err
	}
	if platform.IsLinux() {
		return shell.Run("systemctl", "--user", "stop", u.name)
	}
	return shell.Run("launchctl", "unload", u.path)
}

// =============================================================================
// Systemd (Linux)
// =============================================================================

// renderSystemdUnit builds the srv-daemon.service unit as text. The shape is
// fixed and small; a writer beats a marshall-and-hope library for a file this
// static. HOME and XDG_CONFIG_HOME are pinned because a systemd service
// context may start with an empty environment. SRV_ROOT and
// SRV_CONTAINER_ENGINE are carried over when set: they are precedence-1
// overrides (config, ops/engine), and a daemon installed under one would
// otherwise silently point at the default root/engine instead.
func renderSystemdUnit(executable, homeDir string) string {
	env := []string{"HOME=" + homeDir, "XDG_CONFIG_HOME=" + homeDir + "/.config"}
	for _, key := range []string{"SRV_ROOT", "SRV_CONTAINER_ENGINE"} {
		if val := os.Getenv(key); val != "" {
			env = append(env, key+"="+val)
		}
	}

	var b strings.Builder
	b.WriteString("[Unit]\n")
	b.WriteString("Description=srv daemon - Docker container network connector\n")
	b.WriteString("Documentation=https://github.com/stubbedev/srv\n")
	b.WriteString("After=docker.service\n")
	b.WriteString("Wants=docker.service\n")
	b.WriteString("\n[Service]\n")
	b.WriteString("Type=simple\n")
	fmt.Fprintf(&b, "ExecStart=%s daemon start --foreground\n", executable)
	b.WriteString("Restart=on-failure\n")
	b.WriteString("RestartSec=5\n")
	for _, e := range env {
		fmt.Fprintf(&b, "Environment=%s\n", e)
	}
	b.WriteString("\n[Install]\n")
	b.WriteString("WantedBy=default.target\n")
	return b.String()
}

func systemdServicePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, SystemdUserDir, SystemdServiceName), nil
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

	serviceContent := renderSystemdUnit(executable, homeDir)

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

func restartSystemd(u unit) error {
	if err := shell.Run("systemctl", "--user", "restart", u.name); err != nil {
		return fmt.Errorf("failed to restart service %s: %w", u.name, err)
	}
	return nil
}

// systemdStatus returns the state systemd reports for unit u.
func systemdStatus(u unit) (string, error) {
	output, err := shell.RunQuiet("systemctl", "--user", "is-active", u.name)
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
// renderLaunchdPlist builds the LaunchAgent plist XML by hand: the document
// is a fixed key set over strings, one array, one bool, and two sub-dicts, so
// a writer is smaller than the plist marshalling dependency. Keys are emitted
// in a stable (alphabetical) order — launchd does not care, but deterministic
// output keeps installs reproducible. The daemon is kept alive across
// non-zero exits (KeepAlive/SuccessfulExit=false) and runs with a PATH that
// includes the binary's own dir plus the usual Homebrew/system locations. As
// with the systemd unit, SRV_ROOT and SRV_CONTAINER_ENGINE are carried over
// when set so the daemon honours the overrides its installer ran under.
func renderLaunchdPlist(executable, logPath string) string {
	env := map[string]string{
		"PATH": filepath.Dir(executable) + ":/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin",
	}
	for _, key := range []string{"SRV_ROOT", "SRV_CONTAINER_ENGINE"} {
		if val := os.Getenv(key); val != "" {
			env[key] = val
		}
	}

	var b strings.Builder
	w := func(format string, args ...any) {
		fmt.Fprintf(&b, format, args...)
	}
	key := func(name, value string) {
		w("    <key>%s</key>\n    <string>%s</string>\n", name, plistEscape(value))
	}
	w("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	w("<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n")
	w("<plist version=\"1.0\">\n<dict>\n")
	key("EnvironmentVariables", "")
	w("    <dict>\n")
	for _, k := range slices.Sorted(maps.Keys(env)) {
		w("        <key>%s</key>\n        <string>%s</string>\n", k, plistEscape(env[k]))
	}
	w("    </dict>\n")
	w("    <key>KeepAlive</key>\n    <dict>\n        <key>SuccessfulExit</key>\n        <false/>\n    </dict>\n")
	key("Label", LaunchdLabel)
	w("    <key>ProgramArguments</key>\n    <array>\n")
	for _, arg := range []string{executable, "daemon", "start", "--foreground"} {
		w("        <string>%s</string>\n", plistEscape(arg))
	}
	w("    </array>\n")
	w("    <key>RunAtLoad</key>\n    <true/>\n")
	key("StandardErrorPath", logPath)
	key("StandardOutPath", logPath)
	w("</dict>\n</plist>\n")
	return b.String()
}

// plistEscape escapes the five XML entities for attribute/element text.
func plistEscape(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"\"", "&quot;",
		"'", "&apos;",
	)
	return r.Replace(s)
}

func launchdPlistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, LaunchAgentsDir, LaunchdPlistName), nil
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

	// launchd holds its own descriptor on stdout/stderr. Pointing it at the
	// rotating daemon log would leave it writing into a rotated generation,
	// so panics and runtime output get a file of their own.
	logPath := filepath.Join(cfg.Root, StderrLogFile)

	plistContent := renderLaunchdPlist(executable, logPath)

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

func restartLaunchd(u unit) error {
	// Unload and reload
	_ = shell.Run("launchctl", "unload", u.path)
	if err := shell.Run("launchctl", "load", u.path); err != nil {
		return fmt.Errorf("failed to restart service %s: %w", u.name, err)
	}
	return nil
}

// launchdStatus returns the state launchd reports for unit u.
func launchdStatus(u unit) (string, error) {
	output, err := shell.RunQuiet("launchctl", "list", u.name)
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

// ServicePath returns the file of the unit that runs the daemon. It fails
// when no unit is installed, when two are, or on an unsupported platform, so
// callers never stat an empty path.
func ServicePath() (string, error) {
	u, err := activeUnit()
	if err != nil {
		return "", err
	}
	return u.path, nil
}

// ServiceStatus returns the state the service manager reports for the unit
// that runs the daemon: "not installed" without one, an error with two.
func ServiceStatus() (string, error) {
	sup, err := ActiveSupervisor()
	switch {
	case err != nil:
		return "", err
	case !platform.IsLinux() && !platform.IsDarwin():
		return "unsupported", nil
	case sup == SupervisorNone:
		return "not installed", nil
	}
	u, err := unitFor(sup)
	if err != nil {
		return "", err
	}
	if platform.IsLinux() {
		return systemdStatus(u)
	}
	return launchdStatus(u)
}
