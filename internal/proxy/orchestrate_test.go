package proxy

import "testing"

func TestValidateAddSpecPort(t *testing.T) {
	// Positive: localhost port, name derived from domain (dots → dashes).
	name, cn, cp, isContainer, err := validateAddSpec(AddSpec{Domain: "app.test", Port: "8080"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "app-test" {
		t.Errorf("derived name = %q, want app-test", name)
	}
	if isContainer || cn != "" || cp != "" {
		t.Errorf("port spec should not be a container: %q %q %v", cn, cp, isContainer)
	}
}

func TestValidateAddSpecNegative(t *testing.T) {
	cases := []AddSpec{
		{Domain: "app.test"}, // neither port nor container
		{Domain: "app.test", Port: "8080", Container: "x:1"}, // both
		{Domain: "app.test", Port: "notaport"},               // bad port
		{Domain: "bad/domain", Port: "8080"},                 // bad domain
		{Domain: "app.test", Container: "noport"},            // container missing :port
	}
	for i, c := range cases {
		if _, _, _, _, err := validateAddSpec(c); err == nil {
			t.Errorf("case %d: expected error for %+v", i, c)
		}
	}
}

func TestSplitContainer(t *testing.T) {
	host, port, ok := splitContainer("myapp:3000")
	if !ok || host != "myapp" || port != "3000" {
		t.Errorf("splitContainer = %q %q %v", host, port, ok)
	}
	if _, _, ok := splitContainer("noport"); ok {
		t.Error("expected !ok for missing colon")
	}
	if _, _, ok := splitContainer(":3000"); ok {
		t.Error("expected !ok for empty host")
	}
}
