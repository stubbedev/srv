// Package site handles site management operations.
package site

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/srv/internal/constants"
)

// =============================================================================
// IsPHPFile
// =============================================================================

func TestIsPHPFile(t *testing.T) {
	tests := []struct {
		filename string
		want     bool
	}{
		{"index.php", true},
		{"page.phtml", true},
		{"INDEX.PHP", true},  // case-insensitive
		{"PAGE.PHTML", true}, // case-insensitive
		{"style.css", false},
		{"script.js", false},
		{"image.png", false},
		{"index.html", false},
		{"README.md", false},
		{"php", false}, // no extension
		{".php", true}, // dotfile with .php extension
	}
	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			got := IsPHPFile(tt.filename)
			if got != tt.want {
				t.Errorf("IsPHPFile(%q) = %v, want %v", tt.filename, got, tt.want)
			}
		})
	}
}

// =============================================================================
// DetectRawPHPSite
// =============================================================================

func TestDetectRawPHPSite(t *testing.T) {
	tests := []struct {
		name  string
		files []string
		want  bool
	}{
		{
			name:  "empty directory",
			files: []string{},
			want:  false,
		},
		{
			name:  "only html files",
			files: []string{"index.html", "style.css"},
			want:  false,
		},
		{
			name:  "index.php present (fast path)",
			files: []string{"index.php"},
			want:  true,
		},
		{
			name:  "index.phtml present (fast path)",
			files: []string{"index.phtml"},
			want:  true,
		},
		{
			name:  "other php file (scan path)",
			files: []string{"contact.php", "style.css"},
			want:  true,
		},
		{
			name:  "phtml file (scan path)",
			files: []string{"about.phtml"},
			want:  true,
		},
		{
			name:  "mixed php and static files",
			files: []string{"index.php", "style.css", "script.js"},
			want:  true,
		},
		{
			name:  "README.md only",
			files: []string{"README.md"},
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, f := range tt.files {
				if err := os.WriteFile(filepath.Join(dir, f), []byte("<?php"), 0o644); err != nil {
					t.Fatalf("failed to create %s: %v", f, err)
				}
			}
			got, err := DetectRawPHPSite(dir)
			if err != nil {
				t.Fatalf("DetectRawPHPSite error: %v", err)
			}
			if got != tt.want {
				t.Errorf("DetectRawPHPSite() = %v, want %v", got, tt.want)
			}
		})
	}
}

// =============================================================================
// DetectPHPSite
// =============================================================================

func TestDetectPHPSite_NoComposerJSON(t *testing.T) {
	dir := t.TempDir()
	info, err := DetectPHPSite(dir)
	if err != nil {
		t.Fatalf("DetectPHPSite error: %v", err)
	}
	if info != nil {
		t.Errorf("expected nil for directory without composer.json, got %+v", info)
	}
}

func TestDetectPHPSite_MalformedComposerJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "composer.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := DetectPHPSite(dir)
	if err != nil {
		t.Fatalf("DetectPHPSite error: %v", err)
	}
	// Malformed JSON falls back to raw PHP defaults.
	if info == nil {
		t.Fatal("expected non-nil PHPSiteInfo for malformed composer.json")
	}
	if info.PHPVersion != constants.PHPVersionLatest {
		t.Errorf("expected PHPVersion %q, got %q", constants.PHPVersionLatest, info.PHPVersion)
	}
}

