package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/site"
)

func TestRunListEmpty(t *testing.T) {
	setupSrvRoot(t)
	if err := runList(nil, nil); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunListWithSites(t *testing.T) {
	root := setupSrvRoot(t)
	projectDir := filepath.Join(root, "p")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestSite(t, "blog", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"blog.local"},
		ProjectPath: projectDir,
		Port:        80,
		IsLocal:     true,
		NetworkName: "n",
	})
	if err := runList(nil, nil); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunInfoMissingSite(t *testing.T) {
	setupSrvRoot(t)
	if err := runInfo(nil, []string{"ghost"}); err == nil {
		t.Error("expected err")
	}
}

func TestRunInfoStatic(t *testing.T) {
	root := setupSrvRoot(t)
	projectDir := filepath.Join(root, "p")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestSite(t, "blog", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"blog.local"},
		ProjectPath: projectDir,
		Port:        80,
		IsLocal:     true,
		NetworkName: "n",
	})
	if err := runInfo(nil, []string{"blog"}); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunInfoNode(t *testing.T) {
	root := setupSrvRoot(t)
	projectDir := filepath.Join(root, "p")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestSite(t, "app", site.SiteMetadata{
		Type:               site.SiteTypeNode,
		Domains:            []string{"app.local"},
		ProjectPath:        projectDir,
		Port:               3000,
		IsLocal:            true,
		NetworkName:        "n",
		NodeRuntime:        "node",
		NodePackageManager: "yarn",
		NodeVersion:        "20",
		NodeFramework:      "next",
	})
	if err := runInfo(nil, []string{"app"}); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunInfoRuby(t *testing.T) {
	root := setupSrvRoot(t)
	projectDir := filepath.Join(root, "p")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestSite(t, "api", site.SiteMetadata{
		Type:          site.SiteTypeRuby,
		Domains:       []string{"api.local"},
		ProjectPath:   projectDir,
		Port:          3000,
		IsLocal:       true,
		NetworkName:   "n",
		RubyVersion:   "3.3",
		RubyFramework: "rails",
	})
	if err := runInfo(nil, []string{"api"}); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunInfoPython(t *testing.T) {
	root := setupSrvRoot(t)
	projectDir := filepath.Join(root, "p")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestSite(t, "api", site.SiteMetadata{
		Type:            site.SiteTypePython,
		Domains:         []string{"api.local"},
		ProjectPath:     projectDir,
		Port:            8000,
		IsLocal:         true,
		NetworkName:     "n",
		PythonVersion:   "3.12",
		PythonFramework: "flask",
	})
	if err := runInfo(nil, []string{"api"}); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunInfoCompose(t *testing.T) {
	root := setupSrvRoot(t)
	projectDir := filepath.Join(root, "p")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestSite(t, "x", site.SiteMetadata{
		Type:        site.SiteTypeCompose,
		Domains:     []string{"x.local"},
		ProjectPath: projectDir,
		Port:        8080,
		NetworkName: "n",
		ServiceName: "x-web",
	})
	if err := runInfo(nil, []string{"x"}); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestGetSSLStatusBroken(t *testing.T) {
	s := site.Site{IsBroken: true}
	if got := getSSLStatus(s); got == "" {
		t.Error("expected placeholder")
	}
}

func TestGetSSLStatusProduction(t *testing.T) {
	s := site.Site{IsLocal: false}
	if got := getSSLStatus(s); !strings.Contains(stripAnsiCmd(got), "auto") {
		t.Errorf("expected 'auto', got %q", got)
	}
}

func TestGetSSLStatusLocalMissing(t *testing.T) {
	setupSrvRoot(t)
	s := site.Site{Name: "blog", Domains: []string{"blog.local"}, IsLocal: true}
	got := getSSLStatus(s)
	if !strings.Contains(stripAnsiCmd(got), "missing") {
		t.Errorf("expected 'missing', got %q", got)
	}
}

func TestRunLogsDockerDown(t *testing.T) {
	setupSrvRoot(t)
	t.Cleanup(docker.SwapNewClientErr(errors.New("offline")))
	if err := runLogs(nil, []string{"ghost"}); err == nil {
		t.Error("expected err")
	}
}

func TestRunLogsAllEmpty(t *testing.T) {
	setupSrvRoot(t)
	if err := runLogsAll(); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunLogsHappy(t *testing.T) {
	root := setupSrvRoot(t)
	projectDir := filepath.Join(root, "p")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestSite(t, "blog", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"blog.local"},
		ProjectPath: projectDir,
		Port:        80,
		NetworkName: "n",
	})
	t.Cleanup(docker.SwapNewClientOK())
	t.Cleanup(docker.SwapComposeExec(func(string, bool, ...string) error { return nil }))
	if err := runLogs(nil, []string{"blog"}); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestSetupColoredHelp(t *testing.T) {
	setupColoredHelp()
}

func TestShowCertInfoNoCerts(t *testing.T) {
	setupSrvRoot(t)
	showCertInfo("missing.local")
}

func TestRunInfoBroken(t *testing.T) {
	setupSrvRoot(t)
	writeTestSite(t, "broken", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"x.local"},
		ProjectPath: "/no/such/path-srv-broken",
		Port:        80,
		NetworkName: "n",
	})
	if err := runInfo(nil, []string{"broken"}); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunInfoDockerfile(t *testing.T) {
	root := setupSrvRoot(t)
	projectDir := filepath.Join(root, "p")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestSite(t, "app", site.SiteMetadata{
		Type:           site.SiteTypeDockerfile,
		Domains:        []string{"app.local"},
		ProjectPath:    projectDir,
		Port:           8080,
		IsLocal:        true,
		NetworkName:    "n",
		DockerfilePort: 8080,
	})
	if err := runInfo(nil, []string{"app"}); err != nil {
		t.Errorf("err: %v", err)
	}
}

// stripAnsiCmd is a tiny ANSI stripper for cmd tests (cmd pkg has no public one).
func stripAnsiCmd(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		if r == '\x1b' {
			in = true
			continue
		}
		if in {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				in = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func TestFormatDomainsForList(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{nil, ""},
		{[]string{}, ""},
		{[]string{"a.local"}, "a.local"},
		{[]string{"a.local", "b.local"}, "a.local (+1)"},
		{[]string{"a.local", "b.local", "c.local"}, "a.local (+2)"},
	}
	for _, c := range cases {
		if got := formatDomainsForList(c.in); got != c.want {
			t.Errorf("formatDomainsForList(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestGetSiteTypeLabel(t *testing.T) {
	cases := []struct {
		s    site.Site
		want string
	}{
		{site.Site{IsBroken: true}, "-"},
		{site.Site{Type: site.SiteTypeStatic}, "static"},
		{site.Site{Type: site.SiteTypeNode}, "node"},
		{site.Site{Type: site.SiteTypeRuby}, "ruby"},
		{site.Site{Type: site.SiteTypePython}, "python"},
		{site.Site{Type: site.SiteTypeDockerfile}, "dockerfile"},
		{site.Site{Type: site.SiteTypeCompose}, "compose"},
		{site.Site{Type: ""}, "compose"},
	}
	for _, c := range cases {
		got := getSiteTypeLabel(c.s)
		// Strip ANSI for broken case.
		if c.s.IsBroken {
			if !strings.Contains(got, "-") {
				t.Errorf("broken label missing '-': %q", got)
			}
			continue
		}
		if got != c.want {
			t.Errorf("getSiteTypeLabel(%+v) = %q, want %q", c.s, got, c.want)
		}
	}
}
