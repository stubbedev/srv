package cmd

import (
	"os"
	"testing"

	"github.com/stubbedev/srv/internal/config"
)

// Shared test helpers for the site/proxy/lifecycle command tests. (The add
// pipeline itself is now tested in internal/site; these helpers survive here
// because other cmd tests still use them.)

// mustLoadConfig loads the srv config under the test's SRV_ROOT, failing the
// test on error.
func mustLoadConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// resetAddFlags clears the package-global add flags between tests.
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

// writeFile2 writes content to path with default perms (test convenience).
func writeFile2(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}
