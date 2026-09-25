package daemon

import (
	"strings"
	"testing"
)

func TestRenderSystemdUnitGolden(t *testing.T) {
	unit, err := renderSystemdUnit("/usr/bin/srv", "/home/u")
	if err != nil {
		t.Fatal(err)
	}
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
