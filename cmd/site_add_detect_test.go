package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateSiteSetupMissing(t *testing.T) {
	if _, err := validateSiteSetup("/nonexistent-srv-detect"); err == nil {
		t.Error("expected err")
	}
}

func TestValidateSiteSetupCompose(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	setup, err := validateSiteSetup(dir)
	if err != nil {
		t.Fatal(err)
	}
	if setup.composePath == "" {
		t.Error("compose path missing")
	}
}

func TestValidateSiteSetupStatic(t *testing.T) {
	dir := t.TempDir()
	setup, err := validateSiteSetup(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !setup.isStatic {
		t.Errorf("expected static, got %+v", setup)
	}
}

// Projects that carry language markers (composer.json, package.json, Gemfile,
// requirements.txt …) but no Dockerfile or docker-compose.yml fall through to
// static — srv doesn't own language runtimes anymore, so the markers are not
// special-cased.
func TestValidateSiteSetupLanguageMarkerFallsThroughToStatic(t *testing.T) {
	for _, marker := range []string{"composer.json", "package.json", "Gemfile", "requirements.txt"} {
		t.Run(marker, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, marker), []byte(""), 0o644); err != nil {
				t.Fatal(err)
			}
			setup, err := validateSiteSetup(dir)
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if !setup.isStatic {
				t.Errorf("expected static fallback, got %+v", setup)
			}
		})
	}
}

func TestValidateSiteSetupDockerfile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM nginx\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	setup, err := validateSiteSetup(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !setup.isDockerfile {
		t.Error("expected dockerfile detection")
	}
}

func TestValidateSiteSetupTypeOverride(t *testing.T) {
	dir := t.TempDir()
	// Put a Dockerfile present so default detection would pick dockerfile.
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM nginx\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	addFlags.typeOverride = "static"
	defer func() { addFlags.typeOverride = "" }()
	setup, err := validateSiteSetup(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !setup.isStatic {
		t.Error("override should force static")
	}
}
