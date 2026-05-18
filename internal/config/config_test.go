package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir := t.TempDir()

	// Set SRV_ROOT to our temp directory
	t.Setenv("SRV_ROOT", tmpDir)

	// Reset cache to force reload with new env
	ResetCache()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.Root != tmpDir {
		t.Errorf("Load() Root = %v, want %v", cfg.Root, tmpDir)
	}

	expectedTraefikDir := filepath.Join(tmpDir, "traefik")
	if cfg.TraefikDir != expectedTraefikDir {
		t.Errorf("Load() TraefikDir = %v, want %v", cfg.TraefikDir, expectedTraefikDir)
	}

	expectedSitesDir := filepath.Join(tmpDir, "sites")
	if cfg.SitesDir != expectedSitesDir {
		t.Errorf("Load() SitesDir = %v, want %v", cfg.SitesDir, expectedSitesDir)
	}

	// NetworkName should be based on hostname hash
	if cfg.NetworkName == "" {
		t.Error("Load() NetworkName is empty")
	}
	if len(cfg.NetworkName) < 12 {
		t.Errorf("Load() NetworkName = %v, expected at least 12 chars", cfg.NetworkName)
	}
}

func TestLoadCaching(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SRV_ROOT", tmpDir)
	ResetCache()

	cfg1, err := Load()
	if err != nil {
		t.Fatalf("First Load() failed: %v", err)
	}

	cfg2, err := Load()
	if err != nil {
		t.Fatalf("Second Load() failed: %v", err)
	}

	// Should return the same pointer (cached)
	if cfg1 != cfg2 {
		t.Error("Load() should return cached config")
	}
}

func TestConfigPaths(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SRV_ROOT", tmpDir)
	ResetCache()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	tests := []struct {
		name     string
		got      string
		wantPath string
	}{
		{
			name:     "EnvTraefikPath",
			got:      cfg.EnvTraefikPath(),
			wantPath: filepath.Join(tmpDir, "env.traefik"),
		},
		{
			name:     "TraefikComposePath",
			got:      cfg.TraefikComposePath(),
			wantPath: filepath.Join(tmpDir, "traefik", "docker-compose.yml"),
		},
		{
			name:     "TraefikConfDir",
			got:      cfg.TraefikConfDir(),
			wantPath: filepath.Join(tmpDir, "traefik", "conf"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.wantPath {
				t.Errorf("%s() = %v, want %v", tt.name, tt.got, tt.wantPath)
			}
		})
	}
}

func TestLoadWithInvalidSrvRoot(t *testing.T) {
	// Set SRV_ROOT to a relative path (should fail)
	t.Setenv("SRV_ROOT", "relative/path")
	ResetCache()

	_, err := Load()
	if err == nil {
		t.Error("Load() should fail with relative SRV_ROOT")
	}
}

func TestResetCache(t *testing.T) {
	tmpDir1 := t.TempDir()
	tmpDir2 := t.TempDir()

	t.Setenv("SRV_ROOT", tmpDir1)
	ResetCache()

	cfg1, err := Load()
	if err != nil {
		t.Fatalf("First Load() failed: %v", err)
	}

	// Change env and reset cache
	t.Setenv("SRV_ROOT", tmpDir2)
	ResetCache()

	cfg2, err := Load()
	if err != nil {
		t.Fatalf("Second Load() failed: %v", err)
	}

	if cfg1.Root == cfg2.Root {
		t.Error("ResetCache() didn't clear cache - configs have same Root")
	}
}

func TestGenerateNetworkName(t *testing.T) {
	// generateNetworkName uses hostname internally
	name := generateNetworkName()

	if name == "" {
		t.Error("generateNetworkName() returned empty string")
	}

	// Should end with _traefik
	if !strings.Contains(name, "_traefik") {
		t.Errorf("generateNetworkName() = %v, should contain '_traefik'", name)
	}

	// Should be consistent
	name2 := generateNetworkName()
	if name != name2 {
		t.Errorf("generateNetworkName() not consistent: %v != %v", name, name2)
	}
}

func TestLoadCreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	srvDir := filepath.Join(tmpDir, "new_srv_dir")

	t.Setenv("SRV_ROOT", srvDir)
	ResetCache()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Directory should be created
	if _, err := os.Stat(cfg.Root); os.IsNotExist(err) {
		t.Error("Load() should create the SRV_ROOT directory")
	}
}

func TestConfigPath(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SRV_ROOT", tmpDir)
	ResetCache()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	expected := filepath.Join(tmpDir, "config.yml")
	if cfg.ConfigPath() != expected {
		t.Errorf("ConfigPath() = %v, want %v", cfg.ConfigPath(), expected)
	}
}

func TestLoadUserConfigEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SRV_ROOT", tmpDir)
	ResetCache()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// No config file exists yet
	userCfg, err := cfg.LoadUserConfig()
	if err != nil {
		t.Fatalf("LoadUserConfig() failed: %v", err)
	}

	if len(userCfg.ParkedPaths) > 0 {
		t.Errorf("expected empty ParkedPaths, got %v", userCfg.ParkedPaths)
	}
}

