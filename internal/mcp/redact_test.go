package mcp

import (
	"strings"
	"testing"
)

func TestRedactValueScrubsSensitiveKeys(t *testing.T) {
	in := map[string]any{
		"domain":    "app.test",
		"password":  "hunter2",
		"API_Key":   "abc123",
		"http_pass": "swordfish",
		"nested": map[string]any{
			"token":     "deadbeef",
			"keep_this": "visible",
		},
		"list": []any{
			map[string]any{"secret": "s3cr3t", "ok": "fine"},
		},
	}
	out := redactValue(in).(map[string]any)

	// Positive: non-sensitive values survive.
	if out["domain"] != "app.test" {
		t.Errorf("domain mangled: %v", out["domain"])
	}
	if out["nested"].(map[string]any)["keep_this"] != "visible" {
		t.Error("non-sensitive nested value was redacted")
	}
	// Negative: every sensitive key is scrubbed at any depth.
	for _, p := range []string{
		out["password"].(string),
		out["API_Key"].(string),
		out["http_pass"].(string),
		out["nested"].(map[string]any)["token"].(string),
		out["list"].([]any)[0].(map[string]any)["secret"].(string),
	} {
		if p != redactedPlaceholder {
			t.Errorf("expected %q, got %q", redactedPlaceholder, p)
		}
	}
}

func TestRedactStringScrubsPEMAndInline(t *testing.T) {
	pem := "before\n-----BEGIN RSA PRIVATE KEY-----\nMIIxxx\n-----END RSA PRIVATE KEY-----\nafter"
	got := redactString(pem)
	if strings.Contains(got, "MIIxxx") {
		t.Errorf("PEM body leaked: %q", got)
	}
	if !strings.Contains(got, "before") || !strings.Contains(got, "after") {
		t.Errorf("surrounding text lost: %q", got)
	}

	inline := redactString("HTTP_PASS=swordfish")
	if strings.Contains(inline, "swordfish") {
		t.Errorf("inline secret leaked: %q", inline)
	}
}

func TestRedactMapNilSafe(t *testing.T) {
	if redactMap(nil) != nil {
		t.Error("redactMap(nil) should be nil")
	}
}
