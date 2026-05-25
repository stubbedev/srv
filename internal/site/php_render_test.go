package site

import (
	"sort"
	"strings"
	"testing"
)

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

func TestGeneratePHPDockerfileFrankenPHPBase(t *testing.T) {
	out := generatePHPDockerfile(&PHPSiteInfo{PHPVersion: "8.4", Extensions: []string{"redis"}})
	if !strings.Contains(out, "FROM dunglas/frankenphp:php8.4-alpine") {
		t.Errorf("Dockerfile missing FrankenPHP base image:\n%s", out)
	}
	if !strings.Contains(out, "install-php-extensions") {
		t.Error("Dockerfile missing install-php-extensions")
	}
	if !strings.Contains(out, "redis") {
		t.Error("Dockerfile missing extension")
	}
}

func TestGeneratePHPDockerfileLatestVersion(t *testing.T) {
	out := generatePHPDockerfile(&PHPSiteInfo{PHPVersion: "latest"})
	if !strings.Contains(out, "FROM dunglas/frankenphp:alpine") {
		t.Errorf("latest version should map to dunglas/frankenphp:alpine; got:\n%s", out)
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
	for _, l := range strings.Split(out, "\n") {
		if strings.TrimSpace(l) == "json" {
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

func TestHasComposerPackagePrefix(t *testing.T) {
	c := &ComposerJSON{Require: map[string]string{"symfony/console": "^6", "psr/log": "^3"}}
	if !hasComposerPackagePrefix(c, "symfony/") {
		t.Error("expected true for symfony/ prefix")
	}
	if hasComposerPackagePrefix(c, "laravel/") {
		t.Error("expected false for laravel/")
	}
}
