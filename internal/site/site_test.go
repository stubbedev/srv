// Package site handles site management operations.
package site

import (
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
