// Package site — php.go owns PHP-site *detection*: inspecting a project
// directory for composer.json or raw .php files, parsing the framework
// (Laravel/Symfony/WordPress), document root, PHP version constraint, and
// the requested extension set. Nothing here writes generated files —
// php_render.go and php_pool.go take that role once detection settles.
package site

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/stubbedev/srv/internal/constants"
)

// PHP Site Detection
// =============================================================================

// PHPSiteInfo holds detected configuration for a PHP project.
type PHPSiteInfo struct {
	PHPVersion   string   // "latest" or a specific version like "8.3"
	Extensions   []string // PHP extensions to install
	Framework    string   // laravel, symfony, wordpress, generic
	DocumentRoot string   // Relative path within project (empty = project root)
}

// ComposerJSON represents the structure of a composer.json file.
type ComposerJSON struct {
	Require map[string]string `json:"require"`
}

// DetectPHPSite checks whether dir contains a composer.json and returns
// PHP site info. Returns nil if it is not a PHP/composer project.
func DetectPHPSite(dir string) (*PHPSiteInfo, error) {
	composerPath := filepath.Join(dir, "composer.json")
	data, err := os.ReadFile(composerPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading composer.json: %w", err)
	}

	var composer ComposerJSON
	if err := json.Unmarshal(data, &composer); err != nil {
		// Malformed composer.json — treat as raw PHP rather than failing hard.
		return RawPHPDefaults(), nil //nolint:nilerr
	}

	framework := DetectFramework(dir, &composer)
	docRoot := DetectDocumentRoot(dir, framework)
	phpVersion := ParsePHPVersionFromComposer(&composer)
	// Start from the common baseline and merge in any ext-* entries from
	// composer.json. This ensures every site gets a usable set of extensions
	// without requiring every project to exhaustively declare them.
	extensions := InjectFrameworkExtensions(framework, mergeExtensions(BaselinePHPExtensions(), ExtractComposerExtensions(&composer)))

	return &PHPSiteInfo{
		PHPVersion:   phpVersion,
		Extensions:   extensions,
		Framework:    framework,
		DocumentRoot: docRoot,
	}, nil
}

// frameworkExtensions lists extensions that are essential for each framework
// but not always declared in composer.json's ext-* requires.
var frameworkExtensions = map[string][]string{
	constants.PHPFrameworkLaravel: {
		"bcmath",   // used by many Laravel packages and numeric operations
		"fileinfo", // required for file uploads and MIME detection
		"intl",     // used by Carbon, string helpers, and localisation
		"opcache",  // production performance; always worth having
		"pcntl",    // required by queue workers and Horizon
	},
}

// InjectFrameworkExtensions merges the framework-specific essential extensions
// into the provided list, deduplicating and sorting the result.
// This ensures common extensions are always present even when not explicitly
// listed in composer.json's ext-* requires.
func InjectFrameworkExtensions(framework string, extensions []string) []string {
	required, ok := frameworkExtensions[framework]
	if !ok {
		return extensions
	}
	set := make(map[string]bool, len(extensions)+len(required))
	for _, e := range extensions {
		set[e] = true
	}
	for _, e := range required {
		set[e] = true
	}
	result := make([]string, 0, len(set))
	for e := range set {
		result = append(result, e)
	}
	sort.Strings(result)
	return result
}

// mergeExtensions returns a sorted, deduplicated union of two extension slices.
func mergeExtensions(a, b []string) []string {
	set := make(map[string]bool, len(a)+len(b))
	for _, e := range a {
		set[e] = true
	}
	for _, e := range b {
		set[e] = true
	}
	result := make([]string, 0, len(set))
	for e := range set {
		result = append(result, e)
	}
	sort.Strings(result)
	return result
}

// DetectRawPHPSite checks whether dir contains PHP files without a
// composer.json. Returns true if any .php or .phtml file is found at the
// top level of dir.
func DetectRawPHPSite(dir string) (bool, error) {
	// Fast path: check for the most common entry points first.
	for _, name := range []string{"index.php", "index.phtml"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true, nil
		}
	}

	// Fallback: scan the top-level directory only (no recursion).
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, fmt.Errorf("reading directory %s: %w", dir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if IsPHPFile(entry.Name()) {
			return true, nil
		}
	}
	return false, nil
}

// IsPHPFile returns true if the filename has a .php or .phtml extension.
func IsPHPFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	return ext == ".php" || ext == ".phtml"
}

