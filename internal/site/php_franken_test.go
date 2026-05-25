package site

import (
	"os"
	"path/filepath"
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

func TestResolvePHPDockerfileGenerated(t *testing.T) {
	dir := t.TempDir()
	meta := SiteMetadata{ProjectPath: dir}
	info := &PHPSiteInfo{PHPVersion: "8.4"}
	df, source, err := resolvePHPDockerfile(meta, info)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if source != "" {
		t.Errorf("expected no override source, got %q", source)
	}
	if !strings.Contains(df, "FROM dunglas/frankenphp:php8.4-alpine") {
		t.Errorf("expected generated Dockerfile, got:\n%s", df)
	}
}

func TestResolvePHPDockerfileProjectOverride(t *testing.T) {
	dir := t.TempDir()
	override := "FROM dunglas/frankenphp:php8.3-alpine\nRUN echo override\n"
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile.srv"), []byte(override), 0o644); err != nil {
		t.Fatal(err)
	}
	meta := SiteMetadata{ProjectPath: dir}
	info := &PHPSiteInfo{PHPVersion: "8.4"}
	df, source, err := resolvePHPDockerfile(meta, info)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.HasSuffix(source, "Dockerfile.srv") {
		t.Errorf("expected override source to end with Dockerfile.srv, got %q", source)
	}
	if df != override {
		t.Errorf("expected override contents verbatim, got:\n%s", df)
	}
}

func TestResolvePHPDockerfileSrvSubdir(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".srv"), 0o755); err != nil {
		t.Fatal(err)
	}
	override := "FROM dunglas/frankenphp:1-php8.4-alpine\n"
	if err := os.WriteFile(filepath.Join(dir, ".srv", "Dockerfile"), []byte(override), 0o644); err != nil {
		t.Fatal(err)
	}
	meta := SiteMetadata{ProjectPath: dir}
	info := &PHPSiteInfo{PHPVersion: "8.4"}
	df, source, err := resolvePHPDockerfile(meta, info)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.HasSuffix(source, filepath.Join(".srv", "Dockerfile")) {
		t.Errorf("expected .srv/Dockerfile source, got %q", source)
	}
	if df != override {
		t.Errorf("expected override contents verbatim, got:\n%s", df)
	}
}

func TestResolvePHPDockerfileRejectsNonFrankenPHPBase(t *testing.T) {
	dir := t.TempDir()
	override := "FROM php:8.3-fpm-alpine\nRUN echo bad\n"
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile.srv"), []byte(override), 0o644); err != nil {
		t.Fatal(err)
	}
	meta := SiteMetadata{ProjectPath: dir}
	info := &PHPSiteInfo{PHPVersion: "8.4"}
	_, _, err := resolvePHPDockerfile(meta, info)
	if err == nil {
		t.Fatal("expected error for non-FrankenPHP FROM")
	}
	if !strings.Contains(err.Error(), "dunglas/frankenphp") {
		t.Errorf("error should mention required base image, got: %v", err)
	}
}

func TestResolvePHPDockerfileSkipsCommentedFROM(t *testing.T) {
	dir := t.TempDir()
	override := "# FROM php:8.3-fpm-alpine\nFROM dunglas/frankenphp:alpine\n"
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile.srv"), []byte(override), 0o644); err != nil {
		t.Fatal(err)
	}
	meta := SiteMetadata{ProjectPath: dir}
	info := &PHPSiteInfo{PHPVersion: "8.4"}
	_, _, err := resolvePHPDockerfile(meta, info)
	if err != nil {
		t.Errorf("commented FROM should be skipped, got: %v", err)
	}
}
