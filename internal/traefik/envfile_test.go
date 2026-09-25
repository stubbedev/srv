package traefik

import (
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
