package traefik

import (
	"testing"

	"gopkg.in/yaml.v3"
)

// TestRenderTraefikTemplatePositive: ordinary network/email values land at the
// right paths and the document parses cleanly.
func TestRenderTraefikTemplatePositive(t *testing.T) {
	out, err := renderTraefikTemplate("srv-network", "ops@example.com")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := yaml.Unmarshal(out, &m); err != nil {
		t.Fatalf("rendered template is not valid YAML: %v\n%s", err, out)
	}
	network := m["providers"].(map[string]any)["docker"].(map[string]any)["network"]
	if network != "srv-network" {
		t.Errorf("network = %v, want srv-network", network)
	}
	email := m["certificatesResolvers"].(map[string]any)["letsencrypt"].(map[string]any)["acme"].(map[string]any)["email"]
	if email != "ops@example.com" {
		t.Errorf("email = %v, want ops@example.com", email)
	}
}

// TestRenderTraefikTemplateInjection: a malicious email (the value srv takes
// from the user via `srv start --email`) cannot break the YAML or inject keys.
// Before the yamlpatch rewrite this value was string-substituted into the
// template text and would have produced a broken or attacker-shaped document.
func TestRenderTraefikTemplateInjection(t *testing.T) {
	// Sentinel key that the template does not contain; if it appears at the top
	// level, the email payload escaped its scalar and injected structure.
	malicious := "x@x.com\"\nevilInjectedKey: pwned\nlog:\n  level: DEBUG"
	out, err := renderTraefikTemplate("srv-network", malicious)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := yaml.Unmarshal(out, &m); err != nil {
		t.Fatalf("malicious email broke the document: %v\n%s", err, out)
	}
	if _, leaked := m["evilInjectedKey"]; leaked {
		t.Error("injection leaked a top-level 'evilInjectedKey' via the email field")
	}
	// The email must be stored verbatim as a single scalar.
	email := m["certificatesResolvers"].(map[string]any)["letsencrypt"].(map[string]any)["acme"].(map[string]any)["email"]
	if email != malicious {
		t.Errorf("email mangled:\ngot:  %q\nwant: %q", email, malicious)
	}
}