// RawPHPDefaults returns PHPSiteInfo with the baseline extension set
// and the latest PHP image tag, used when no composer.json is available.
func RawPHPDefaults() *PHPSiteInfo {
	return &PHPSiteInfo{
		PHPVersion:   constants.PHPVersionLatest,
		Extensions:   BaselinePHPExtensions(),
		Framework:    constants.PHPFrameworkGeneric,
		DocumentRoot: "",
	}
}

// PHPSiteInfoFromMetadata reconstructs a PHPSiteInfo from the persisted
// metadata of a PHP site. Used by reload and regenerate paths where we want
// to re-apply the *site's* configuration rather than re-detect from disk.
func PHPSiteInfoFromMetadata(meta SiteMetadata) *PHPSiteInfo {
	info := &PHPSiteInfo{
		PHPVersion:   meta.PHPVersion,
		Extensions:   meta.PHPExtensions,
		Framework:    meta.PHPFramework,
		DocumentRoot: meta.DocumentRoot,
	}
	if info.PHPVersion == "" {
		info.PHPVersion = constants.PHPVersionLatest
	}
	if info.Framework == "" {
		info.Framework = constants.PHPFrameworkGeneric
	}
	if len(info.Extensions) == 0 {
		info.Extensions = BaselinePHPExtensions()
	}
	return info
}

// BaselinePHPExtensions returns the set of extensions installed for every PHP
// site regardless of what composer.json declares. It covers the most common
// needs out of the box so projects work without having to enumerate every
// ext-* dependency.
func BaselinePHPExtensions() []string {
	return []string{
		// Databases — relational
		"pdo",
		"pdo_mysql",
		"pdo_pgsql",
		"pdo_sqlite",
		"mysqli",
		"pgsql",
		// Databases — NoSQL / cache
		"mongodb",
		"redis",
		// Core string / encoding
		"mbstring",
		"iconv",
		"intl",
		"gettext",
		// XML / HTML
		"xml",
		"simplexml",
		"dom",
		"soap",
		// Networking
		"curl",
		"sockets",
		// File handling
		"zip",
		"fileinfo",
		"ftp",
		// Image processing
		"gd",
		"exif",
		"imagick",
		// Cryptography & security
		"mcrypt",
		"sodium",
		// Math
		"bcmath",
		"gmp",
		// Performance
		"opcache",
		"apcu",
		// Process control (queues, workers)
		"pcntl",
		"posix",
		// Misc commonly needed
		"calendar",
		"shmop",
		"sysvmsg",
		"sysvsem",
		"sysvshm",
	}
}

// =============================================================================
// Framework & Document Root Detection
// =============================================================================

// DetectFramework returns the PHP framework in use based on project files and
// composer dependencies.
func DetectFramework(dir string, composer *ComposerJSON) string {
	// Laravel: has an artisan file at the project root.
	if fileExists(filepath.Join(dir, "artisan")) {
		return constants.PHPFrameworkLaravel
	}

	// WordPress: has wp-config.php or wp-content directory.
	if fileExists(filepath.Join(dir, "wp-config.php")) || dirExists(filepath.Join(dir, "wp-content")) {
		return constants.PHPFrameworkWordPress
	}

	// Symfony: has bin/console and a symfony/* package requirement.
	if fileExists(filepath.Join(dir, "bin", "console")) && hasComposerPackagePrefix(composer, "symfony/") {
		return constants.PHPFrameworkSymfony
	}

	return constants.PHPFrameworkGeneric
}

// DetectDocumentRoot returns the document root path relative to the project
// directory for the given framework.
func DetectDocumentRoot(dir, framework string) string {
	switch framework {
	case constants.PHPFrameworkLaravel:
		if dirExists(filepath.Join(dir, "public")) {
			return "public"
		}
	case constants.PHPFrameworkSymfony:
		// Symfony 4+ uses public/; older versions used web/.
		if dirExists(filepath.Join(dir, "public")) {
			return "public"
		}
		if dirExists(filepath.Join(dir, "web")) {
			return "web"
		}
	case constants.PHPFrameworkWordPress:
		// WordPress is typically served from its root.
		return ""
	default:
		// Generic: prefer public/ or web/ if they contain an index file.
		for _, candidate := range []string{"public", "web", "html"} {
			candidateDir := filepath.Join(dir, candidate)
			if fileExists(filepath.Join(candidateDir, "index.php")) ||
				fileExists(filepath.Join(candidateDir, "index.phtml")) {
				return candidate
			}
		}
	}
	return ""
}

