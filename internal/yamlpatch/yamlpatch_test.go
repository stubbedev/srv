package yamlpatch

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestParsePath(t *testing.T) {
	segs, err := ParsePath("a.b[0].c")
	if err != nil {
		t.Fatal(err)
	}
	want := []Segment{{Key: "a"}, {Key: "b"}, {IsIndex: true, Idx: 0}, {Key: "c"}}
	if len(segs) != len(want) {
		t.Fatalf("got %d segments, want %d: %+v", len(segs), len(want), segs)
	}
	for i := range want {
		if segs[i] != want[i] {
			t.Errorf("seg %d = %+v, want %+v", i, segs[i], want[i])
		}
	}

	// Negative cases.
	for _, bad := range []string{"", "a..b", "a[", "a[x]"} {
		if _, err := ParsePath(bad); err == nil {
			t.Errorf("ParsePath(%q) = nil error, want error", bad)
		}
	}
}

// roundtrip parses src, applies SetPath(path,val), re-marshals, and re-parses
// into a generic map for assertions.
func roundtrip(t *testing.T, src, path string, val any) map[string]any {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		t.Fatal(err)
	}
	if err := SetPath(&doc, path, val); err != nil {
		t.Fatal(err)
	}
	out, err := Marshal(&doc)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := yaml.Unmarshal(out, &m); err != nil {
		t.Fatalf("re-parse failed (broke the document!): %v\n%s", err, out)
	}
	return m
}

func TestSetPathOverwritesExisting(t *testing.T) {
	m := roundtrip(t, "providers:\n  docker:\n    network: \"\"\n", "providers.docker.network", "srv-net")
	got := m["providers"].(map[string]any)["docker"].(map[string]any)["network"]
	if got != "srv-net" {
		t.Errorf("network = %v, want srv-net", got)
	}
}

func TestSetPathCreatesMissingKeys(t *testing.T) {
	m := roundtrip(t, "providers:\n  docker:\n    network: \"\"\n", "providers.docker.exposedByDefault", false)
	docker := m["providers"].(map[string]any)["docker"].(map[string]any)
	if docker["exposedByDefault"] != false {
		t.Errorf("exposedByDefault = %v, want false", docker["exposedByDefault"])
	}
	// The pre-existing sibling must survive.
	if _, ok := docker["network"]; !ok {
		t.Error("existing key 'network' was lost")
	}
}

// TestSetPathInjectionSafe is the headline negative case: a value that looks
// like YAML (quotes, newlines, sibling keys) must be stored as an opaque scalar
// and must not alter the document structure.
func TestSetPathInjectionSafe(t *testing.T) {
	malicious := "evil@example.com\"\nadmin: true\nproviders:\n  file:\n    directory: /etc/passwd"
	m := roundtrip(t, "certificatesResolvers:\n  letsencrypt:\n    acme:\n      email: \"\"\n", "certificatesResolvers.letsencrypt.acme.email", malicious)

	// The injected `admin:` key must NOT appear at the top level.
	if _, leaked := m["admin"]; leaked {
		t.Error("injection leaked a top-level 'admin' key")
	}
	// The email value must round-trip verbatim as a single scalar.
	acme := m["certificatesResolvers"].(map[string]any)["letsencrypt"].(map[string]any)["acme"].(map[string]any)
	if acme["email"] != malicious {
		t.Errorf("email value mangled:\ngot:  %q\nwant: %q", acme["email"], malicious)
	}
}

func TestSetPathSequenceIndexOutOfRange(t *testing.T) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte("items:\n  - a\n  - b\n"), &doc); err != nil {
		t.Fatal(err)
	}
	// Index 5 is out of range — Set must refuse rather than silently grow it.
	if err := SetPath(&doc, "items[5]", "c"); err == nil {
		t.Error("expected out-of-range index error")
	}
}
