package site

import (
	"sort"
	"strings"
	"testing"

	"github.com/stubbedev/srv/internal/constants"
)

func TestGeneratePHPFPMConfOndemand(t *testing.T) {
	if !strings.Contains(generatePHPFPMConf(true), "pm = ondemand") {
		t.Error("local should use ondemand")
	}
}

func TestGeneratePHPFPMConfDynamic(t *testing.T) {
	if !strings.Contains(generatePHPFPMConf(false), "pm = dynamic") {
		t.Error("non-local should use dynamic")
	}
}

func TestGeneratePHPIni(t *testing.T) {
	out := generatePHPIni()
	if !strings.Contains(out, "memory_limit") {
		t.Error("php.ini missing memory_limit")
	}
	if !strings.Contains(out, "session.gc_maxlifetime") {
		t.Error("php.ini missing session config")
	}
}

func TestGeneratePHPNginxConfBasics(t *testing.T) {
	info := &PHPSiteInfo{PHPVersion: "8.3", Framework: ""}
	out := generatePHPNginxConf(info, nil, "blog", "srv-fpm-x")
	if !strings.Contains(out, "fastcgi_pass srv-fpm-x:9000") {
		t.Error("fastcgi_pass missing")
	}
	if !strings.Contains(out, "/var/www/blog") {
		t.Error("docroot missing")
	}
	if !strings.Contains(out, "client_max_body_size 2G") {
		t.Error("default max body missing")
	}
}

func TestGeneratePHPNginxConfWithLimits(t *testing.T) {
	info := &PHPSiteInfo{PHPVersion: "8.3"}
	limits := &Limits{
		MaxBody:        "100M",
		ConnectTimeout: "30s",
		SendTimeout:    "120s",
		ReadTimeout:    "120s",
	}
	out := generatePHPNginxConf(info, limits, "blog", "srv-fpm-x")
	if !strings.Contains(out, "client_max_body_size 100M") {
		t.Error("custom max body missing")
	}
	if !strings.Contains(out, "fastcgi_connect_timeout 30s") {
		t.Error("connect timeout missing")
	}
	if !strings.Contains(out, "fastcgi_send_timeout 120s") {
		t.Error("send timeout missing")
	}
	if !strings.Contains(out, "fastcgi_read_timeout 120s") {
		t.Error("read timeout missing")
	}
}

func TestGeneratePHPNginxConfWordPress(t *testing.T) {
	info := &PHPSiteInfo{PHPVersion: "8.3", Framework: constants.PHPFrameworkWordPress}
	out := generatePHPNginxConf(info, nil, "blog", "srv-fpm-x")
	if !strings.Contains(out, "$args") {
		t.Error("WordPress try_files should use $args")
	}
}

func TestGeneratePHPNginxConfSymfonyWeb(t *testing.T) {
	info := &PHPSiteInfo{PHPVersion: "8.3", Framework: constants.PHPFrameworkSymfony, DocumentRoot: "web"}
	out := generatePHPNginxConf(info, nil, "site", "fpm")
	if !strings.Contains(out, "app.php") {
		t.Error("Symfony web docroot should use app.php")
	}
}

func TestGeneratePHPNginxConfLaravelDeniesDotenv(t *testing.T) {
	info := &PHPSiteInfo{PHPVersion: "8.3", Framework: constants.PHPFrameworkLaravel}
	out := generatePHPNginxConf(info, nil, "site", "fpm")
	if !strings.Contains(out, `location ~ \.env$`) {
		t.Error("Laravel .env block missing")
	}
}

func TestKnownPHPExtensionsSorted(t *testing.T) {
	exts := KnownPHPExtensions()
	if len(exts) == 0 {
		t.Fatal("empty extension list")
	}
	if !sort.StringsAreSorted(exts) {
		t.Error("KnownPHPExtensions not sorted")
	}
	// Verify caller can't mutate the package-level slice.
	exts[0] = "MUTATED"
	again := KnownPHPExtensions()
	if again[0] == "MUTATED" {
		t.Error("KnownPHPExtensions returned reference to package slice")
	}
}

