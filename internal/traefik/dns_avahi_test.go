package traefik

import (
	"strings"
	"testing"
)

func TestDotLocalDomains(t *testing.T) {
	in := []string{"grafana.local", "stubbe.test", "*.wild.local", "local", "app.localhost"}
	got := dotLocalDomains(in)
	want := []string{"grafana.local", "wild.local"} // .test/.localhost/bare-local excluded; wildcard reduced to apex
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("dotLocalDomains = %v, want %v", got, want)
	}
}

func TestRenderAvahiHostsRoundTrip(t *testing.T) {
	// Fresh file: managed block appended.
	out := renderAvahiHosts("", []string{"grafana.local", "traefik.local"})
	if !strings.Contains(out, "127.0.0.1 grafana.local") || !strings.Contains(out, "127.0.0.1 traefik.local") {
		t.Errorf("missing published names:\n%s", out)
	}
	if !strings.Contains(out, avahiManagedStart) || !strings.Contains(out, avahiManagedEnd) {
		t.Errorf("missing managed markers:\n%s", out)
	}

	// User entries outside the block are preserved; the managed block is
	// replaced wholesale (idempotent — no duplicate blocks).
	withUser := "192.168.1.5 nas.local\n" + out
	out2 := renderAvahiHosts(withUser, []string{"grafana.local"})
	if !strings.Contains(out2, "192.168.1.5 nas.local") {
		t.Errorf("user entry lost:\n%s", out2)
	}
	if strings.Count(out2, avahiManagedStart) != 1 {
		t.Errorf("expected exactly one managed block:\n%s", out2)
	}
	if strings.Contains(out2, "traefik.local") {
		t.Errorf("stale managed entry not removed:\n%s", out2)
	}

	// Empty names → managed block removed entirely, user entries kept.
	out3 := renderAvahiHosts(withUser, nil)
	if strings.Contains(out3, avahiManagedStart) || strings.Contains(out3, "grafana.local") {
		t.Errorf("managed block should be gone:\n%s", out3)
	}
	if !strings.Contains(out3, "192.168.1.5 nas.local") {
		t.Errorf("user entry lost on clear:\n%s", out3)
	}
}
