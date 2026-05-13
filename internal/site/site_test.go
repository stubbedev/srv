// Package site handles site management operations.
package site

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestIsLocalDomain(t *testing.T) {
	tests := []struct {
		domain   string
		expected bool
	}{
		{"myapp.test", true},
		{"myapp.localhost", true},
		{"example.com", false},
		{"test.example.com", false},
		{"myapp.dev", false},
		{"foo.test", true},
		{"foo.bar.test", true},
		{"test", false},       // No leading dot
		{"testdomain", false}, // Not a suffix
	}

	for _, tt := range tests {
		t.Run(tt.domain, func(t *testing.T) {
			result := IsLocalDomain(tt.domain)
			if result != tt.expected {
				t.Errorf("IsLocalDomain(%q) = %v, want %v", tt.domain, result, tt.expected)
			}
		})
	}
}

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"MyProject", "myproject"},
		{"my project", "my-project"},
		{"/path/to/my-app", "my-app"},
		{"My App Name", "my-app-name"},
		{"UPPERCASE", "uppercase"},
		{"already-valid", "already-valid"},
		{"/home/user/projects/SomeApp", "someapp"},
		// BUG-06: dots must become hyphens so the name passes ValidateSiteName
		{"myapp.test", "myapp-test"},
		{"v1.2.3", "v1-2-3"},
		{"/home/user/myapp.test", "myapp-test"},
		{"My App.Test", "my-app-test"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := SanitizeName(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizeName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestResolvePath(t *testing.T) {
	// Test absolute path
	t.Run("absolute path", func(t *testing.T) {
		tmpDir := t.TempDir()
		result, err := ResolvePath(tmpDir)
		if err != nil {
			t.Fatalf("ResolvePath failed: %v", err)
		}
		if result != tmpDir {
			t.Errorf("ResolvePath(%q) = %q, want %q", tmpDir, result, tmpDir)
		}
	})

	// Test relative path
	t.Run("relative path", func(t *testing.T) {
		result, err := ResolvePath(".")
		if err != nil {
			t.Fatalf("ResolvePath failed: %v", err)
		}
		if !filepath.IsAbs(result) {
			t.Errorf("expected absolute path, got %q", result)
		}
	})

	// Test non-existent path returns absolute path without error
	t.Run("non-existent path", func(t *testing.T) {
		result, err := ResolvePath("/path/that/does/not/exist")
		if err != nil {
			t.Fatalf("ResolvePath failed: %v", err)
		}
		if result != "/path/that/does/not/exist" {
			t.Errorf("expected absolute path for non-existent, got %q", result)
		}
	})
}

func TestFindComposeFile(t *testing.T) {
	tests := []struct {
		name     string
		files    []string
		expected string
		wantErr  bool
	}{
		{
			name:     "docker-compose.yml",
			files:    []string{"docker-compose.yml"},
			expected: "docker-compose.yml",
		},
		{
			name:     "docker-compose.yaml",
			files:    []string{"docker-compose.yaml"},
			expected: "docker-compose.yaml",
		},
		{
			name:     "compose.yml",
			files:    []string{"compose.yml"},
			expected: "compose.yml",
		},
		{
			name:     "compose.yaml",
			files:    []string{"compose.yaml"},
			expected: "compose.yaml",
		},
		{
			name:     "prefers docker-compose.yml",
			files:    []string{"docker-compose.yml", "compose.yml"},
			expected: "docker-compose.yml",
		},
		{
			name:    "no compose file",
			files:   []string{"README.md", "main.go"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			for _, f := range tt.files {
				path := filepath.Join(tmpDir, f)
				if err := os.WriteFile(path, []byte("test"), 0o644); err != nil {
					t.Fatalf("failed to create file %s: %v", f, err)
				}
			}

			result, err := FindComposeFile(tmpDir)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("FindComposeFile failed: %v", err)
			}
			expectedPath := filepath.Join(tmpDir, tt.expected)
			if result != expectedPath {
				t.Errorf("FindComposeFile() = %q, want %q", result, expectedPath)
			}
		})
	}
}

func TestIsNotFoundError(t *testing.T) {
	t.Run("empty dir returns not-found error", func(t *testing.T) {
		dir := t.TempDir()
		_, err := FindComposeFile(dir)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !IsNotFoundError(err) {
			t.Errorf("IsNotFoundError(%v) = false, want true", err)
		}
	})

	t.Run("wrapped not-found error is detected", func(t *testing.T) {
		wrapped := fmt.Errorf("outer: %w", &composeNotFoundError{dir: "/fake"})
		if !IsNotFoundError(wrapped) {
			t.Errorf("IsNotFoundError on wrapped error = false, want true")
		}
	})

	t.Run("unrelated error is not a not-found error", func(t *testing.T) {
		err := fmt.Errorf("some other error")
		if IsNotFoundError(err) {
			t.Errorf("IsNotFoundError on generic error = true, want false")
		}
	})
}

func TestParseComposeFile(t *testing.T) {
	t.Run("valid compose file with services", func(t *testing.T) {
		tmpDir := t.TempDir()
		content := `services:
  web:
    image: nginx:latest
    labels:
      - traefik.enable=true
  db:
    image: postgres:15
`
		composePath := filepath.Join(tmpDir, "docker-compose.yml")
		if err := os.WriteFile(composePath, []byte(content), 0o644); err != nil {
			t.Fatalf("failed to write compose file: %v", err)
		}

		compose, err := ParseComposeFile(composePath)
		if err != nil {
			t.Fatalf("ParseComposeFile failed: %v", err)
		}

		if len(compose.Services) != 2 {
			t.Errorf("expected 2 services, got %d", len(compose.Services))
		}
		if _, ok := compose.Services["web"]; !ok {
			t.Errorf("expected 'web' service")
		}
		if _, ok := compose.Services["db"]; !ok {
			t.Errorf("expected 'db' service")
		}
	})

	t.Run("compose file with map labels", func(t *testing.T) {
		tmpDir := t.TempDir()
		content := `services:
  web:
    image: nginx:latest
    labels:
      traefik.enable: "true"
      traefik.http.routers.web.rule: "Host('example.com')"
`
		composePath := filepath.Join(tmpDir, "docker-compose.yml")
		if err := os.WriteFile(composePath, []byte(content), 0o644); err != nil {
			t.Fatalf("failed to write compose file: %v", err)
		}

		compose, err := ParseComposeFile(composePath)
		if err != nil {
			t.Fatalf("ParseComposeFile failed: %v", err)
		}

		if len(compose.Services) != 1 {
			t.Errorf("expected 1 service, got %d", len(compose.Services))
		}
	})
}

func TestGetServiceInfos(t *testing.T) {
	tmpDir := t.TempDir()
	content := `services:
  web:
    image: nginx
    container_name: my-web
  api:
    image: node
    profiles:
      - dev
  db:
    image: postgres
    profiles:
      - dev
      - prod
`
	composePath := filepath.Join(tmpDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write compose file: %v", err)
	}

	infos, err := GetServiceInfos(composePath)
	if err != nil {
		t.Fatalf("GetServiceInfos failed: %v", err)
	}

	if len(infos) != 3 {
		t.Errorf("expected 3 services, got %d", len(infos))
	}

	// Build a map for easier testing
	infoMap := make(map[string]ServiceInfo)
	for _, info := range infos {
		infoMap[info.ServiceName] = info
	}

	// Check web service (has container_name, no profiles)
	if web, ok := infoMap["web"]; ok {
		if web.ContainerName != "my-web" {
			t.Errorf("expected web container name 'my-web', got '%s'", web.ContainerName)
		}
		if len(web.Profiles) != 0 {
			t.Errorf("expected web to have no profiles, got %v", web.Profiles)
		}
	} else {
		t.Error("web service not found")
	}

	// Check api service (no container_name, one profile)
	if api, ok := infoMap["api"]; ok {
		if len(api.Profiles) != 1 || api.Profiles[0] != "dev" {
			t.Errorf("expected api profiles [dev], got %v", api.Profiles)
		}
	} else {
		t.Error("api service not found")
	}

	// Check db service (no container_name, two profiles)
	if db, ok := infoMap["db"]; ok {
		if len(db.Profiles) != 2 {
			t.Errorf("expected db to have 2 profiles, got %v", db.Profiles)
		}
	} else {
		t.Error("db service not found")
	}
}

// ---------------------------------------------------------------------------
// Metadata: SiteMetadata.PrimaryDomain + legacy domain migration on read
// ---------------------------------------------------------------------------

func TestSiteMetadata_PrimaryDomain(t *testing.T) {
	m := &SiteMetadata{}
	if got := m.PrimaryDomain(); got != "" {
		t.Errorf("empty Domains: got %q, want \"\"", got)
	}
	m.Domains = []string{"a.test", "b.test"}
	if got := m.PrimaryDomain(); got != "a.test" {
		t.Errorf("PrimaryDomain() = %q, want %q", got, "a.test")
	}
}

func TestSite_Domain(t *testing.T) {
	s := &Site{Domains: []string{"x.test", "y.test"}}
	if got := s.Domain(); got != "x.test" {
		t.Errorf("Site.Domain() = %q, want %q", got, "x.test")
	}
	empty := &Site{}
	if got := empty.Domain(); got != "" {
		t.Errorf("empty Site.Domain() = %q, want \"\"", got)
	}
}

// ---------------------------------------------------------------------------
// HasListener + internal-listener label wiring
// ---------------------------------------------------------------------------

func TestHasListener(t *testing.T) {
	tests := []struct {
		name      string
		listeners []string
		probe     string
		want      bool
	}{
		{"empty", nil, "internal", false},
		{"match", []string{"internal"}, "internal", true},
		{"case-insensitive", []string{"Internal"}, "internal", true},
		{"miss", []string{"web"}, "internal", false},
		{"multiple", []string{"web", "internal", "extra"}, "internal", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := HasListener(tc.listeners, tc.probe); got != tc.want {
				t.Errorf("HasListener(%v, %q) = %v, want %v", tc.listeners, tc.probe, got, tc.want)
			}
		})
	}
}

