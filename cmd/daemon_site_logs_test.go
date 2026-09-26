package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
