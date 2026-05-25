package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseVolumeSpecBasic(t *testing.T) {
	dir := t.TempDir()
	spec := dir + ":/mnt/data"
	m, err := ParseVolumeSpec(spec)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if m.Source != dir || m.Target != "/mnt/data" || m.ReadOnly {
		t.Errorf("got %+v", m)
	}
}

func TestParseVolumeSpecReadOnly(t *testing.T) {
	dir := t.TempDir()
	m, err := ParseVolumeSpec(dir + ":/mnt/data:ro")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !m.ReadOnly {
		t.Errorf("expected ro: %+v", m)
	}
}

func TestParseVolumeSpecRejectsRelativeSource(t *testing.T) {
	if _, err := ParseVolumeSpec("relative/path:/mnt"); err == nil {
		t.Error("expected error for relative source")
	}
}

func TestParseVolumeSpecRejectsRelativeTarget(t *testing.T) {
	dir := t.TempDir()
	if _, err := ParseVolumeSpec(dir + ":relative/target"); err == nil {
		t.Error("expected error for relative target")
	}
}

func TestParseVolumeSpecRejectsMissingSource(t *testing.T) {
	if _, err := ParseVolumeSpec("/nonexistent/path/12345:/mnt"); err == nil {
		t.Error("expected error for missing source")
	}
}

func TestParseVolumeSpecRejectsMalformed(t *testing.T) {
	cases := []string{
		"",
		"/single",
		"/a:/b:/c:/d",
		"/source:/target:invalid",
	}
	for _, c := range cases {
		if _, err := ParseVolumeSpec(c); err == nil {
			t.Errorf("expected error for %q", c)
		}
	}
}

func TestParseVolumeSpecExpandsTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	// Pick a subdir of $HOME that exists — t.TempDir() is outside $HOME.
	// Create one explicitly.
	sub := filepath.Join(home, ".srv-test-tilde")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Skip("cannot create home subdir")
	}
	defer os.Remove(sub) //nolint:errcheck

	m, err := ParseVolumeSpec("~/.srv-test-tilde:/mnt")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.HasPrefix(m.Source, home) {
		t.Errorf("expected source under HOME, got %q", m.Source)
	}
}
