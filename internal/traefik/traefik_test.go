package traefik

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// writeEnvFile / readEnvFile
// ---------------------------------------------------------------------------

func TestWriteEnvFilePreservesOtherKeys(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "env.traefik")

	// Pre-populate with an unrelated key, then persist a different one and
	// check the original survives (writeEnvFile used to be fed by the
	// credentials generator; the preservation contract now lives here).
	if err := os.WriteFile(envPath, []byte("ACME_EMAIL=me@example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	envMap := readEnvFile(envPath)
	envMap["OTHER_KEY"] = "value"
	if err := writeEnvFile(envPath, envMap); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ACME_EMAIL=me@example.com", "OTHER_KEY=value"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("existing key lost; missing %q; content: %s", want, data)
		}
	}
}

// ---------------------------------------------------------------------------
// mergeTraefikConfigs
// ---------------------------------------------------------------------------

func TestMergeTraefikConfigs_PreservesUserSections(t *testing.T) {
	existing := map[string]any{
		"api": map[string]any{"dashboard": true},
		"log": map[string]any{"level": "DEBUG"},
	}
	template := map[string]any{
		"api":       map[string]any{"dashboard": false},
		"providers": map[string]any{"docker": map[string]any{}},
	}

	merged := mergeTraefikConfigs(existing, template)

	api, ok := merged["api"].(map[string]any)
	if !ok {
		t.Fatal("merged.api is not a map")
	}
	if api["dashboard"] != true {
		t.Errorf("expected dashboard=true (preserved from existing), got %v", api["dashboard"])
	}
}

func TestMergeTraefikConfigs_UsesManagedSectionsFromTemplate(t *testing.T) {
	existing := map[string]any{
		"providers": map[string]any{"file": map[string]any{"directory": "/old"}},
	}
	template := map[string]any{
		"providers": map[string]any{"docker": map[string]any{"endpoint": "unix:///var/run/docker.sock"}},
	}

	merged := mergeTraefikConfigs(existing, template)

	providers, ok := merged["providers"].(map[string]any)
	if !ok {
		t.Fatal("merged.providers is not a map")
	}
	if _, hasDocker := providers["docker"]; !hasDocker {
		t.Error("expected providers.docker from template, not found")
	}
	if _, hasFile := providers["file"]; hasFile {
		t.Error("existing providers.file should have been replaced by template")
	}
}

// ---------------------------------------------------------------------------
// mergeEntryPoints
// ---------------------------------------------------------------------------

func TestMergeEntryPoints_PreservesUserEntrypoints(t *testing.T) {
	existing := map[string]any{
		"entryPoints": map[string]any{
			"metrics": map[string]any{"address": ":9090"},
		},
	}
	template := map[string]any{
		"entryPoints": map[string]any{
			"web":       map[string]any{"address": ":80"},
			"websecure": map[string]any{"address": ":443"},
		},
	}

	result := mergeEntryPoints(existing, template)

	if _, ok := result["metrics"]; !ok {
		t.Error("user entrypoint 'metrics' was lost")
	}
	if _, ok := result["web"]; !ok {
		t.Error("template entrypoint 'web' is missing")
	}
	if _, ok := result["websecure"]; !ok {
		t.Error("template entrypoint 'websecure' is missing")
	}
}

func TestMergeEntryPoints_OverridesWebWebsecure(t *testing.T) {
	existing := map[string]any{
		"entryPoints": map[string]any{
			"web": map[string]any{"address": ":9999"}, // user accidentally changed port
		},
	}
	template := map[string]any{
		"entryPoints": map[string]any{
			"web":       map[string]any{"address": ":80"},
			"websecure": map[string]any{"address": ":443"},
		},
	}

	result := mergeEntryPoints(existing, template)

	web, ok := result["web"].(map[string]any)
	if !ok {
		t.Fatal("web entrypoint missing or wrong type")
	}
	if web["address"] != ":80" {
		t.Errorf("expected web address :80 from template, got %v", web["address"])
	}
}

// ---------------------------------------------------------------------------
// ExtractDomainFromRule
// ---------------------------------------------------------------------------

func TestExtractDomainFromRule(t *testing.T) {
	tests := []struct {
		rule string
		want string
	}{
		{"Host(`example.com`)", "example.com"},
		{"Host(`myapp.test`)", "myapp.test"},
		{"Host(`a.b.c.d`)", "a.b.c.d"},
		{"Host(`example.com`, `www.example.com`)", "example.com"}, // first domain
		{"PathPrefix(`/api`)", ""},                                // no Host rule
		{"", ""},
	}

	for _, tc := range tests {
		got := ExtractDomainFromRule(tc.rule)
		if got != tc.want {
			t.Errorf("ExtractDomainFromRule(%q) = %q, want %q", tc.rule, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// ExtractDomainsFromRule
// ---------------------------------------------------------------------------

func TestExtractDomainsFromRule(t *testing.T) {
	tests := []struct {
		rule string
		want []string
	}{
		{"Host(`a.test`)", []string{"a.test"}},
		{"Host(`a.test`) || Host(`b.test`)", []string{"a.test", "b.test"}},
		{
			"Host(`a.test`) || HostRegexp(`^[^.]+\\.a.test$`) || Host(`b.test`)",
			[]string{"a.test", "b.test"},
		},
		{"PathPrefix(`/x`)", nil},
		{"", nil},
	}
	for _, tc := range tests {
		got := ExtractDomainsFromRule(tc.rule)
		if len(got) != len(tc.want) {
			t.Errorf("ExtractDomainsFromRule(%q) = %v, want %v", tc.rule, got, tc.want)
			continue
		}
		for i, v := range got {
			if v != tc.want[i] {
				t.Errorf("ExtractDomainsFromRule(%q)[%d] = %q, want %q", tc.rule, i, v, tc.want[i])
			}
		}
	}
}

// ---------------------------------------------------------------------------
// BuildHostRule
// ---------------------------------------------------------------------------

func TestBuildHostRule(t *testing.T) {
	tests := []struct {
		name     string
		domains  []string
		wildcard bool
		want     string
	}{
		{
			name:    "single domain, no wildcard",
			domains: []string{"a.test"},
			want:    "Host(`a.test`)",
		},
		{
			name:    "two domains, no wildcard",
			domains: []string{"a.test", "b.test"},
			want:    "Host(`a.test`) || Host(`b.test`)",
		},
		{
			name:     "single domain, wildcard",
			domains:  []string{"a.test"},
			wildcard: true,
			want:     "Host(`a.test`) || HostRegexp(`^[^.]+\\.a\\.test$`)",
		},
		{
			name:     "two domains, wildcard",
			domains:  []string{"a.test", "b.test"},
			wildcard: true,
			want:     "Host(`a.test`) || HostRegexp(`^[^.]+\\.a\\.test$`) || Host(`b.test`) || HostRegexp(`^[^.]+\\.b\\.test$`)",
		},
		{
			name:    "skip empty",
			domains: []string{"a.test", "", "b.test"},
			want:    "Host(`a.test`) || Host(`b.test`)",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := BuildHostRule(tc.domains, tc.wildcard)
			if got != tc.want {
				t.Errorf("BuildHostRule(%v, %v) = %q\nwant %q", tc.domains, tc.wildcard, got, tc.want)
			}
		})
	}
}
