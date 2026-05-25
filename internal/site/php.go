// Package site — php.go is the residual PHP-detection layer used solely by
// `srv add` to flag "you have .php files but no Dockerfile" with a helpful
// error pointing at `srv scaffold`. srv no longer manages PHP runtimes, so
// everything that lived here for that purpose (composer.json parsing,
// framework detection, baseline extensions, version constraint parsing) is
// gone. The valet importer carries its own framework detector
// (`detectPHPFrameworkForImport` in cmd/import_valet.go).
package site

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DetectRawPHPSite returns true if dir contains any .php / .phtml file at
// its top level. Used by `srv add` to refuse PHP projects without a
// Dockerfile / docker-compose.yml.
func DetectRawPHPSite(dir string) (bool, error) {
	for _, name := range []string{"index.php", "index.phtml"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true, nil
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, fmt.Errorf("reading directory %s: %w", dir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if isPHPFile(entry.Name()) {
			return true, nil
		}
	}
	return false, nil
}

// isPHPFile returns true if the filename has a .php or .phtml extension.
func isPHPFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	return ext == ".php" || ext == ".phtml"
}
