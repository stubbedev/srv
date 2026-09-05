package ops

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
)

func withRoot(t *testing.T) *config.Config {
	t.Helper()
	t.Setenv(constants.EnvSrvRoot, t.TempDir())
	t.Setenv(constants.EnvContainerEngine, "")
	os.Unsetenv(constants.EnvContainerEngine)
	config.ResetCache()
	t.Cleanup(config.ResetCache)
	ResetEngineCache()
	t.Cleanup(ResetEngineCache)

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// ─── upstream_dns ────────────────────────────────────────────────────────
//
// These land in dnsmasq.conf as `server=<value>`, one per line. A newline
// injects arbitrary directives; anything unparseable stops dnsmasq starting,
// which takes every local domain down with it. This is the check that used to
// be missing entirely.

func TestValidateUpstreamDNSAccepts(t *testing.T) {
	for _, s := range []string{"8.8.8.8", "1.1.1.1", "192.168.1.1#5353", "::1", "2001:4860:4860::8888"} {
		if err := validateUpstreamDNS(s); err != nil {
			t.Errorf("validateUpstreamDNS(%q) = %v, want nil", s, err)
		}
	}
}

func TestValidateUpstreamDNSRejectsInjection(t *testing.T) {
	cases := map[string]string{
		"newline injects a directive": "8.8.8.8\nlog-queries",
		"crlf injects a directive":    "8.8.8.8\r\naddress=/evil.test/1.2.3.4",
		"trailing space":              "8.8.8.8 ",
		"two servers on a line":       "8.8.8.8 8.8.4.4",
		"comma separated":             "8.8.8.8,8.8.4.4",
		"a hostname, not an address":  "dns.example.com",
		"empty":                       "",
		"port out of range":           "8.8.8.8#70000",
		"port not a number":           "8.8.8.8#dns",
		"path-ish":                    "8.8.8.8/24",
	}
	for name, s := range cases {
		if err := validateUpstreamDNS(s); err == nil {
			t.Errorf("%s: validateUpstreamDNS(%q) = nil, want a rejection", name, s)
		}
	}
}

// ─── parked_paths ────────────────────────────────────────────────────────

func TestValidateParkedPath(t *testing.T) {
	if err := validateParkedPath("/srv/projects"); err != nil {
		t.Errorf("absolute path rejected: %v", err)
	}
	for _, p := range []string{"", "relative/path", "./here", "~/projects"} {
		if err := validateParkedPath(p); err == nil {
			t.Errorf("validateParkedPath(%q) = nil, want a rejection", p)
		}
	}
}

// ─── whole-file validation ───────────────────────────────────────────────

func TestValidateUserConfigReportsEveryProblemAtOnce(t *testing.T) {
	err := ValidateUserConfig(&config.UserConfig{
		ContainerEngine: "podmna",
		UpstreamDNS:     []string{"not-an-ip", "8.8.8.8"},
		ParkedPaths:     []string{"relative"},
	})
	if err == nil {
		t.Fatal("ValidateUserConfig() = nil, want errors")
	}
	msg := err.Error()
	for _, want := range []string{"podmna", "not-an-ip", "relative"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not mention %q — a hand-edited file with several\nmistakes should not need one round trip per mistake", msg, want)
		}
	}
}

func TestValidateUserConfigAcceptsEmptyAndNil(t *testing.T) {
	if err := ValidateUserConfig(nil); err != nil {
		t.Errorf("ValidateUserConfig(nil) = %v, want nil", err)
	}
	if err := ValidateUserConfig(&config.UserConfig{}); err != nil {
		t.Errorf("an empty config is the normal first-run state: %v", err)
	}
}

// ─── UpdateUserConfig ────────────────────────────────────────────────────

func TestUpdateUserConfigRoundTrips(t *testing.T) {
	withRoot(t)

	if err := UpdateUserConfig(func(c *config.UserConfig) error {
		c.UpstreamDNS = []string{"1.1.1.1"}
		c.ContainerEngine = "podman"
		return nil
	}); err != nil {
		t.Fatalf("UpdateUserConfig() = %v", err)
	}

	got, err := UserConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got.ContainerEngine != "podman" || len(got.UpstreamDNS) != 1 || got.UpstreamDNS[0] != "1.1.1.1" {
		t.Errorf("round trip lost data: %+v", got)
	}
}

