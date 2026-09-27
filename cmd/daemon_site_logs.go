// Package cmd — daemon_site_logs.go renders the access log of daemon-served
// static sites. The daemon's embedded HTTP server (internal/httpd) writes one
// JSON event per request into the daemon log file via zerolog; `srv logs`
// filters those lines back out by site and renders them the way
// `docker compose logs` would render a container's stdout.
package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/daemon"
	"github.com/stubbedev/srv/internal/logfile"
	"github.com/stubbedev/srv/internal/ui"
)

// siteLogToken is the exact JSON substring that identifies a request line for
// a site; the site name is validated [a-z0-9-] so the quoted token cannot
// match a different key or value.
func siteLogToken(siteName string) string {
	return `"site":"` + siteName + `"`
}

// streamDaemonSiteLogs prints a daemon-served site's access lines from the
// daemon log, then follows for new ones when follow is set. Missing log file
// is a friendly no-op, not an error.
func streamDaemonSiteLogs(siteName string, follow bool, tail int) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logPath := daemon.LogPath(cfg)
	if _, err := os.Stat(logPath); err != nil {
		if os.IsNotExist(err) {
			ui.Dim("No daemon log file found — the srv daemon has not run yet")
			return nil
		}
		return fmt.Errorf("cannot access daemon log file: %w", err)
	}

	lines, err := daemonSiteLogLines(logPath, siteName, tail)
	if err != nil {
		return err
	}
	printDaemonSiteLogLines(lines, "")
	if !follow {
		return nil
	}
	return followDaemonSiteLog(logPath, siteName)
}

// daemonSiteLogLines returns the log's request lines for one site, across
// rotated generations. tail <= 0 returns every matching line; otherwise only
// the last tail.
func daemonSiteLogLines(path, siteName string, tail int) ([]string, error) {
	token := siteLogToken(siteName)
	lines, err := logfile.Tail(path, tail, func(line string) bool { return strings.Contains(line, token) })
	if err != nil {
		return nil, fmt.Errorf("error reading daemon log file: %w", err)
	}
	return lines, nil
}

// printDaemonSiteLogLines renders request lines; a non-empty prefix gets the
// `[site]` treatment used by `srv logs --all`.
func printDaemonSiteLogLines(lines []string, prefix string) {
	for _, raw := range lines {
		formatted, ok := formatDaemonAccessLine(raw)
		if !ok {
			continue
		}
		if prefix != "" {
			fmt.Printf("[%s] %s\n", prefix, formatted)
		} else {
			fmt.Println(formatted)
		}
	}
}

// formatDaemonAccessLine turns one raw JSON access event into
// `15:04:05 GET / 200 4.2KiB in 1.2ms`. Unparseable lines are skipped so a
// truncated tail of the daemon log never prints garbage.
func formatDaemonAccessLine(raw string) (string, bool) {
	var event struct {
		Time   string  `json:"time"`
		Method string  `json:"method"`
		Path   string  `json:"path"`
		Status int     `json:"status"`
		Bytes  int64   `json:"bytes"`
		DurMS  float64 `json:"dur_ms"`
	}
	if err := json.Unmarshal([]byte(raw), &event); err != nil || event.Status == 0 || event.Method == "" {
		return "", false
	}
	clock := ""
	if t, err := time.Parse(time.RFC3339, event.Time); err == nil {
		clock = t.Format("15:04:05") + " "
	}
	return fmt.Sprintf("%s%s %s %d %s in %.1fms", clock, event.Method, event.Path, event.Status, daemonLogBytes(event.Bytes), event.DurMS), true
}

// daemonLogBytes formats a byte count the way the access log renders it.
func daemonLogBytes(b int64) string {
	switch {
	case b >= 1<<20:
		return fmt.Sprintf("%.1fMiB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1fKiB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%dB", b)
	}
}

// followDaemonSiteLog streams one site's new request lines from the daemon
// log, across rotations, until the process is interrupted.
func followDaemonSiteLog(path, siteName string) error {
	token := siteLogToken(siteName)
	return logfile.Follow(context.Background(), path, func(line string) {
		if !strings.Contains(line, token) {
			return
		}
		if formatted, ok := formatDaemonAccessLine(line); ok {
			fmt.Println(formatted)
		}
	})
}
