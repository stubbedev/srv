package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/srv/internal/site"
)

func TestLimitsFromFlagsNil(t *testing.T) {
	resetAddFlags()
	if got := limitsFromFlags(); got != nil {
		t.Errorf("empty flags -> %+v, want nil", got)
	}
}

func TestLimitsFromFlagsSet(t *testing.T) {
	resetAddFlags()
	addFlags.maxBody = "2G"
	addFlags.readTimeout = "60s"
	defer resetAddFlags()
	got := limitsFromFlags()
	if got == nil || got.MaxBody != "2G" || got.ReadTimeout != "60s" {
		t.Errorf("got %+v", got)
	}
}

func TestDetectionSummaryCompose(t *testing.T) {
	setup := &siteSetup{composePath: "/x/docker-compose.yml"}
	got := detectionSummary(setup)
	if !strings.Contains(got, "compose") {
		t.Errorf("got %q", got)
	}
}

func TestDetectionSummaryNode(t *testing.T) {
	setup := &siteSetup{isNode: true, nodeInfo: &site.NodeSiteInfo{Runtime: "node", PackageManager: "yarn", NodeVersion: "20", Framework: "next"}}
	got := detectionSummary(setup)
	if !strings.Contains(got, "next") {
		t.Errorf("got %q", got)
	}
}

func TestDetectionSummaryNodeBun(t *testing.T) {
	setup := &siteSetup{isNode: true, nodeInfo: &site.NodeSiteInfo{Runtime: "bun", PackageManager: "bun", NodeVersion: "lts", Framework: "generic"}}
	got := detectionSummary(setup)
	if !strings.Contains(got, "bun") {
		t.Errorf("got %q", got)
	}
}

func TestDetectionSummaryRuby(t *testing.T) {
	setup := &siteSetup{isRuby: true, rubyInfo: &site.RubySiteInfo{RubyVersion: "3.3", Framework: "rails"}}
	got := detectionSummary(setup)
	if !strings.Contains(got, "rails") {
		t.Errorf("got %q", got)
	}
}

func TestDetectionSummaryPython(t *testing.T) {
	setup := &siteSetup{isPython: true, pythonInfo: &site.PythonSiteInfo{PythonVersion: "3.12", Framework: "flask"}}
	got := detectionSummary(setup)
	if !strings.Contains(got, "flask") {
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

func TestApplyTypeOverrideNode(t *testing.T) {
	setup, err := applyTypeOverride(&siteSetup{}, t.TempDir(), "node")
	if err != nil {
		t.Fatal(err)
	}
	if !setup.isNode || setup.nodeInfo == nil {
		t.Errorf("got %+v", setup)
	}
}

func TestApplyTypeOverrideRuby(t *testing.T) {
	setup, err := applyTypeOverride(&siteSetup{}, t.TempDir(), "ruby")
	if err != nil {
		t.Fatal(err)
	}
	if !setup.isRuby || setup.rubyInfo == nil {
		t.Errorf("got %+v", setup)
	}
}

func TestApplyTypeOverridePython(t *testing.T) {
	setup, err := applyTypeOverride(&siteSetup{}, t.TempDir(), "python")
	if err != nil {
		t.Fatal(err)
	}
	if !setup.isPython || setup.pythonInfo == nil {
		t.Errorf("got %+v", setup)
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
	addFlags.maxBody = ""
	addFlags.readTimeout = ""
	addFlags.sendTimeout = ""
	addFlags.connectTimeout = ""
	addFlags.service = ""
	addFlags.local = false
	addFlags.wildcard = false
	addFlags.force = false
	addFlags.internalHTTP = false
	addFlags.spa = false
	addFlags.cache = false
	addFlags.cors = false
	addFlags.nodeVersion = ""
	addFlags.typeOverride = ""
	addFlags.aliases = nil
}
