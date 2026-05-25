package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/srv/internal/importers/valet"
)

func TestPortFromHostPort(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"localhost:8080", 8080},
		{"127.0.0.1:80", 80},
		{"missingport", 0},
		{"", 0},
		{":80", 80},
		{"host:notnum", 0},
	}
	for _, c := range cases {
		if got := portFromHostPort(c.in); got != c.want {
			t.Errorf("portFromHostPort(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestRouteFlags(t *testing.T) {
	cases := []struct {
		name string
		r    valet.Route
		want string
	}{
		{"path", valet.Route{Path: "/api"}, "--path /api"},
		{"regex", valet.Route{PathRegex: "^/v1"}, "--path-regex '^/v1'"},
		{"rewrite", valet.Route{PathRegex: "^/x", Rewrite: "/y"}, "--path-regex '^/x' --rewrite '/y'"},
		{"port", valet.Route{Path: "/api", Port: 8080}, "--path /api --port 8080"},
		{"empty", valet.Route{}, ""},
	}
	for _, c := range cases {
		if got := routeFlags(c.r); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestAddLimitFlags(t *testing.T) {
	args := []string{"add", "/srv/x"}
	s := &valet.Site{MaxBody: "2G", ReadTimeout: "60s", SendTimeout: "30s", ConnTimeout: "5s"}
	addLimitFlags(&args, s)
	joined := strings.Join(args, " ")
	for _, want := range []string{"--max-body 2G", "--read-timeout 60s", "--send-timeout 30s", "--connect-timeout 5s"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in %q", want, joined)
		}
	}
}

func TestAddLimitFlagsAllEmpty(t *testing.T) {
	args := []string{"add"}
	addLimitFlags(&args, &valet.Site{})
	if len(args) != 1 {
		t.Errorf("expected no flags added, got %v", args)
	}
}

func TestValidateValetDirMissing(t *testing.T) {
	dir := t.TempDir()
	if err := validateValetDir(dir); err == nil {
		t.Error("expected err when Nginx/ missing")
	}
}

func TestValidateValetDirPresent(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "Nginx"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := validateValetDir(dir); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestResolveValetDirExplicitValid(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "Nginx"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := resolveValetDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != dir {
		t.Errorf("got %q", got)
	}
}

func TestResolveValetDirExplicitMissing(t *testing.T) {
	if _, err := resolveValetDir("/nonexistent-srv-valet-explicit"); err == nil {
		t.Error("expected err")
	}
}

func TestResolveValetDirFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	xdgDir := filepath.Join(home, ".config", "valet", "Nginx")
	if err := os.MkdirAll(xdgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := resolveValetDir("")
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(home, ".config", "valet") {
		t.Errorf("got %q", got)
	}
}

func TestResolveValetDirAllMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if _, err := resolveValetDir(""); err == nil {
		t.Error("expected err when no candidates exist")
	}
}

func TestBuildImportPlanGrouping(t *testing.T) {
	sites := []*valet.Site{
		{Domain: "cms.test", ProjectPath: "/srv/cms", IsPHP: true},
		{Domain: "cms-admin.test", ProjectPath: "/srv/cms", IsPHP: true, Aliases: []string{"cms-extra.test"}},
		{Domain: "other.test", ProjectPath: "/srv/other", IsPHP: true},
	}
	plan := buildImportPlan(sites)
	// Each PHP site now produces TWO steps: scaffold + add.
	if len(plan) != 4 {
		t.Errorf("expected 4 steps (2 grouped sites × scaffold+add), got %d (%v)", len(plan), plan)
	}
	var addLine string
	for _, step := range plan {
		if strings.Contains(step.line, " add /srv/cms ") {
			addLine = step.line
		}
	}
	if addLine == "" {
		t.Fatalf("missing add step for /srv/cms; got %v", plan)
	}
	if !strings.Contains(addLine, "--alias cms-admin.test") {
		t.Errorf("aliases not folded into add: %q", addLine)
	}
	// Scaffold step must come first.
	if !strings.Contains(plan[0].line, "scaffold") {
		t.Errorf("first step should be scaffold, got %q", plan[0].line)
	}
}

func TestBuildImportPlanProxy(t *testing.T) {
	sites := []*valet.Site{
		{Domain: "api.test", ProxyTarget: "localhost:8080"},
	}
	plan := buildImportPlan(sites)
	if len(plan) != 1 {
		t.Fatalf("expected 1 plan, got %d", len(plan))
	}
	if !strings.Contains(plan[0].line, "proxy add") {
		t.Errorf("expected proxy add line, got %q", plan[0].line)
	}
	if !strings.Contains(plan[0].line, "-p 8080") {
		t.Errorf("port flag missing: %q", plan[0].line)
	}
}

func TestPlanLooseSiteUnresolvedPHP(t *testing.T) {
	s := &valet.Site{Domain: "x.test", IsPHP: true, File: "/etc/nginx/x.test", Internal: true, Wildcard: true}
	step, ok := planLooseSite(s)
	if !ok {
		t.Fatal("expected ok=true for unresolved PHP")
	}
	if !strings.Contains(step.line, "unresolved") {
		t.Errorf("expected commented marker: %q", step.line)
	}
	if !strings.Contains(step.line, "--internal-http") || !strings.Contains(step.line, "--wildcard") {
		t.Errorf("flags missing: %q", step.line)
	}
}

func TestPlanLooseSiteEmptyDomain(t *testing.T) {
	if _, ok := planLooseSite(&valet.Site{}); ok {
		t.Error("empty-domain site should be skipped")
	}
}

func TestPlanLooseSiteProxyMissingPort(t *testing.T) {
	if _, ok := planLooseSite(&valet.Site{Domain: "x.test", ProxyTarget: "no-port-here"}); ok {
		t.Error("proxy without port should be skipped")
	}
}

func TestRunImportValetMissingDir(t *testing.T) {
	importFlags.valetDir = "/nonexistent-srv-valet"
	importFlags.apply = false
	defer func() { importFlags.valetDir = "" }()
	if err := runImportValet(nil, nil); err == nil {
		t.Error("expected err")
	}
}

func TestRunImportValetDryRun(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "Nginx"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Add one valid nginx file so plan has something to print.
	conf := `server { listen 443 ssl; server_name api.test; }`
	if err := os.WriteFile(filepath.Join(dir, "Nginx", "api.test"), []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	importFlags.valetDir = dir
	importFlags.apply = false
	defer func() {
		importFlags.valetDir = ""
		importFlags.apply = false
	}()
	if err := runImportValet(nil, nil); err != nil {
		t.Errorf("dry-run err: %v", err)
	}
}

func TestRunImportValetEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "Nginx"), 0o755); err != nil {
		t.Fatal(err)
	}
	importFlags.valetDir = dir
	defer func() { importFlags.valetDir = "" }()
	if err := runImportValet(nil, nil); err != nil {
		t.Errorf("empty dir -> %v", err)
	}
}
