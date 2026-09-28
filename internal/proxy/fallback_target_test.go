package proxy

import (
	"errors"
	"net"
	"testing"
)

type fakeDNS struct {
	owned    map[string]bool
	ip       string
	err      error
	resolved int
}

func (f *fakeDNS) Owns(name string) bool { return f.owned[name] }

func (f *fakeDNS) ResolveA(string) (net.IP, error) {
	f.resolved++
	return net.ParseIP(f.ip), f.err
}

func TestFallbackTarget(t *testing.T) {
	cases := []struct {
		name, raw, wantURL, wantSNI string
		wantResolve                 int
	}{
		{"shadowed host pins upstream ip", "https://app.com", "https://203.0.113.7", "app.com", 1},
		{"path and port survive", "https://app.com:8443/x?y=1", "https://203.0.113.7:8443/x?y=1", "app.com", 1},
		{"foreign host unchanged", "https://prod.example.com", "https://prod.example.com", "", 0},
		{"ip literal unchanged", "https://198.51.100.1", "https://198.51.100.1", "", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dns := &fakeDNS{owned: map[string]bool{"app.com": true}, ip: "203.0.113.7"}
			gotURL, gotSNI, err := fallbackTarget(dns, c.raw)
			if err != nil {
				t.Fatal(err)
			}
			if gotURL != c.wantURL || gotSNI != c.wantSNI {
				t.Errorf("fallbackTarget(%q) = %q, %q; want %q, %q", c.raw, gotURL, gotSNI, c.wantURL, c.wantSNI)
			}
			if dns.resolved != c.wantResolve {
				t.Errorf("ResolveA called %d times, want %d", dns.resolved, c.wantResolve)
			}
		})
	}
}

// A shadowed host that cannot be resolved upstream must fail rather than
// render a fallback that loops back into Traefik.
func TestFallbackTargetShadowedUnresolvableFails(t *testing.T) {
	dns := &fakeDNS{owned: map[string]bool{"app.com": true}, err: errors.New("offline")}
	if _, _, err := fallbackTarget(dns, "https://app.com"); err == nil {
		t.Fatal("want an error for an unresolvable shadowed fallback host")
	}
}

func TestProxyRouteSkipsDNSWithoutFallback(t *testing.T) {
	dns := func() (dnsView, error) {
		t.Fatal("DNS consulted for a proxy without a fallback")
		return nil, nil
	}
	route, err := proxyRoute(dns, &Metadata{Name: "a", Domains: []string{"a.test"}}, "http://localhost:1")
	if err != nil {
		t.Fatal(err)
	}
	if route.FallbackURL != "" || route.TargetURL != "http://localhost:1" {
		t.Errorf("unexpected route: %+v", route)
	}
}