func TestIsBuiltinPHPExtension(t *testing.T) {
	if !IsBuiltinPHPExtension("json") {
		t.Error("json should be builtin")
	}
	if !IsBuiltinPHPExtension("hash") {
		t.Error("hash should be builtin")
	}
	if IsBuiltinPHPExtension("redis") {
		t.Error("redis should NOT be builtin")
	}
}

func TestNonBuiltinExtensions(t *testing.T) {
	in := []string{"json", "redis", "hash", "imagick"}
	out := nonBuiltinExtensions(in)
	want := map[string]bool{"redis": true, "imagick": true}
	if len(out) != 2 {
		t.Errorf("got %v, want 2 entries", out)
	}
	for _, e := range out {
		if !want[e] {
			t.Errorf("unexpected entry %q", e)
		}
	}
}

func TestGeneratePHPDockerfileDeterministic(t *testing.T) {
	a := generatePHPDockerfile(&PHPSiteInfo{PHPVersion: "8.3", Extensions: []string{"redis", "imagick"}})
	b := generatePHPDockerfile(&PHPSiteInfo{PHPVersion: "8.3", Extensions: []string{"imagick", "redis"}})
	if a != b {
		t.Errorf("Dockerfile not deterministic for unordered extensions")
	}
}

func TestGeneratePHPDockerfileSkipsBuiltin(t *testing.T) {
	out := generatePHPDockerfile(&PHPSiteInfo{PHPVersion: "8.3", Extensions: []string{"json", "redis"}})
	// json is builtin; should not appear as an extension install target.
	lines := strings.Split(out, "\n")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "json" {
			t.Errorf("json (builtin) appears as install target")
		}
	}
	if !strings.Contains(out, "redis") {
		t.Error("redis should appear")
	}
}

func TestGeneratePHPDockerfileIncludesOpcache(t *testing.T) {
	out := generatePHPDockerfile(&PHPSiteInfo{PHPVersion: "8.3"})
	if !strings.Contains(out, "opcache.enable=1") {
		t.Error("opcache config missing")
	}
}

func TestPHPImageFingerprint(t *testing.T) {
	a := PHPImageFingerprint(&PHPSiteInfo{PHPVersion: "8.3", Extensions: []string{"redis"}})
	b := PHPImageFingerprint(&PHPSiteInfo{PHPVersion: "8.3", Extensions: []string{"redis"}})
	if a != b {
		t.Error("fingerprint not stable")
	}
	if !strings.HasPrefix(a, "srv-php:") {
		t.Errorf("fingerprint = %q", a)
	}
	c := PHPImageFingerprint(&PHPSiteInfo{PHPVersion: "8.4", Extensions: []string{"redis"}})
	if a == c {
		t.Error("fingerprint should differ on PHP version")
	}
}

func TestPHPImageFingerprintIgnoresBuiltins(t *testing.T) {
	a := PHPImageFingerprint(&PHPSiteInfo{PHPVersion: "8.3", Extensions: []string{"redis"}})
	b := PHPImageFingerprint(&PHPSiteInfo{PHPVersion: "8.3", Extensions: []string{"redis", "json", "hash"}})
	if a != b {
		t.Errorf("builtin extensions changed fingerprint: %q vs %q", a, b)
	}
}

func TestHasComposerPackagePrefix(t *testing.T) {
	c := &ComposerJSON{Require: map[string]string{"symfony/console": "^6", "psr/log": "^3"}}
	if !hasComposerPackagePrefix(c, "symfony/") {
		t.Error("expected true for symfony/ prefix")
	}
	if hasComposerPackagePrefix(c, "laravel/") {
		t.Error("expected false for laravel/")
	}
}
