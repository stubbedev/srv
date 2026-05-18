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

func TestValidateSiteSetupPHP(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "composer.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	setup, err := validateSiteSetup(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !setup.isPHP {
		t.Error("expected php detection")
	}
}

func TestValidateSiteSetupNode(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"scripts":{"start":"node ."}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	setup, err := validateSiteSetup(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !setup.isNode {
		t.Error("expected node detection")
	}
}

func TestValidateSiteSetupRuby(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Gemfile"), []byte(`gem "rails"`), 0o644); err != nil {
		t.Fatal(err)
	}
	setup, err := validateSiteSetup(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !setup.isRuby {
		t.Error("expected ruby detection")
	}
}

func TestValidateSiteSetupPython(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("flask\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	setup, err := validateSiteSetup(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !setup.isPython {
		t.Error("expected python detection")
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
