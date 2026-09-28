package traefik

import "testing"

// The golden bytes matter: updateSystemdResolvedConfig skips a disruptive
// systemd-resolved restart only when the rendered file is byte-identical to
// what is on disk, so any format change here costs every user one restart.
// The DNSStubListener=yes line is deliberate: the restart its first rollout
// forces is what re-enables the stub listener on hosts where a Laravel
// Valet install set DNSStubListener=no.
func TestRenderResolvedConfGolden(t *testing.T) {
	got := renderResolvedConf([]string{"~test", "~localhost", "~foo.local"})
	want := "[Resolve]\nDNS=127.0.0.1:15353\nDomains=~test ~localhost ~foo.local\nDNSStubListener=yes\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
