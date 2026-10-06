package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeDaemonLog builds a daemon log file with a mix of access events and
// unrelated lines, the way the daemon's embedded HTTP server and d.log write
// them interleaved.
func writeDaemonLog(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "daemon.log")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func accessLine(site string) string {
	return `{"level":"info","time":"2026-09-26T10:00:0` + site[len(site)-1:] + `Z","site":"` + site + `","method":"GET","path":"/","status":200,"bytes":42,"dur_ms":1.5,"message":"request"}`
}

func TestDaemonSiteLogLinesFiltersBySite(t *testing.T) {
	path := writeDaemonLog(t,
		accessLine("site-a"),
		"[2026-09-26 10:00:01] Embedded static server listening on 127.0.0.1:15380",
		accessLine("site-b"),
		accessLine("site-a"),
		"not json at all",
	)

	lines, err := daemonSiteLogLines(path, "site-a", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 {
		t.Fatalf("want 2 site-a lines, got %d", len(lines))
	}
	for _, line := range lines {
		if !strings.Contains(line, `"site":"site-a"`) {
			t.Errorf("foreign line leaked: %s", line)
		}
	}
}

func TestDaemonSiteLogLinesTail(t *testing.T) {
	path := writeDaemonLog(t, accessLine("site-a"), accessLine("site-a"), accessLine("site-a"))

	lines, err := daemonSiteLogLines(path, "site-a", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 {
		t.Fatalf("want last 2 lines, got %d", len(lines))
	}
	// The ring buffer must preserve order: the second-oldest kept line first.
	for i, line := range lines {
		if !strings.Contains(line, `"site":"site-a"`) {
			t.Errorf("line %d: unexpected content", i)
		}
	}
}

func TestFormatDaemonAccessLine(t *testing.T) {
	raw := `{"level":"info","time":"2026-09-26T10:00:00Z","site":"start-local","method":"GET","path":"/","status":200,"bytes":4300,"dur_ms":1.25,"message":"request"}`
	formatted, ok := formatDaemonAccessLine(raw)
	if !ok {
		t.Fatal("expected the event to format")
	}
	for _, want := range []string{"10:00:00", "GET / 200", "4.2KiB", "in 1.2ms"} {
		if !strings.Contains(formatted, want) {
			t.Errorf("formatted %q missing %q", formatted, want)
		}
	}

	for _, bad := range []string{"", "not json", `{"time":"nope"}`, `{"method":"GET","time":"2026-09-26T10:00:00Z"}`} {
		if _, ok := formatDaemonAccessLine(bad); ok {
			t.Errorf("garbage line %q should not format", bad)
		}
	}
}

func TestParseTailFlag(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int
	}{
		{"", 0}, {"  ", 0}, {"5", 5}, {"junk", 0}, {"0", 0},
	} {
		if got := parseTailFlag(tc.in); got != tc.want {
			t.Errorf("parseTailFlag(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestParseSinceDuration(t *testing.T) {
	cutoff, err := parseSinceDuration("")
	if err != nil || !cutoff.IsZero() {
		t.Errorf("empty --since: cutoff=%v err=%v, want zero cutoff", cutoff, err)
	}
	cutoff, err = parseSinceDuration("10m")
	if err != nil || cutoff.IsZero() {
		t.Errorf("10m --since: cutoff=%v err=%v", cutoff, err)
	}
	if d := time.Since(cutoff); d < 9*time.Minute || d > 11*time.Minute {
		t.Errorf("10m cutoff landed %v in the past", d)
	}
	if _, err := parseSinceDuration("yesterday"); err == nil {
		t.Error("unparseable --since must error, not be silently ignored")
	}
}

// daemonLineAfter filters by the event timestamp; unparseable lines stay so
// the renderer (not the filter) decides their fate.
func TestDaemonLineAfter(t *testing.T) {
	old := `{"time":"2020-01-01T00:00:00Z","site":"a","method":"GET","path":"/","status":200}`
	fresh := `{"time":"` + time.Now().UTC().Format(time.RFC3339) + `","site":"a","method":"GET","path":"/","status":200}`
	cutoff := time.Now().Add(-time.Hour)

	if !daemonLineAfter(old, time.Time{}) {
		t.Error("zero cutoff must keep everything")
	}
	if daemonLineAfter(old, cutoff) {
		t.Error("2020 event survived a one-hour cutoff")
	}
	if !daemonLineAfter(fresh, cutoff) {
		t.Error("current event dropped by a one-hour cutoff")
	}
	if !daemonLineAfter("not json", cutoff) {
		t.Error("unparseable line dropped by the filter")
	}
}
