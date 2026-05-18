package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/site"
)

func TestPromptForDomainPreset(t *testing.T) {
	resetAddFlags()
	addFlags.domain = "blog.local"
	defer resetAddFlags()
	setup := &siteSetup{}
	if err := promptForDomain(setup); err != nil {
		t.Fatal(err)
	}
	if setup.domain != "blog.local" {
		t.Errorf("got %q", setup.domain)
	}
}

func TestPromptForMissingConfigPresetAll(t *testing.T) {
	setupSrvRoot(t)
	resetAddFlags()
	addFlags.domain = "blog.local"
	addFlags.name = "blog"
	addFlags.local = true
	defer resetAddFlags()

	setup := &siteSetup{isStatic: true}
	if err := promptForMissingConfig(setup); err != nil {
		t.Fatal(err)
	}
	if setup.siteName != "blog" || setup.domain != "blog.local" || !setup.isLocal {
		t.Errorf("got %+v", setup)
	}
}

func TestPromptForMissingConfigWildcardWithoutLocal(t *testing.T) {
	setupSrvRoot(t)
	resetAddFlags()
	addFlags.domain = "blog.com"
	addFlags.name = "blog"
	addFlags.wildcard = true
	addFlags.local = false
	defer resetAddFlags()

	setup := &siteSetup{isStatic: true}
	if err := promptForMissingConfig(setup); err == nil {
		t.Error("expected err: --wildcard requires --local")
	}
}

func TestPromptForMissingConfigExistingSiteNoForce(t *testing.T) {
	setupSrvRoot(t)
	resetAddFlags()
	addFlags.domain = "blog.local"
	addFlags.name = "blog"
	addFlags.local = true
	addFlags.force = false
	defer resetAddFlags()

	writeTestSite(t, "blog", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"blog.local"},
		ProjectPath: "/tmp",
		Port:        80,
		NetworkName: "n",
	})

	setup := &siteSetup{isStatic: true}
	if err := promptForMissingConfig(setup); err == nil {
		t.Error("expected err: existing site without --force")
	}
}

func TestPromptForMissingConfigInternalHTTP(t *testing.T) {
	setupSrvRoot(t)
	resetAddFlags()
	addFlags.domain = "blog.local"
	addFlags.name = "blog"
	addFlags.local = true
	addFlags.internalHTTP = true
	defer resetAddFlags()

	setup := &siteSetup{isStatic: true}
	if err := promptForMissingConfig(setup); err != nil {
		t.Fatal(err)
	}
	if !site.HasListener(setup.listeners, constants.ListenerInternal) {
		t.Error("internal listener not set")
	}
}

func TestPromptForMissingConfigPHPOverrides(t *testing.T) {
	setupSrvRoot(t)
	resetAddFlags()
	addFlags.domain = "wp.local"
	addFlags.name = "wp"
	addFlags.local = true
	addFlags.phpVersion = "8.4"
	addFlags.documentRoot = "public"
	addFlags.phpExtensions = "+redis"
	defer resetAddFlags()

	setup := &siteSetup{isPHP: true, phpInfo: &site.PHPSiteInfo{PHPVersion: "8.3", Extensions: []string{"mbstring"}}}
	if err := promptForMissingConfig(setup); err != nil {
		t.Fatal(err)
	}
	if setup.phpInfo.PHPVersion != "8.4" {
		t.Errorf("version not overridden: %q", setup.phpInfo.PHPVersion)
	}
	if setup.phpInfo.DocumentRoot != "public" {
		t.Error("docroot not overridden")
	}
}

func TestPromptForServiceNoCompose(t *testing.T) {
	setup := &siteSetup{}
	if err := promptForService(setup); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestPromptForServiceBadCompose(t *testing.T) {
	setup := &siteSetup{composePath: "/nonexistent-srv-prompt"}
	if err := promptForService(setup); err == nil {
		t.Error("expected err")
	}
}

func TestPromptForServiceSingleAuto(t *testing.T) {
	resetAddFlags()
	defer resetAddFlags()
	dir := t.TempDir()
	path := dir + "/docker-compose.yml"
	body := "services:\n  web:\n    image: nginx\n    container_name: blog-web\n    ports:\n      - 8080:80\n"
	if err := writeTmpFile(path, body); err != nil {
		t.Fatal(err)
	}
	setup := &siteSetup{composePath: path}
	if err := promptForService(setup); err != nil {
		t.Errorf("err: %v", err)
	}
	if setup.serviceName != "blog-web" {
		t.Errorf("got %q", setup.serviceName)
	}
	if setup.composeServiceName != "web" {
		t.Errorf("got %q", setup.composeServiceName)
	}
}

func TestPromptForServiceFlagOverride(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/docker-compose.yml"
	body := "services:\n  web:\n    image: nginx\n  worker:\n    image: redis\n"
	if err := writeTmpFile(path, body); err != nil {
		t.Fatal(err)
	}
	resetAddFlags()
	addFlags.service = "worker"
	defer resetAddFlags()
	setup := &siteSetup{composePath: path}
	if err := promptForService(setup); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestPromptForServiceFlagNotFound(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/docker-compose.yml"
	body := "services:\n  web:\n    image: nginx\n"
	if err := writeTmpFile(path, body); err != nil {
		t.Fatal(err)
	}
	resetAddFlags()
	addFlags.service = "ghost"
	defer resetAddFlags()
	setup := &siteSetup{composePath: path}
	if err := promptForService(setup); err == nil {
		t.Error("expected err: service not found")
	}
}

func TestPromptForServiceSingleWithOneProfile(t *testing.T) {
	resetAddFlags()
	defer resetAddFlags()
	dir := t.TempDir()
	path := dir + "/docker-compose.yml"
	body := "services:\n  web:\n    image: nginx\n    profiles: [dev]\n"
	if err := writeTmpFile(path, body); err != nil {
		t.Fatal(err)
	}
	setup := &siteSetup{composePath: path}
	if err := promptForService(setup); err != nil {
		t.Errorf("err: %v", err)
	}
	if setup.profile != "dev" {
		t.Errorf("expected profile=dev, got %q", setup.profile)
	}
}

func writeTmpFile(path, content string) error {
	return mkAllAndWrite(path, content)
}

func mkAllAndWrite(path, content string) error {
	dir := filepath.Dir(path)
	if err := mkAllDir(dir); err != nil {
		return err
	}
	return writeFile2(path, content)
}

func writeFile2(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

func TestPromptForProfileMissingTTY(t *testing.T) {
	setup := &siteSetup{}
	if err := promptForProfile(setup, []string{"dev", "prod"}); err == nil {
		t.Error("expected TTY err in test env")
	}
}

func TestPromptForMissingConfigNodeOverrides(t *testing.T) {
	setupSrvRoot(t)
	resetAddFlags()
	addFlags.domain = "app.local"
	addFlags.name = "app"
	addFlags.local = true
	addFlags.nodeVersion = "22"
	addFlags.port = 5000
	defer resetAddFlags()

	setup := &siteSetup{isNode: true, nodeInfo: &site.NodeSiteInfo{NodeVersion: "20", Port: 3000}}
	if err := promptForMissingConfig(setup); err != nil {
		t.Fatal(err)
	}
	if setup.nodeInfo.NodeVersion != "22" {
		t.Errorf("version not overridden: %q", setup.nodeInfo.NodeVersion)
	}
	if setup.nodeInfo.Port != 5000 {
		t.Errorf("port not overridden: %d", setup.nodeInfo.Port)
	}
}