func TestDetectPHPSite_LaravelProject(t *testing.T) {
	dir := t.TempDir()
	// Write composer.json with Laravel dependency and PHP constraint.
	composerJSON := `{
		"require": {
			"php": "^8.3",
			"laravel/framework": "^10.0",
			"ext-gd": "*",
			"ext-pdo_mysql": "*"
		}
	}`
	if err := os.WriteFile(filepath.Join(dir, "composer.json"), []byte(composerJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	// Create artisan file and public/ directory.
	if err := os.WriteFile(filepath.Join(dir, "artisan"), []byte(""), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "public"), 0o755); err != nil {
		t.Fatal(err)
	}

	info, err := DetectPHPSite(dir)
	if err != nil {
		t.Fatalf("DetectPHPSite error: %v", err)
	}
	if info == nil {
		t.Fatal("expected PHPSiteInfo, got nil")
	}
	if info.Framework != constants.PHPFrameworkLaravel {
		t.Errorf("expected framework %q, got %q", constants.PHPFrameworkLaravel, info.Framework)
	}
	if info.PHPVersion != "8.3" {
		t.Errorf("expected PHPVersion 8.3, got %q", info.PHPVersion)
	}
	if info.DocumentRoot != "public" {
		t.Errorf("expected DocumentRoot 'public', got %q", info.DocumentRoot)
	}
	// Should contain gd and pdo_mysql from ext- requirements.
	extMap := make(map[string]bool)
	for _, e := range info.Extensions {
		extMap[e] = true
	}
	if !extMap["gd"] {
		t.Error("expected 'gd' extension from composer.json")
	}
	if !extMap["pdo_mysql"] {
		t.Error("expected 'pdo_mysql' extension from composer.json")
	}
}

func TestDetectPHPSite_GenericWithoutPHPConstraint(t *testing.T) {
	dir := t.TempDir()
	composerJSON := `{"require": {"vendor/package": "^1.0"}}`
	if err := os.WriteFile(filepath.Join(dir, "composer.json"), []byte(composerJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	info, err := DetectPHPSite(dir)
	if err != nil {
		t.Fatalf("DetectPHPSite error: %v", err)
	}
	if info == nil {
		t.Fatal("expected PHPSiteInfo, got nil")
	}
	if info.PHPVersion != constants.PHPVersionLatest {
		t.Errorf("expected PHPVersion %q (no constraint), got %q", constants.PHPVersionLatest, info.PHPVersion)
	}
}

// =============================================================================
// DetectFramework
// =============================================================================

func TestDetectFramework(t *testing.T) {
	tests := []struct {
		name         string
		files        []string
		dirs         []string
		composerJSON string
		want         string
	}{
		{
			name:  "Laravel: artisan file present",
			files: []string{"artisan"},
			want:  constants.PHPFrameworkLaravel,
		},
		{
			name:  "WordPress: wp-config.php present",
			files: []string{"wp-config.php"},
			want:  constants.PHPFrameworkWordPress,
		},
		{
			name: "WordPress: wp-content directory present",
			dirs: []string{"wp-content"},
			want: constants.PHPFrameworkWordPress,
		},
		{
			name:         "Symfony: bin/console + symfony package",
			files:        []string{"bin/console"},
			composerJSON: `{"require": {"symfony/framework-bundle": "^6.0"}}`,
			want:         constants.PHPFrameworkSymfony,
		},
		{
			name:  "Generic: no framework markers",
			files: []string{"index.php"},
			want:  constants.PHPFrameworkGeneric,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, f := range tt.files {
				full := filepath.Join(dir, f)
				if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(full, []byte(""), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			for _, d := range tt.dirs {
				if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
					t.Fatal(err)
				}
			}

			composer := &ComposerJSON{Require: map[string]string{}}
			if tt.composerJSON != "" {
				// Simple inline parse for test fixtures.
				if strings.Contains(tt.composerJSON, "symfony/") {
					composer.Require["symfony/framework-bundle"] = "^6.0"
				}
			}

			got := DetectFramework(dir, composer)
			if got != tt.want {
				t.Errorf("DetectFramework() = %q, want %q", got, tt.want)
			}
		})
	}
}

// =============================================================================
// DetectDocumentRoot
// =============================================================================

func TestDetectDocumentRoot(t *testing.T) {
	tests := []struct {
		name      string
		framework string
		dirs      []string
		files     []string
		want      string
	}{
		{
			name:      "Laravel with public dir",
			framework: constants.PHPFrameworkLaravel,
			dirs:      []string{"public"},
			want:      "public",
		},
		{
			name:      "Laravel without public dir falls back to root",
			framework: constants.PHPFrameworkLaravel,
			want:      "",
		},
		{
			name:      "Symfony with public dir",
			framework: constants.PHPFrameworkSymfony,
			dirs:      []string{"public"},
			want:      "public",
		},
		{
			name:      "Symfony with web dir (older)",
			framework: constants.PHPFrameworkSymfony,
			dirs:      []string{"web"},
			want:      "web",
		},
		{
			name:      "WordPress always uses root",
			framework: constants.PHPFrameworkWordPress,
			dirs:      []string{"public"},
			want:      "",
		},
		{
			name:      "Generic with public/index.php",
			framework: constants.PHPFrameworkGeneric,
			files:     []string{"public/index.php"},
			want:      "public",
		},
		{
			name:      "Generic with web/index.php",
			framework: constants.PHPFrameworkGeneric,
			files:     []string{"web/index.php"},
			want:      "web",
		},
		{
			name:      "Generic with no subdirectory index",
			framework: constants.PHPFrameworkGeneric,
			files:     []string{"index.php"},
			want:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, d := range tt.dirs {
				if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			for _, f := range tt.files {
				full := filepath.Join(dir, f)
				if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(full, []byte("<?php"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			got := DetectDocumentRoot(dir, tt.framework)
			if got != tt.want {
				t.Errorf("DetectDocumentRoot(%q, %q) = %q, want %q", dir, tt.framework, got, tt.want)
			}
		})
	}
}

// =============================================================================
// ParsePHPVersionFromComposer
// =============================================================================

func TestParsePHPVersionFromComposer(t *testing.T) {
	tests := []struct {
		constraint string
		want       string
	}{
		{"^8.3", "8.3"},
		{"^8.2", "8.2"},
		{">=8.1", "8.1"},
		{">8.0", "8.0"},
		{"~8.3", "8.3"},
		{"8.3.0", "8.3"},
		{"8.3.*", "8.3"},
		{"8.3", "8.3"},
		{"", constants.PHPVersionLatest},
	}

	for _, tt := range tests {
		t.Run(tt.constraint, func(t *testing.T) {
			composer := &ComposerJSON{
				Require: map[string]string{},
			}
			if tt.constraint != "" {
				composer.Require["php"] = tt.constraint
			}
			got := ParsePHPVersionFromComposer(composer)
			if got != tt.want {
				t.Errorf("ParsePHPVersionFromComposer(%q) = %q, want %q", tt.constraint, got, tt.want)
			}
		})
	}
}

func TestParsePHPVersionFromComposer_NoPHPKey(t *testing.T) {
	composer := &ComposerJSON{
		Require: map[string]string{
			"laravel/framework": "^10.0",
		},
	}
	got := ParsePHPVersionFromComposer(composer)
	if got != constants.PHPVersionLatest {
		t.Errorf("expected %q when no php key, got %q", constants.PHPVersionLatest, got)
	}
}

// =============================================================================
// ExtractComposerExtensions
// =============================================================================

func TestExtractComposerExtensions(t *testing.T) {
	composer := &ComposerJSON{
		Require: map[string]string{
			"php":               "^8.3",
			"ext-gd":            "*",
			"ext-pdo_mysql":     "*",
			"ext-redis":         "*",
			"laravel/framework": "^10.0",
		},
	}
	exts := ExtractComposerExtensions(composer)
	extMap := make(map[string]bool)
	for _, e := range exts {
		extMap[e] = true
	}
	if !extMap["gd"] {
		t.Error("expected 'gd'")
	}
	if !extMap["pdo_mysql"] {
		t.Error("expected 'pdo_mysql'")
	}
	if !extMap["redis"] {
		t.Error("expected 'redis'")
	}
	if extMap["php"] {
		t.Error("'php' should not be treated as an extension")
	}
	if extMap["laravel/framework"] {
		t.Error("'laravel/framework' should not be treated as an extension")
	}
}

// =============================================================================
// ParseExtensionOverrides
// =============================================================================

func TestParseExtensionOverrides(t *testing.T) {
	defaults := []string{"gd", "pdo_mysql", "mbstring", "zip"}

	tests := []struct {
		name      string
		flagValue string
		want      []string
	}{
		{
			name:      "empty flag returns defaults",
			flagValue: "",
			want:      []string{"gd", "pdo_mysql", "mbstring", "zip"},
		},
		{
			name:      "full replacement (no modifiers)",
			flagValue: "gd,mysqli",
			want:      []string{"gd", "mysqli"},
		},
		{
			name:      "add extension with +",
			flagValue: "+redis",
			want:      []string{"gd", "mbstring", "pdo_mysql", "redis", "zip"},
		},
		{
			name:      "remove extension with -",
			flagValue: "-gd",
			want:      []string{"mbstring", "pdo_mysql", "zip"},
		},
		{
			name:      "add and remove",
			flagValue: "+redis,-gd",
			want:      []string{"mbstring", "pdo_mysql", "redis", "zip"},
		},
		{
			name:      "remove non-existent is a no-op",
			flagValue: "-nonexistent",
			want:      []string{"gd", "mbstring", "pdo_mysql", "zip"},
		},
		{
			name:      "mixed: unmodified treated as add",
			flagValue: "+redis,opcache,-gd",
			want:      []string{"mbstring", "opcache", "pdo_mysql", "redis", "zip"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseExtensionOverrides(tt.flagValue, defaults)
			if len(got) != len(tt.want) {
				t.Errorf("ParseExtensionOverrides(%q) = %v, want %v", tt.flagValue, got, tt.want)
				return
			}
			for i, v := range got {
				if v != tt.want[i] {
					t.Errorf("ParseExtensionOverrides(%q)[%d] = %q, want %q", tt.flagValue, i, v, tt.want[i])
				}
			}
		})
	}
}

// =============================================================================
// PHPImageTag
// =============================================================================

func TestPHPImageTag(t *testing.T) {
	tests := []struct {
		version string
		want    string
	}{
		{"latest", constants.PHPFPMImageLatest},
		{"", constants.PHPFPMImageLatest},
		{"8.3", "php:8.3-fpm-alpine"},
		{"8.2", "php:8.2-fpm-alpine"},
		{"8.1", "php:8.1-fpm-alpine"},
	}
	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			got := PHPImageTag(tt.version)
			if got != tt.want {
				t.Errorf("PHPImageTag(%q) = %q, want %q", tt.version, got, tt.want)
			}
		})
	}
}

// =============================================================================
// generatePHPNginxConf
// =============================================================================

func TestGeneratePHPNginxConf_ContainsKeyDirectives(t *testing.T) {
	info := &PHPSiteInfo{
		PHPVersion:   "latest",
		Framework:    constants.PHPFrameworkGeneric,
		DocumentRoot: "",
	}
	conf := generatePHPNginxConf(info)

	checks := []string{
		"listen 80",
		"fastcgi_pass php:9000",
		"fastcgi_param SCRIPT_FILENAME",
		"include fastcgi_params",
		`\.(php|phtml)$`,
		"index index.php index.phtml index.html",
	}
	for _, check := range checks {
		if !strings.Contains(conf, check) {
			t.Errorf("nginx conf missing %q\nGot:\n%s", check, conf)
		}
	}
}

func TestGeneratePHPNginxConf_LaravelDocRoot(t *testing.T) {
	info := &PHPSiteInfo{
		Framework:    constants.PHPFrameworkLaravel,
		DocumentRoot: "public",
	}
	conf := generatePHPNginxConf(info)

	if !strings.Contains(conf, "/var/www/html/public") {
		t.Errorf("expected root to include /public, got:\n%s", conf)
	}
	if !strings.Contains(conf, ".env") {
		t.Errorf("expected .env deny block in Laravel config, got:\n%s", conf)
	}
}

func TestGeneratePHPNginxConf_SymfonyOldDocRoot(t *testing.T) {
	info := &PHPSiteInfo{
		Framework:    constants.PHPFrameworkSymfony,
		DocumentRoot: "web",
	}
	conf := generatePHPNginxConf(info)

	if !strings.Contains(conf, "app.php") {
		t.Errorf("expected app.php entry point for old Symfony, got:\n%s", conf)
	}
}

func TestGeneratePHPNginxConf_WordPressArgs(t *testing.T) {
	info := &PHPSiteInfo{
		Framework:    constants.PHPFrameworkWordPress,
		DocumentRoot: "",
	}
	conf := generatePHPNginxConf(info)

	if !strings.Contains(conf, "$args") {
		t.Errorf("expected $args in WordPress try_files, got:\n%s", conf)
	}
}

// =============================================================================
// generatePHPDockerfile
// =============================================================================

func TestGeneratePHPDockerfile_LatestVersion(t *testing.T) {
	info := &PHPSiteInfo{
		PHPVersion: constants.PHPVersionLatest,
		Extensions: []string{"pdo_mysql", "gd"},
	}
	df := generatePHPDockerfile(info)

	if !strings.HasPrefix(df, "# Generated by srv") {
		t.Error("Dockerfile should start with generated comment")
	}
	if !strings.Contains(df, "FROM php:fpm-alpine") {
		t.Errorf("expected FROM php:fpm-alpine, got:\n%s", df)
	}
}

func TestGeneratePHPDockerfile_SpecificVersion(t *testing.T) {
	info := &PHPSiteInfo{
		PHPVersion: "8.3",
		Extensions: []string{"pdo_mysql"},
	}
	df := generatePHPDockerfile(info)

	if !strings.Contains(df, "FROM php:8.3-fpm-alpine") {
		t.Errorf("expected FROM php:8.3-fpm-alpine, got:\n%s", df)
	}
}

func TestGeneratePHPDockerfile_MongoDBViaPECL(t *testing.T) {
	info := &PHPSiteInfo{
		PHPVersion: constants.PHPVersionLatest,
		Extensions: []string{"mongodb"},
	}
	df := generatePHPDockerfile(info)

	if !strings.Contains(df, "pecl install mongodb") {
		t.Errorf("expected PECL install for mongodb, got:\n%s", df)
	}
	if !strings.Contains(df, "docker-php-ext-enable mongodb") {
		t.Errorf("expected docker-php-ext-enable mongodb, got:\n%s", df)
	}
}

func TestGeneratePHPDockerfile_OpcacheConfig(t *testing.T) {
	info := &PHPSiteInfo{
		PHPVersion: constants.PHPVersionLatest,
		Extensions: []string{"opcache"},
	}
	df := generatePHPDockerfile(info)

	if !strings.Contains(df, "opcache.enable=1") {
		t.Errorf("expected opcache configuration, got:\n%s", df)
	}
}

func TestGeneratePHPDockerfile_GDConfigureStep(t *testing.T) {
	info := &PHPSiteInfo{
		PHPVersion: constants.PHPVersionLatest,
		Extensions: []string{"gd"},
	}
	df := generatePHPDockerfile(info)

	if !strings.Contains(df, "docker-php-ext-configure gd") {
		t.Errorf("expected configure step for gd, got:\n%s", df)
	}
	if !strings.Contains(df, "--with-freetype") {
		t.Errorf("expected --with-freetype in gd configure, got:\n%s", df)
	}
}

func TestGeneratePHPDockerfile_BuiltinExtensionsSkipped(t *testing.T) {
	info := &PHPSiteInfo{
		PHPVersion: constants.PHPVersionLatest,
		Extensions: []string{"json", "hash", "openssl", "pdo_mysql"},
	}
	df := generatePHPDockerfile(info)

	// Built-ins (json, hash, openssl) should not appear in install commands.
	// Only pdo_mysql should be in docker-php-ext-install.
	if strings.Contains(df, "docker-php-ext-install json") {
		t.Error("json is built-in, should not be in docker-php-ext-install")
	}
	if !strings.Contains(df, "pdo_mysql") {
		t.Error("pdo_mysql should be installed")
	}
}

func TestGeneratePHPDockerfile_WorkdirSet(t *testing.T) {
	info := &PHPSiteInfo{
		PHPVersion: constants.PHPVersionLatest,
		Extensions: []string{},
	}
	df := generatePHPDockerfile(info)

	if !strings.Contains(df, "WORKDIR /var/www/html") {
		t.Errorf("expected WORKDIR /var/www/html, got:\n%s", df)
	}
}

// =============================================================================
// RawPHPDefaults
// =============================================================================

func TestRawPHPDefaults(t *testing.T) {
	d := RawPHPDefaults()

	if d.PHPVersion != constants.PHPVersionLatest {
		t.Errorf("expected PHPVersion %q, got %q", constants.PHPVersionLatest, d.PHPVersion)
	}
	if d.Framework != constants.PHPFrameworkGeneric {
		t.Errorf("expected Framework %q, got %q", constants.PHPFrameworkGeneric, d.Framework)
	}
	if d.DocumentRoot != "" {
		t.Errorf("expected empty DocumentRoot, got %q", d.DocumentRoot)
	}
	if len(d.Extensions) == 0 {
		t.Error("expected non-empty extension list for raw PHP defaults")
	}
	// mongodb must be in the default set.
	hasMongo := slices.Contains(d.Extensions, "mongodb")
	if !hasMongo {
		t.Error("expected 'mongodb' in default extension set")
	}
}
