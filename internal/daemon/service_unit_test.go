package daemon

import (
	"strings"
	"testing"
)

func TestRenderSystemdUnitGolden(t *testing.T) {
	unit := renderSystemdUnit("/usr/bin/srv", "/home/u")
	want := strings.Join([]string{
		"[Unit]",
		"Description=srv daemon - Docker container network connector",
		"Documentation=https://github.com/stubbedev/srv",
		"After=docker.service",
		"Wants=docker.service",
		"",
		"[Service]",
		"Type=simple",
		"ExecStart=/usr/bin/srv daemon start --foreground",
		"Restart=on-failure",
		"RestartSec=5",
		"Environment=HOME=/home/u",
		"Environment=XDG_CONFIG_HOME=/home/u/.config",
		"",
		"[Install]",
		"WantedBy=default.target",
		"",
	}, "\n")
	if unit != want {
		t.Errorf("got:\n%s\nwant:\n%s", unit, want)
	}
}

func TestRenderLaunchdPlistIsValid(t *testing.T) {
	t.Setenv("SRV_ROOT", "/custom/root")
	plist := renderLaunchdPlist("/usr/bin/srv", "/tmp/srv.log")
	for _, want := range []string{
		"<key>Label</key>", "<string>dev.stubbe.srv-daemon</string>",
		"<key>RunAtLoad</key>", "<true/>",
		"<key>SuccessfulExit</key>", "<false/>",
		"<key>SRV_ROOT</key>", "<string>/custom/root</string>",
		"<!DOCTYPE plist",
	} {
		if !strings.Contains(plist, want) {
			t.Errorf("plist missing %q:\n%s", want, plist)
		}
	}
	// An executable path with XML-special characters must not break the file.
	escaped := renderLaunchdPlist("/opt/a b&c<d>/srv", "/tmp/x")
	if !strings.Contains(escaped, "a b&amp;c&lt;d&gt;") {
		t.Errorf("XML escaping missing:\n%s", escaped)
	}
}