// =============================================================================
// Composer.json Parsing
// =============================================================================

// ParsePHPVersionFromComposer extracts the minimum required PHP version from
// a composer.json Require map. Returns PHPVersionLatest when no PHP
// requirement is declared or the constraint cannot be parsed.
func ParsePHPVersionFromComposer(composer *ComposerJSON) string {
	constraint, ok := composer.Require["php"]
	if !ok || strings.TrimSpace(constraint) == "" {
		return constants.PHPVersionLatest
	}
	if v := parseVersionConstraint(constraint); v != "" {
		return v
	}
	return constants.PHPVersionLatest
}

// ExtractComposerExtensions collects PHP extensions declared in the require
// map (keys beginning with "ext-") and returns them as plain extension names.
func ExtractComposerExtensions(composer *ComposerJSON) []string {
	var exts []string
	for key := range composer.Require {
		if after, ok := strings.CutPrefix(key, "ext-"); ok {
			exts = append(exts, after)
		}
	}
	sort.Strings(exts)
	return exts
}

// parseVersionConstraint converts a composer version constraint string (e.g.
// "^8.3", ">=8.2", "~8.1", "8.3.*") into a simple "major.minor" string
// suitable for use as a Docker image tag. Returns "" on failure.
func parseVersionConstraint(constraint string) string {
	constraint = strings.TrimSpace(constraint)

	// Patterns tried in order: ^8.3 | >=8.2 | >8.2 | ~8.1 | 8.3.0 | 8.3 | 8.3.*
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`^\^(\d+\.\d+)`),
		regexp.MustCompile(`^>=?(\d+\.\d+)`),
		regexp.MustCompile(`^~(\d+\.\d+)`),
		regexp.MustCompile(`^(\d+\.\d+)[\.|\*]?`),
	}
	for _, re := range patterns {
		if m := re.FindStringSubmatch(constraint); len(m) > 1 {
			return m[1]
		}
	}
	return ""
}

// =============================================================================
// Extension Parsing (--php-extensions flag)
// =============================================================================

// ParseExtensionOverrides applies the user-provided --php-extensions flag
// value on top of the defaults slice.
//
// Rules:
//   - Empty string → return defaults unchanged.
//   - Any element starting with "+" → add to defaults.
//   - Any element starting with "-" → remove from defaults.
//   - No +/- modifiers at all → treat the whole list as a full replacement.
//   - Mixed modifiers → start from defaults and apply each +/- in order.
func ParseExtensionOverrides(flagValue string, defaults []string) []string {
	if flagValue == "" {
		return defaults
	}

	parts := strings.Split(flagValue, ",")

	// Detect whether any element carries a +/- modifier.
	hasModifiers := false
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(p, "+") || strings.HasPrefix(p, "-") {
			hasModifiers = true
			break
		}
	}

	if !hasModifiers {
		// Full replacement.
		result := make([]string, 0, len(parts))
		for _, ext := range parts {
			ext = strings.TrimSpace(ext)
			if ext != "" {
				result = append(result, ext)
			}
		}
		sort.Strings(result)
		return result
	}

	// Modifier mode: start from defaults and apply each instruction.
	set := make(map[string]bool, len(defaults))
	for _, ext := range defaults {
		set[ext] = true
	}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		switch {
		case strings.HasPrefix(p, "+"):
			ext := strings.TrimPrefix(p, "+")
			if ext != "" {
				set[ext] = true
			}
		case strings.HasPrefix(p, "-"):
			ext := strings.TrimPrefix(p, "-")
			delete(set, ext)
		default:
			if p != "" {
				set[p] = true
			}
		}
	}

	result := make([]string, 0, len(set))
	for ext := range set {
		result = append(result, ext)
	}
	sort.Strings(result)
	return result
}

// =============================================================================
// PHP Docker image tag helpers
// =============================================================================

// PHPImageTag returns the Docker image tag for the given PHP version string.
// When version is "latest" or empty, it returns the unversioned
// "php:fpm-alpine" tag; otherwise it returns "php:<version>-fpm-alpine".
func PHPImageTag(version string) string {
	if version == "" || version == constants.PHPVersionLatest {
		return constants.PHPFPMImageLatest
	}
	return fmt.Sprintf(constants.PHPFPMImageFormat, version)
}
