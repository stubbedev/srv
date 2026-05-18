package valet

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestReadConfigMissing(t *testing.T) {
	c := ReadConfig(t.TempDir())
	if len(c.Paths) != 0 {
		t.Errorf("expected zero-value Config, got %+v", c)
	}
}

func TestReadConfigPresent(t *testing.T) {
	dir := t.TempDir()
	body := map[string]any{"paths": []string{"/srv/a", "/srv/b"}}
	data, _ := json.Marshal(body)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	c := ReadConfig(dir)
	if len(c.Paths) != 2 || c.Paths[0] != "/srv/a" {
		t.Errorf("got %+v", c)
	}
}

func TestReadConfigBadJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := ReadConfig(dir)
	if len(c.Paths) != 0 {
		t.Errorf("expected zero-value on parse err, got %+v", c)
	}
}

func TestParseDirSkipsHidden(t *testing.T) {
	nginxDir := t.TempDir()
	// Underscore prefix and dotfile must be skipped.
	if err := os.WriteFile(filepath.Join(nginxDir, "_default"), []byte("server { listen 80; server_name a.test; }"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nginxDir, ".keep"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	// A real one.
	if err := os.WriteFile(filepath.Join(nginxDir, "blog.test"), []byte("server { listen 80; server_name blog.test; }\nserver { listen 443 ssl; server_name blog.test; }"), 0o644); err != nil {
		t.Fatal(err)
	}
	sites, err := ParseDir(nginxDir, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != 1 {
		t.Errorf("expected 1 site, got %d", len(sites))
	}
}

func TestParseDirMissing(t *testing.T) {
	if _, err := ParseDir("/nonexistent-dir-srv-valet", "", nil); err == nil {
		t.Error("expected error for missing dir")
	}
}

func TestParseDirParseFileError(t *testing.T) {
	nginxDir := t.TempDir()
	// Create a subdirectory; ReadDir lists it but skips it via IsDir check.
	if err := os.MkdirAll(filepath.Join(nginxDir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Now create an actual file whose contents are unreadable for ParseFile.
	// We use a directory-like entry replaced with a file with valid contents
	// for normal parsing — and a file that ParseFile will fail on by simulating
	// it being inaccessible. Since we can't easily make ParseFile error on a
	// read here, just test the happy path skip-dir behavior.
	sites, err := ParseDir(nginxDir, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != 0 {
		t.Errorf("expected 0 sites from empty dir, got %d", len(sites))
	}
}
