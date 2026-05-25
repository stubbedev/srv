package site

import (
	"strings"
	"testing"
)

func TestRenderPHPComposeIncludesExtraNetworks(t *testing.T) {
	meta := SiteMetadata{
		Type:          SiteTypePHP,
		Domains:       []string{"app.test"},
		ProjectPath:   "/home/user/project",
		NetworkName:   "srv-abc_traefik",
		ExtraNetworks: []string{"mysql01_default", "redis01_default"},
		IsLocal:       true,
	}
	info := &PHPSiteInfo{PHPVersion: "8.4", Framework: "laravel", DocumentRoot: "public"}

	out, err := renderPHPCompose("app", meta, info, "/tmp/site")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	s := string(out)

	if !strings.Contains(s, "mysql01_default") {
		t.Errorf("expected mysql01_default in compose, got:\n%s", s)
	}
	if !strings.Contains(s, "redis01_default") {
		t.Errorf("expected redis01_default in compose, got:\n%s", s)
	}
	// Both must appear under the service's `networks:` list AND as top-level
	// external network entries.
	if strings.Count(s, "mysql01_default") < 2 {
		t.Errorf("expected mysql01_default to appear at service level + top level, got %d occurrences", strings.Count(s, "mysql01_default"))
	}
}

func TestRenderPHPComposeNoExtraNetworks(t *testing.T) {
	meta := SiteMetadata{
		Type:        SiteTypePHP,
		Domains:     []string{"app.test"},
		ProjectPath: "/home/user/project",
		NetworkName: "srv-abc_traefik",
		IsLocal:     true,
	}
	info := &PHPSiteInfo{PHPVersion: "8.4"}

	out, err := renderPHPCompose("app", meta, info, "/tmp/site")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	s := string(out)

	// Service must still join the traefik network.
	if !strings.Contains(s, "traefik") {
		t.Errorf("expected traefik network in compose, got:\n%s", s)
	}
}

func TestRenderPHPComposeFrankenPHPDocRoot(t *testing.T) {
	meta := SiteMetadata{
		Type:        SiteTypePHP,
		Domains:     []string{"app.test"},
		ProjectPath: "/home/user/project",
		NetworkName: "srv-abc_traefik",
	}
	info := &PHPSiteInfo{PHPVersion: "8.4", DocumentRoot: "public"}

	out, err := renderPHPCompose("app", meta, info, "/tmp/site")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	s := string(out)

	if !strings.Contains(s, "SERVER_ROOT") {
		t.Errorf("expected SERVER_ROOT env, got:\n%s", s)
	}
	if !strings.Contains(s, "/app/public") {
		t.Errorf("expected /app/public as docroot, got:\n%s", s)
	}
}

func TestRenderPHPComposeNoDocRootDefaults(t *testing.T) {
	meta := SiteMetadata{
		Type:        SiteTypePHP,
		Domains:     []string{"app.test"},
		ProjectPath: "/home/user/project",
		NetworkName: "srv-abc_traefik",
	}
	info := &PHPSiteInfo{PHPVersion: "8.4"}

	out, err := renderPHPCompose("app", meta, info, "/tmp/site")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(string(out), "SERVER_ROOT: /app\n") &&
		!strings.Contains(string(out), `SERVER_ROOT: "/app"`) {
		t.Errorf("expected SERVER_ROOT to default to /app, got:\n%s", out)
	}
}
