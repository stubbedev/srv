package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/srv/internal/shell"
	"github.com/stubbedev/srv/internal/shell/shelltest"
)

func TestPrintLastLinesMissing(t *testing.T) {
	if err := printLastLines("/nonexistent-srv-cmd", 5); err == nil {
		t.Error("expected err")
	}
}

func TestPrintLastLinesZeroN(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "log")
	if err := os.WriteFile(tmp, []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := printLastLines(tmp, 0); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestPrintLastLinesRingBuffer(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "log")
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = "line" + strings.Repeat("x", i%5)
	}
	if err := os.WriteFile(tmp, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := printLastLines(tmp, 5); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunDaemonStartNoServiceInstalled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Not installed in this temp HOME.
	daemonStartFlags.foreground = false
	defer func() { daemonStartFlags.foreground = false }()
	if err := runDaemonStart(nil, nil); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunDaemonStopNotInstalled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := runDaemonStop(nil, nil); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunDaemonStatus(t *testing.T) {
	setupSrvRoot(t)
	if err := runDaemonStatus(nil, nil); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunDaemonLogsMissing(t *testing.T) {
	setupSrvRoot(t)
	if err := runDaemonLogs(nil, nil); err != nil {
		t.Errorf("missing log file should be no-op: %v", err)
	}
}

func TestRunDaemonLogsExisting(t *testing.T) {
	root := setupSrvRoot(t)
	logPath := filepath.Join(root, "daemon.log")
	if err := os.WriteFile(logPath, []byte("line1\nline2\nline3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	daemonLogsFlags.follow = false
	daemonLogsFlags.tail = 2
	if err := runDaemonLogs(nil, nil); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunDaemonUninstallNotInstalled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := runDaemonUninstall(nil, nil); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunDaemonRestartNotInstalled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := runDaemonRestart(nil, nil); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunDaemonStopServiceRunning(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Pre-create a fake systemd service file so IsInstalled returns true.
	servicePath := filepath.Join(home, ".config", "systemd", "user", "srv-daemon.service")
	if err := os.MkdirAll(filepath.Dir(servicePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(servicePath, []byte("[Unit]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Stub shell so the systemctl `stop` call doesn't actually run.
	t.Cleanup(shellSwap())
	_ = runDaemonStop(nil, nil)
}

func TestRunDaemonRestartInstalled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	servicePath := filepath.Join(home, ".config", "systemd", "user", "srv-daemon.service")
	if err := os.MkdirAll(filepath.Dir(servicePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(servicePath, []byte("[Unit]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shellSwap())
	_ = runDaemonRestart(nil, nil)
}

func TestRunDaemonInstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Cleanup(shell.SwapDefault(shelltest.New(nil)))
	if err := runDaemonInstall(nil, nil); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunDaemonInstallAlreadyInstalled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	servicePath := filepath.Join(home, ".config", "systemd", "user", "srv-daemon.service")
	if err := os.MkdirAll(filepath.Dir(servicePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(servicePath, []byte("[Unit]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runDaemonInstall(nil, nil); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunDaemonUninstallInstalled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	servicePath := filepath.Join(home, ".config", "systemd", "user", "srv-daemon.service")
	if err := os.MkdirAll(filepath.Dir(servicePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(servicePath, []byte("[Unit]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shellSwap())
	if err := runDaemonUninstall(nil, nil); err != nil {
		t.Errorf("err: %v", err)
	}
}

func shellSwap() func() {
	return shell.SwapDefault(shelltest.New(nil))
}

func TestRunDaemonLogsFollow(t *testing.T) {
	root := setupSrvRoot(t)
	logPath := filepath.Join(root, "daemon.log")
	if err := os.WriteFile(logPath, []byte("line1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prev := daemonLogsFlags
	defer func() { daemonLogsFlags = prev }()
	daemonLogsFlags.tail = 1
	daemonLogsFlags.follow = false
	if err := runDaemonLogs(nil, nil); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunDaemonStartForegroundLogErr(t *testing.T) {
	root := setupSrvRoot(t)
	prev := daemonStartFlags
	defer func() { daemonStartFlags = prev }()
	daemonStartFlags.foreground = true
	daemonStartFlags.noWatch = true
	// Block log file by making log path a non-empty dir.
	if err := os.MkdirAll(filepath.Join(root, "daemon.log", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "daemon.log", "sub", "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runDaemonStart(nil, nil); err == nil {
		t.Error("expected err opening log dir as file")
	}
}
