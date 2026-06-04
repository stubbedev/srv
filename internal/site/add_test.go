package site

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeAddAliases(t *testing.T) {
	tests := []struct {
		name      string
		canonical string
		input     []string
		want      []string
		wantErr   bool
	}{
		{name: "empty input", canonical: "a.test", input: nil, want: []string{}},
		{name: "lowercases and dedupes", canonical: "a.test", input: []string{"B.test", "b.test", "c.test"}, want: []string{"b.test", "c.test"}},
		{name: "rejects canonical clash", canonical: "a.test", input: []string{"A.TEST"}, want: []string{}},
		{name: "skips empty entries", canonical: "a.test", input: []string{"", "b.test", "   "}, want: []string{"b.test"}},
		{name: "rejects invalid alias", canonical: "a.test", input: []string{"not valid"}, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeAddAliases(tc.canonical, tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i, v := range got {
				if v != tc.want[i] {
					t.Errorf("got[%d] = %q, want %q", i, v, tc.want[i])
				}
			}
		})
	}
}

func TestDetectType(t *testing.T) {
	dir := t.TempDir()

	// Auto-detect: empty dir → static.
	s := &addSetup{sitePath: dir}
	if err := detectType(s, ""); err != nil || !s.isStatic {
		t.Errorf("empty dir should detect static: static=%v err=%v", s.isStatic, err)
	}

	// Auto-detect: docker-compose.yml present → compose.
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s = &addSetup{sitePath: dir}
	if err := detectType(s, ""); err != nil || s.composePath == "" {
		t.Errorf("compose file should detect compose: composePath=%q err=%v", s.composePath, err)
	}

	// Override: static even though a compose file exists.
	s = &addSetup{sitePath: dir}
	if err := detectType(s, "static"); err != nil || !s.isStatic {
		t.Errorf("override static failed: static=%v err=%v", s.isStatic, err)
	}

	// Override: unknown type errors.
	if err := detectType(&addSetup{sitePath: dir}, "bogus"); err == nil {
		t.Error("expected error for unknown type override")
	}
}

func TestResolveAddSetupValidation(t *testing.T) {
	withSRVRoot(t)
	dir := t.TempDir() // empty → static site, no docker needed for resolve

	// Negative: missing domain.
	if _, err := resolveAddSetup(AddOptions{Path: dir}); err == nil {
		t.Error("expected error for missing domain")
	}
	// Negative: wildcard without local.
	if _, err := resolveAddSetup(AddOptions{Path: dir, Domain: "x.test", Wildcard: true}); err == nil {
		t.Error("expected error for wildcard without local")
	}
	// Negative: nonexistent path.
	if _, err := resolveAddSetup(AddOptions{Path: "/no/such/dir/srv-test", Domain: "x.test"}); err == nil {
		t.Error("expected error for missing path")
	}

	// Positive: static site, name derived from domain.
	s, err := resolveAddSetup(AddOptions{Path: dir, Domain: "app.test", Local: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.siteName != "app-test" || !s.isStatic {
		t.Errorf("setup = name:%q static:%v", s.siteName, s.isStatic)
	}
}