func TestSaveAndLoadUserConfig(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SRV_ROOT", tmpDir)
	ResetCache()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Save user config with parked paths
	userCfg := &UserConfig{
		ParkedPaths: []string{"/path/to/projects", "/another/path"},
	}
	if err := cfg.SaveUserConfig(userCfg); err != nil {
		t.Fatalf("SaveUserConfig() failed: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(cfg.ConfigPath()); os.IsNotExist(err) {
		t.Error("config.yml should exist after SaveUserConfig()")
	}

	// Load it back
	loadedCfg, err := cfg.LoadUserConfig()
	if err != nil {
		t.Fatalf("LoadUserConfig() failed: %v", err)
	}

	if len(loadedCfg.ParkedPaths) != 2 {
		t.Errorf("expected 2 parked paths, got %d", len(loadedCfg.ParkedPaths))
	}
	if loadedCfg.ParkedPaths[0] != "/path/to/projects" {
		t.Errorf("ParkedPaths[0] = %v, want /path/to/projects", loadedCfg.ParkedPaths[0])
	}
	if loadedCfg.ParkedPaths[1] != "/another/path" {
		t.Errorf("ParkedPaths[1] = %v, want /another/path", loadedCfg.ParkedPaths[1])
	}
}

func TestGetParkedPaths(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SRV_ROOT", tmpDir)
	ResetCache()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Initially empty
	paths, err := cfg.GetParkedPaths()
	if err != nil {
		t.Fatalf("GetParkedPaths() failed: %v", err)
	}
	if len(paths) != 0 {
		t.Errorf("expected empty paths, got %v", paths)
	}

	// Set some paths
	if err := cfg.SetParkedPaths([]string{"/foo", "/bar"}); err != nil {
		t.Fatalf("SetParkedPaths() failed: %v", err)
	}

	// Get them back
	paths, err = cfg.GetParkedPaths()
	if err != nil {
		t.Fatalf("GetParkedPaths() failed: %v", err)
	}
	if len(paths) != 2 {
		t.Errorf("expected 2 paths, got %d", len(paths))
	}
}

func TestSetParkedPathsPreservesOtherConfig(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SRV_ROOT", tmpDir)
	ResetCache()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Set initial paths
	if err := cfg.SetParkedPaths([]string{"/initial"}); err != nil {
		t.Fatalf("SetParkedPaths() failed: %v", err)
	}

	// Update paths
	if err := cfg.SetParkedPaths([]string{"/updated", "/paths"}); err != nil {
		t.Fatalf("SetParkedPaths() failed: %v", err)
	}

	// Verify update worked
	paths, err := cfg.GetParkedPaths()
	if err != nil {
		t.Fatalf("GetParkedPaths() failed: %v", err)
	}
	if len(paths) != 2 || paths[0] != "/updated" {
		t.Errorf("expected [/updated, /paths], got %v", paths)
	}
}

func TestSiteCertsDir(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SRV_ROOT", tmpDir)
	ResetCache()
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.SiteCertsDir("blog")
	want := filepath.Join(tmpDir, "sites", "blog", "certs")
	if got != want {
		t.Errorf("SiteCertsDir = %q, want %q", got, want)
	}
}

func TestGetSrvRootXDGConfigHome(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("SRV_ROOT", "")
	t.Setenv("XDG_CONFIG_HOME", xdg)
	ResetCache()
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load err: %v", err)
	}
	want := filepath.Join(xdg, "srv")
	if cfg.Root != want {
		t.Errorf("Root = %q, want %q", cfg.Root, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Errorf("XDG srv dir not created: %v", err)
	}
}

func TestLoadUserConfigInvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SRV_ROOT", tmpDir)
	ResetCache()
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.ConfigPath(), []byte("not: valid: : yaml"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.LoadUserConfig(); err == nil {
		t.Error("expected parse error")
	}
}

func TestAtomicWriteFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := atomicWriteFile(path, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hi" {
		t.Errorf("contents = %q", data)
	}
	// Temp file must be cleaned up after rename.
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("tmp file remains: %v", err)
	}
}

func TestAtomicWriteFileTempCreateFails(t *testing.T) {
	// Parent path doesn't exist — WriteFile on the .tmp will fail.
	path := "/nonexistent-path-srv-cfgtest/should-fail"
	if err := atomicWriteFile(path, []byte("x"), 0o644); err == nil {
		t.Error("expected error when parent dir missing")
	}
}

func TestAtomicWriteFileRenameFails(t *testing.T) {
	dir := t.TempDir()
	// Make the destination path a non-empty directory so Rename fails.
	dest := filepath.Join(dir, "dest")
	if err := os.MkdirAll(filepath.Join(dest, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Place a file inside so the dir is non-empty.
	if err := os.WriteFile(filepath.Join(dest, "sub", "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := atomicWriteFile(dest, []byte("y"), 0o644); err == nil {
		t.Error("expected rename err over non-empty dir")
	}
}

func TestLoadCachesPanicSafe(t *testing.T) {
	// Already covered, but verify config Reset works repeatedly without issue.
	ResetCache()
	ResetCache()
	tmpDir := t.TempDir()
	t.Setenv("SRV_ROOT", tmpDir)
	cfg1, _ := Load()
	cfg2, _ := Load()
	if cfg1 != cfg2 {
		t.Error("Load should return same pointer when cached")
	}
}

func TestSetParkedPathsEmptyList(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SRV_ROOT", tmpDir)
	ResetCache()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Set some paths first
	if err := cfg.SetParkedPaths([]string{"/foo"}); err != nil {
		t.Fatalf("SetParkedPaths() failed: %v", err)
	}

	// Clear paths
	if err := cfg.SetParkedPaths([]string{}); err != nil {
		t.Fatalf("SetParkedPaths() failed: %v", err)
	}

	// Verify cleared
	paths, err := cfg.GetParkedPaths()
	if err != nil {
		t.Fatalf("GetParkedPaths() failed: %v", err)
	}
	if len(paths) != 0 {
		t.Errorf("expected empty paths after clear, got %v", paths)
	}
}