func TestAddInternalListenerLabels(t *testing.T) {
	labels := buildStaticTraefikLabels("kontainer", []string{"a.test", "b.test"}, true, true)
	addInternalListenerLabels(labels, "kontainer", []string{"a.test", "b.test"}, true)

	wantKeys := []string{
		"traefik.http.routers.kontainer-internal.rule",
		"traefik.http.routers.kontainer-internal.entrypoints",
		"traefik.http.routers.kontainer-internal.service",
	}
	for _, k := range wantKeys {
		if _, ok := labels[k]; !ok {
			t.Errorf("missing label %q", k)
		}
	}
	if labels["traefik.http.routers.kontainer-internal.entrypoints"] != "internal" {
		t.Errorf("expected entrypoints=internal, got %q", labels["traefik.http.routers.kontainer-internal.entrypoints"])
	}
	if labels["traefik.http.routers.kontainer-internal.service"] != "kontainer" {
		t.Errorf("expected service=kontainer (shared with HTTPS router), got %q", labels["traefik.http.routers.kontainer-internal.service"])
	}
	// The internal router must reuse the same multi-host rule as the HTTPS one.
	if labels["traefik.http.routers.kontainer-internal.rule"] != labels["traefik.http.routers.kontainer.rule"] {
		t.Errorf("internal router rule diverged from HTTPS rule")
	}
}
