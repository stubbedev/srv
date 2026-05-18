package site

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/srv/internal/constants"
)

func TestWriteStaticSiteConfigCreatesFiles(t *testing.T) {
	root := withSRVRoot(t)
	meta := SiteMetadata{
		Type:        SiteTypeStatic,
		Domains:     []string{"blog.local"},
		ProjectPath: "/srv/blog",
		Port:        80,
		IsLocal:     true,
		NetworkName: "tnet",
		SPA:         true,
		Cache:       true,
	}
	if err := WriteStaticSiteConfig("blog", meta, true); err != nil {
		t.Fatalf("WriteStaticSiteConfig err: %v", err)
	}
	siteDir := filepath.Join(root, "sites", "blog")
	for _, f := range []string{"nginx.conf", "docker-compose.yml"} {
		if _, err := os.Stat(filepath.Join(siteDir, f)); err != nil {
			t.Errorf("missing %s: %v", f, err)
		}
	}
	nginx, _ := os.ReadFile(filepath.Join(siteDir, "nginx.conf"))
	if !strings.Contains(string(nginx), "try_files $uri $uri/ /index.html") {
		t.Error("nginx.conf missing SPA fallback")
	}
}

func TestWriteStaticSiteConfigForceFalsePreserves(t *testing.T) {
	root := withSRVRoot(t)
	meta := SiteMetadata{
		Type:        SiteTypeStatic,
		Domains:     []string{"blog.local"},
		ProjectPath: "/srv/blog",
		Port:        80,
		IsLocal:     true,
		NetworkName: "tnet",
	}
	if err := WriteStaticSiteConfig("blog", meta, true); err != nil {
		t.Fatal(err)
	}
	siteDir := filepath.Join(root, "sites", "blog")
	nginxPath := filepath.Join(siteDir, "nginx.conf")
	// Overwrite manually.
	if err := os.WriteFile(nginxPath, []byte("MANUAL"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteStaticSiteConfig("blog", meta, false); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(nginxPath)
	if string(got) != "MANUAL" {
		t.Errorf("force=false should preserve, got %q", string(got))
	}
}

func TestWriteNodeSiteConfig(t *testing.T) {
	root := withSRVRoot(t)
	meta := SiteMetadata{
		Type:        SiteTypeNode,
		Domains:     []string{"app.local"},
		ProjectPath: "/srv/app",
		Port:        3000,
		IsLocal:     true,
		NetworkName: "tnet",
	}
	info := &NodeSiteInfo{
		Runtime:        constants.NodeRuntimeNode,
		PackageManager: constants.NodePMNPM,
		NodeVersion:    "20",
		Framework:      constants.NodeFrameworkExpress,
		StartCmd:       "npm start",
		Port:           3000,
	}
	if err := WriteNodeSiteConfig("app", meta, info, true); err != nil {
		t.Fatalf("WriteNodeSiteConfig err: %v", err)
	}
	compose, err := os.ReadFile(filepath.Join(root, "sites", "app", "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(compose)
	if !strings.Contains(body, "node:20-alpine") {
		t.Error("compose missing node:20-alpine image")
	}
	if !strings.Contains(body, "npm start") {
		t.Error("compose missing start cmd")
	}
}

func TestWriteRubySiteConfig(t *testing.T) {
	root := withSRVRoot(t)
	meta := SiteMetadata{
		Type:        SiteTypeRuby,
		Domains:     []string{"api.local"},
		ProjectPath: "/srv/api",
		Port:        3000,
		IsLocal:     true,
		NetworkName: "tnet",
	}
	info := &RubySiteInfo{
		RubyVersion: "3.3",
		Framework:   constants.RubyFrameworkRails,
		StartCmd:    "sh -c 'bundle install && bundle exec rails server -b 0.0.0.0 -p 3000'",
		Port:        3000,
	}
	if err := WriteRubySiteConfig("api", meta, info, true); err != nil {
		t.Fatalf("WriteRubySiteConfig err: %v", err)
	}
	compose, err := os.ReadFile(filepath.Join(root, "sites", "api", "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(compose), "ruby:3.3-alpine") {
		t.Error("compose missing ruby image")
	}
}

func TestWritePythonSiteConfig(t *testing.T) {
	root := withSRVRoot(t)
	meta := SiteMetadata{
		Type:        SiteTypePython,
		Domains:     []string{"api.local"},
		ProjectPath: "/srv/api",
		Port:        8000,
		IsLocal:     true,
		NetworkName: "tnet",
	}
	info := &PythonSiteInfo{
		PythonVersion: "3.12",
		Framework:     constants.PythonFrameworkFlask,
		StartCmd:      "flask run",
		Port:          8000,
	}
	if err := WritePythonSiteConfig("api", meta, info, true); err != nil {
		t.Fatalf("WritePythonSiteConfig err: %v", err)
	}
	compose, err := os.ReadFile(filepath.Join(root, "sites", "api", "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(compose)
	if !strings.Contains(body, "python:3.12-alpine") {
		t.Error("compose missing python image")
	}
	if !strings.Contains(body, "FLASK_ENV") {
		t.Error("flask env should be set when framework=flask")
	}
}

func TestWriteDockerfileSiteConfig(t *testing.T) {
	root := withSRVRoot(t)
	meta := SiteMetadata{
		Type:        SiteTypeDockerfile,
		Domains:     []string{"app.local"},
		ProjectPath: "/srv/app",
		Port:        8080,
		IsLocal:     true,
		NetworkName: "tnet",
	}
	info := &DockerfileSiteInfo{Port: 8080}
	if err := WriteDockerfileSiteConfig("app", meta, info, true); err != nil {
		t.Fatalf("err: %v", err)
	}
	compose, err := os.ReadFile(filepath.Join(root, "sites", "app", "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(compose), "build:") {
		t.Error("compose missing build directive")
	}
	if !strings.Contains(string(compose), constants.DockerfileFile) {
		t.Error("compose missing Dockerfile reference")
	}
}
