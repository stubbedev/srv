package traefik

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseEnvFile(t *testing.T) {
	env, err := parseEnvFile(strings.NewReader(
		"# comment\n" +
			"\n" +
			"ACME_EMAIL=a@b.com\n" +
			"QUOTED=\"double quoted\"\n" +
			"SINGLE='single quoted'\n" +
			"URL=https://x.test?a=b&c=d\n" +
			"  SPACED  =  trimmed  \n"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"ACME_EMAIL": "a@b.com",
		"QUOTED":     "double quoted",
		"SINGLE":     "single quoted",
		"URL":        "https://x.test?a=b&c=d",
		"SPACED":     "trimmed",
	}
	for k, v := range want {
		if env[k] != v {
			t.Errorf("%s = %q, want %q", k, env[k], v)
		}
	}
	if _, err := parseEnvFile(strings.NewReader("no-equals-sign\n")); err == nil {
		t.Error("expected error for line without =")
	}
}

// writeEnvFile and parseEnvFile must be inverses: the parser strips exactly
// one matching outer quote pair, so values with whitespace, comment markers,
// leading quotes or full single-quote wrapping are written quoted. Unquoted
// emission used to eat those on the next `srv install --email` round-trip.
func TestEnvFileWriteParseRoundTrip(t *testing.T) {
	env := map[string]string{
		"ACME_EMAIL":    "a@b.com",
		"PASS_PHRASE":   "s3cr3t",
		"SPACED":        "two words",
		"HASH_MARKED":   "a#b",
		"EMPTY":         "",
		"LEADING_QUOTE": `"half`,
		"SINGLE_WRAP":   "'wrapped'",
		"EMBEDDED":      `a"b`,
		"TABBED":        "a\tb",
	}
	path := filepath.Join(t.TempDir(), "env.traefik")
	if err := writeEnvFile(path, env); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	back, err := parseEnvFile(f)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range env {
		if back[k] != v {
			t.Errorf("%s round-tripped as %q, want %q", k, back[k], v)
		}
	}
}
