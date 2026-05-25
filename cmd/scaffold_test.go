package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelectScaffoldTemplatePHPLaravel(t *testing.T) {
	tpl, err := selectScaffoldTemplate("php", "laravel")
	if err != nil {
		t.Fatal(err)
	}
	if tpl.docRoot != "public" {
		t.Errorf("laravel docroot = %q, want public", tpl.docRoot)
	}
	if len(tpl.extensions) == 0 {
		t.Error("laravel template should ship default extensions")
	}
}

func TestSelectScaffoldTemplatePHPGenericFallback(t *testing.T) {
	tpl, err := selectScaffoldTemplate("php", "bogus")
	if err != nil {
		t.Fatal(err)
	}
	if tpl.framework != "generic" {
		t.Errorf("unknown framework should fall back to generic, got %q", tpl.framework)
	}
}

func TestSelectScaffoldTemplateUnknownLang(t *testing.T) {
	if _, err := selectScaffoldTemplate("rust", "actix"); err == nil {
		t.Fatal("expected error for unknown language")
	}
}

func TestScaffoldTemplateApplyOverrides(t *testing.T) {
	tpl, _ := selectScaffoldTemplate("php", "laravel")
	tpl.applyOverrides("8.3", 8080, "imagick,xdebug")
	if tpl.version != "8.3" {
		t.Errorf("version not overridden: %q", tpl.version)
	}
	if tpl.port != 8080 {
		t.Errorf("port not overridden: %d", tpl.port)
	}
	if !strings.Contains(strings.Join(tpl.extensions, ","), "imagick") {
		t.Errorf("extensions not appended: %v", tpl.extensions)
	}
	if !strings.Contains(tpl.baseImage, "php8.3") {
		t.Errorf("base image not rebuilt for version override: %q", tpl.baseImage)
	}
}

func TestScaffoldRenderProducesAllFiles(t *testing.T) {
	tpl, _ := selectScaffoldTemplate("php", "laravel")
	files := tpl.render()
	for _, name := range []string{"Dockerfile", "docker-compose.yml", ".dockerignore"} {
		if _, ok := files[name]; !ok {
			t.Errorf("missing file %q", name)
		}
	}
}

func TestRunScaffoldRefusesExisting(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	scaffoldFlags.lang = "php"
	scaffoldFlags.framework = "laravel"
	scaffoldFlags.dir = dir
	scaffoldFlags.force = false
	defer func() {
		scaffoldFlags = struct {
			lang       string
			framework  string
			version    string
			extensions string
			port       int
			dir        string
			force      bool
		}{}
	}()
	if err := runScaffold(nil, nil); err == nil {
		t.Fatal("expected error: Dockerfile already exists")
	}
}

func TestRunScaffoldForceOverwrites(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	scaffoldFlags.lang = "php"
	scaffoldFlags.framework = "laravel"
	scaffoldFlags.dir = dir
	scaffoldFlags.force = true
	defer func() {
		scaffoldFlags = struct {
			lang       string
			framework  string
			version    string
			extensions string
			port       int
			dir        string
			force      bool
		}{}
	}()
	if err := runScaffold(nil, nil); err != nil {
		t.Fatalf("err: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "Dockerfile"))
	if !strings.Contains(string(data), "dunglas/frankenphp") {
		t.Errorf("Dockerfile not overwritten: %q", data)
	}
}

func TestProjectName(t *testing.T) {
	cases := map[string]string{
		"/path/to/My App": "my-app",
		"./foo":           "foo",
		"":                "", // resolved to cwd, can't predict
	}
	for in, want := range cases {
		got := projectName(in)
		if want != "" && got != want {
			t.Errorf("projectName(%q) = %q, want %q", in, got, want)
		}
	}
}