// The point of routing writes through one place: an invalid value never
// reaches the file, so dnsmasq is never handed something that stops it booting.
func TestUpdateUserConfigRefusesToPersistInvalid(t *testing.T) {
	cfg := withRoot(t)

	if err := UpdateUserConfig(func(c *config.UserConfig) error {
		c.UpstreamDNS = []string{"8.8.8.8\nlog-queries"}
		return nil
	}); err == nil {
		t.Fatal("UpdateUserConfig() = nil, want a refusal")
	}

	if _, err := os.Stat(cfg.ConfigPath()); !os.IsNotExist(err) {
		data, _ := os.ReadFile(cfg.ConfigPath())
		t.Errorf("an invalid config was written:\n%s", data)
	}
}

func TestUpdateUserConfigPropagatesMutatorError(t *testing.T) {
	withRoot(t)
	if err := UpdateUserConfig(func(*config.UserConfig) error { return errUserAbort }); !errors.Is(err, errUserAbort) {
		t.Errorf("UpdateUserConfig() = %v, want the mutator's own error", err)
	}
}

var errUserAbort = errors.New("caller changed its mind")

// An already-invalid file must not block the edit that repairs it.
func TestUpdateUserConfigCanFixAnInvalidFile(t *testing.T) {
	cfg := withRoot(t)
	// Write a bad file behind the layer's back, the way a hand edit would.
	if err := os.WriteFile(cfg.ConfigPath(), []byte("upstream_dns:\n  - not-an-ip\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := UserConfig(); err == nil {
		t.Fatal("expected the seeded file to be invalid")
	}

	if err := UpdateUserConfig(func(c *config.UserConfig) error {
		c.UpstreamDNS = []string{"8.8.8.8"}
		return nil
	}); err != nil {
		t.Fatalf("UpdateUserConfig() could not repair an invalid file: %v", err)
	}
	if _, err := UserConfig(); err != nil {
		t.Errorf("config still invalid after repair: %v", err)
	}
}

// ─── UserConfig ──────────────────────────────────────────────────────────

// A missing config.yml is the normal first-run state.
func TestUserConfigWithNoFile(t *testing.T) {
	withRoot(t)
	got, err := UserConfig()
	if err != nil {
		t.Fatalf("UserConfig() = %v on a fresh root, want nil", err)
	}
	if got.ContainerEngine != "" || len(got.UpstreamDNS) != 0 {
		t.Errorf("got %+v, want the zero config", got)
	}
}

// An invalid file still yields a usable value: the engine resolver and the
// dnsmasq writer have to keep working while doctor reports the problem.
func TestUserConfigReturnsValueAlongsideError(t *testing.T) {
	cfg := withRoot(t)
	if err := os.WriteFile(cfg.ConfigPath(), []byte("container_engine: podmna\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := UserConfig()
	if err == nil {
		t.Fatal("UserConfig() = nil error for an invalid file")
	}
	if got == nil {
		t.Fatal("UserConfig() returned no value alongside the error")
	}
}

// ─── UserConfigJSON ──────────────────────────────────────────────────────

// The MCP resource used to emit Go field names because UserConfig carries only
// yaml tags, so an agent reading it would write back keys srv ignores. The
// projection must use the file's own keys.
func TestUserConfigJSONUsesTheFilesOwnKeys(t *testing.T) {
	withRoot(t)
	if err := UpdateUserConfig(func(c *config.UserConfig) error {
		c.ContainerEngine = "podman"
		c.ParkedPaths = []string{"/srv/projects"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	out, err := UserConfigJSON()
	if err != nil {
		t.Fatal(err)
	}
	if out["container_engine"] != "podman" {
		t.Errorf("container_engine = %v", out["container_engine"])
	}
	for _, goName := range []string{"ContainerEngine", "ParkedPaths", "UpstreamDNS"} {
		if _, bad := out[goName]; bad {
			t.Errorf("projection leaked the Go field name %q", goName)
		}
	}
}

// Absent lists render as [] rather than null so a consumer can append without
// a nil check.
func TestUserConfigJSONRendersEmptyListsNotNull(t *testing.T) {
	withRoot(t)
	out, err := UserConfigJSON()
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"parked_paths", "upstream_dns"} {
		v, ok := out[k].([]string)
		if !ok {
			t.Errorf("%s = %#v, want an empty slice", k, out[k])
			continue
		}
		if v == nil {
			t.Errorf("%s is nil, want []", k)
		}
	}
}

// A reader of the resource is exactly who needs to know the file is being
// ignored, so the problem is reported in-band rather than swallowed.
func TestUserConfigJSONReportsInvalidInBand(t *testing.T) {
	cfg := withRoot(t)
	if err := os.WriteFile(cfg.ConfigPath(), []byte("upstream_dns:\n  - nope\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := UserConfigJSON()
	if err != nil {
		t.Fatal(err)
	}
	msg, ok := out["invalid"].(string)
	if !ok || !strings.Contains(msg, "nope") {
		t.Errorf("invalid = %v, want it to name the bad value", out["invalid"])
	}
}

// The projection's keys must match the yaml keys the schema publishes, or the
// resource and the file disagree again.
func TestUserConfigJSONKeysMatchTheYAMLTags(t *testing.T) {
	withRoot(t)
	out, err := UserConfigJSON()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"container_engine", "parked_paths", "upstream_dns"} {
		if _, ok := out[want]; !ok {
			t.Errorf("projection is missing the %q key", want)
		}
	}
}

func TestConfigPathIsUnderTheRoot(t *testing.T) {
	cfg := withRoot(t)
	if filepath.Dir(cfg.ConfigPath()) != cfg.Root {
		t.Errorf("ConfigPath() = %q, want it directly under %q", cfg.ConfigPath(), cfg.Root)
	}
}

// ─── parked paths ────────────────────────────────────────────────────────
//
// These accessors moved here from config.Config, where they wrote the file
// directly. The round-trip cases came with them; the validation case is new.

func TestParkedPathsRoundTrip(t *testing.T) {
	withRoot(t)

	got, err := ParkedPaths()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("ParkedPaths() = %v on a fresh root, want empty", got)
	}

	if err := SetParkedPaths([]string{"/foo", "/bar"}); err != nil {
		t.Fatal(err)
	}
	got, err = ParkedPaths()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "/foo" || got[1] != "/bar" {
		t.Errorf("ParkedPaths() = %v, want [/foo /bar]", got)
	}

	if err := SetParkedPaths([]string{}); err != nil {
		t.Fatal(err)
	}
	if got, _ := ParkedPaths(); len(got) != 0 {
		t.Errorf("ParkedPaths() = %v after clearing, want empty", got)
	}
}

func TestSetParkedPathsPreservesOtherKeys(t *testing.T) {
	withRoot(t)
	if err := UpdateUserConfig(func(c *config.UserConfig) error {
		c.UpstreamDNS = []string{"1.1.1.1"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := SetParkedPaths([]string{"/projects"}); err != nil {
		t.Fatal(err)
	}
	got, err := UserConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.UpstreamDNS) != 1 || got.UpstreamDNS[0] != "1.1.1.1" {
		t.Errorf("SetParkedPaths clobbered upstream_dns: %+v", got)
	}
}

// A relative path would resolve against whatever directory srv was started in,
// so it must not reach the file. The old config.Config accessors would have
// written it.
func TestSetParkedPathsRejectsRelative(t *testing.T) {
	withRoot(t)
	if err := SetParkedPaths([]string{"relative/projects"}); err == nil {
		t.Fatal("SetParkedPaths() = nil for a relative path, want a refusal")
	}
	if got, _ := ParkedPaths(); len(got) != 0 {
		t.Errorf("a relative path was persisted: %v", got)
	}
}
