package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/srv/internal/site"
)

func TestDetectionSummaryCompose(t *testing.T) {
	setup := &siteSetup{composePath: "/x/docker-compose.yml"}
	got := detectionSummary(setup)
	if !strings.Contains(got, "compose") {
		t.Errorf("got %q", got)
	}
}

func TestDetectionSummaryDockerfile(t *testing.T) {
	setup := &siteSetup{isDockerfile: true, dockerfileInfo: &site.DockerfileSiteInfo{Port: 8080}}
	got := detectionSummary(setup)
	if got == "" {
		t.Errorf("expected non-empty: %q", got)
	}
}

func TestDetectionSummaryStatic(t *testing.T) {
	setup := &siteSetup{isStatic: true}
	got := detectionSummary(setup)
	if got == "" {
		t.Error("expected non-empty")
	}
}

func TestApplyTypeOverrideStatic(t *testing.T) {
	setup, err := applyTypeOverride(&siteSetup{}, t.TempDir(), "static")
	if err != nil {
		t.Fatal(err)
	}
	if !setup.isStatic {
		t.Error("isStatic not set")
	}
}

func TestApplyTypeOverridePHPRejected(t *testing.T) {
	if _, err := applyTypeOverride(&siteSetup{}, t.TempDir(), "php"); err == nil {
		t.Fatal("expected error: --type php no longer supported")
	}
}

func TestApplyTypeOverrideNodeRejected(t *testing.T) {
	if _, err := applyTypeOverride(&siteSetup{}, t.TempDir(), "node"); err == nil {
		t.Fatal("expected error: --type node no longer supported")
	}
}

func TestApplyTypeOverrideRubyRejected(t *testing.T) {
	if _, err := applyTypeOverride(&siteSetup{}, t.TempDir(), "ruby"); err == nil {
		t.Fatal("expected error: --type ruby no longer supported")
	}
}

func TestApplyTypeOverridePythonRejected(t *testing.T) {
	if _, err := applyTypeOverride(&siteSetup{}, t.TempDir(), "python"); err == nil {
		t.Fatal("expected error: --type python no longer supported")
	}
}

func TestApplyTypeOverrideDockerfile(t *testing.T) {
	setup, err := applyTypeOverride(&siteSetup{}, t.TempDir(), "dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	if !setup.isDockerfile || setup.dockerfileInfo == nil {
		t.Errorf("got %+v", setup)
	}
}

func TestApplyTypeOverrideComposeMissing(t *testing.T) {
	dir := t.TempDir()
	if _, err := applyTypeOverride(&siteSetup{}, dir, "compose"); err == nil {
		t.Error("expected err: no compose file")
	}
}

func TestApplyTypeOverrideComposeFound(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	setup, err := applyTypeOverride(&siteSetup{}, dir, "compose")
	if err != nil {
		t.Fatal(err)
	}
	if setup.composePath == "" {
		t.Error("composePath should be set")
	}
}

func TestApplyTypeOverrideUnknown(t *testing.T) {
	if _, err := applyTypeOverride(&siteSetup{}, t.TempDir(), "weird"); err == nil {
		t.Error("expected err for unknown type")
	}
}

func TestValidateSiteInputsBadName(t *testing.T) {
	resetAddFlags()
	addFlags.name = "bad name"
	defer resetAddFlags()
	setup := &siteSetup{siteName: "bad name", domain: "x.local", port: 80}
	if err := validateSiteInputs(setup); err == nil {
		t.Error("expected err: bad site name")
	}
}

func TestValidateSiteInputsBadDomain(t *testing.T) {
	resetAddFlags()
	addFlags.domain = "bad domain"
	defer resetAddFlags()
	setup := &siteSetup{siteName: "ok", domain: "bad domain", port: 80}
	if err := validateSiteInputs(setup); err == nil {
		t.Error("expected err: bad domain")
	}
}

func TestValidateSiteInputsBadPort(t *testing.T) {
	resetAddFlags()
	setup := &siteSetup{siteName: "ok", port: 0}
	if err := validateSiteInputs(setup); err == nil {
		t.Error("expected err: bad port")
	}
}

func TestValidateSiteInputsOK(t *testing.T) {
	resetAddFlags()
	setup := &siteSetup{siteName: "ok", port: 80}
	if err := validateSiteInputs(setup); err != nil {
		t.Errorf("err: %v", err)
	}
}

func resetAddFlags() {
	addFlags.name = ""
	addFlags.domain = ""
	addFlags.service = ""
	addFlags.local = false
	addFlags.wildcard = false
	addFlags.force = false
	addFlags.internalHTTP = false
	addFlags.spa = false
	addFlags.cache = false
	addFlags.cors = false
	addFlags.typeOverride = ""
	addFlags.aliases = nil
}
