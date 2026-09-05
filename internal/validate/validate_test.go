package validate

import (
	"strings"
	"testing"
)

func TestDomain(t *testing.T) {
	valid := []string{
		"example.com",
		"my-app.test",
		"sub.domain.local",
		"a.b.c.d.example.com",
		"x.test",
	}
	for _, d := range valid {
		if err := Domain(d); err != nil {
			t.Errorf("Domain(%q) = %v, want nil", d, err)
		}
	}

	invalid := []string{
		"",                      // empty
		"evil.test/127.0.0.1",   // slash — dnsmasq directive injection
		"a b.test",              // space
		"x.test\nserver=/e/1",   // embedded newline
		"-leading-hyphen.test",  // label starts with hyphen
		"trailing-hyphen-.test", // label ends with hyphen
		"under_score.test",      // underscore not allowed in hostname
		"*.wildcard.test",       // asterisk
		"a..b.test",             // empty label
	}
	for _, d := range invalid {
		if err := Domain(d); err == nil {
			t.Errorf("Domain(%q) = nil, want error", d)
		}
	}
}

func TestDomainTooLong(t *testing.T) {
	// 64-char label exceeds the 63-char label limit.
	long := ""
	var longSb40 strings.Builder
	for range 64 {
		longSb40.WriteString("a")
	}
	long += longSb40.String()
	if err := Domain(long + ".test"); err == nil {
		t.Error("expected error for over-length label")
	}
}

func TestNoTraversal(t *testing.T) {
	ok := []string{"site", "_proxy-blog", "redirect-foo", "a.b.c"}
	for _, s := range ok {
		if err := NoTraversal(s); err != nil {
			t.Errorf("NoTraversal(%q) = %v, want nil", s, err)
		}
	}

	bad := []string{
		"",           // empty
		"..",         // parent dir
		"../etc",     // traversal
		"a/b",        // forward slash
		`a\b`,        // backslash
		"foo/../bar", // embedded traversal
	}
	for _, s := range bad {
		if err := NoTraversal(s); err == nil {
			t.Errorf("NoTraversal(%q) = nil, want error", s)
		}
	}
}

func TestPort(t *testing.T) {
	for _, p := range []int{1, 80, 443, 8080, 65535} {
		if err := Port(p); err != nil {
			t.Errorf("Port(%d) = %v, want nil", p, err)
		}
	}
	for _, p := range []int{0, -1, 65536, 99999} {
		if err := Port(p); err == nil {
			t.Errorf("Port(%d) = nil, want error", p)
		}
	}
}

func TestPortString(t *testing.T) {
	if err := PortString("8080"); err != nil {
		t.Errorf("PortString(8080) = %v, want nil", err)
	}
	for _, p := range []string{"", "abc", "70000", "-1"} {
		if err := PortString(p); err == nil {
			t.Errorf("PortString(%q) = nil, want error", p)
		}
	}
}

func TestSiteName(t *testing.T) {
	for _, n := range []string{"blog", "my-site", "site_1", "A1"} {
		if err := SiteName(n); err != nil {
			t.Errorf("SiteName(%q) = %v, want nil", n, err)
		}
	}
	for _, n := range []string{"", "-leading", "has space", "dots.notallowed", "a/b"} {
		if err := SiteName(n); err == nil {
			t.Errorf("SiteName(%q) = nil, want error", n)
		}
	}
}

func TestContainerName(t *testing.T) {
	valid := []string{"web", "web-1", "web_1", "app.db", "a", "A1", "srv_static_abc123"}
	for _, n := range valid {
		if err := ContainerName(n); err != nil {
			t.Errorf("ContainerName(%q) = %v, want nil", n, err)
		}
	}
	// Leading punctuation, spaces and shell metacharacters must be rejected:
	// these names reach `docker exec` argument lists.
	invalid := []string{"", "-web", "_web", ".web", "web 1", "web/1", "web;rm", "web$(x)", "wéb"}
	for _, n := range invalid {
		if err := ContainerName(n); err == nil {
			t.Errorf("ContainerName(%q) = nil, want an error", n)
		}
	}
}

func TestProxyNameAcceptsDomainShapes(t *testing.T) {
	// Proxy names are routinely derived from domains, so periods are legal
	// here even though SiteName rejects them.
	valid := []string{"myapp", "myapp.com", "api.myapp.test", "a-b.c"}
	for _, n := range valid {
		if err := ProxyName(n); err != nil {
			t.Errorf("ProxyName(%q) = %v, want nil", n, err)
		}
	}
	invalid := []string{"", "-myapp", "myapp-.com", "my app", "my/app", ".myapp", "myapp..com"}
	for _, n := range invalid {
		if err := ProxyName(n); err == nil {
			t.Errorf("ProxyName(%q) = nil, want an error", n)
		}
	}
}

func TestProxyNameTooLong(t *testing.T) {
	long := strings.Repeat("a", 254)
	if err := ProxyName(long); err == nil {
		t.Error("ProxyName() accepted a 254-character name, want a length error")
	}
}

// SiteName and ProxyName differ deliberately: a site name becomes a directory
// and a compose project, a proxy name becomes a route key. Pin the difference
// so neither drifts into the other.
func TestSiteNameRejectsWhatProxyNameAllows(t *testing.T) {
	const dotted = "myapp.com"
	if err := ProxyName(dotted); err != nil {
		t.Errorf("ProxyName(%q) = %v, want nil", dotted, err)
	}
	if err := SiteName(dotted); err == nil {
		t.Errorf("SiteName(%q) = nil, want a rejection", dotted)
	}
}

// An over-long label is rejected. Note which check does it: domainRegex caps a
// label at 63 characters on its own, so the explicit per-label loop in Domain
// never fires and is defence in depth rather than the active guard. Asserting
// only on rejection keeps this test honest about that.
func TestDomainRejectsOverLongLabel(t *testing.T) {
	if err := Domain(strings.Repeat("a", 64) + ".test"); err == nil {
		t.Fatal("Domain() accepted a 64-character label, want an error")
	}
}

func TestDomainAcceptsMaximumLabel(t *testing.T) {
	if err := Domain(strings.Repeat("a", 63) + ".test"); err != nil {
		t.Errorf("Domain() rejected a legal 63-character label: %v", err)
	}
}

func TestSiteNameEmptyAndTooLong(t *testing.T) {
	if err := SiteName(""); err == nil {
		t.Error("SiteName(\"\") = nil, want an error")
	}
	if err := SiteName(strings.Repeat("a", 64)); err == nil {
		t.Error("SiteName() accepted a 64-character name, want a length error")
	}
	if err := SiteName(strings.Repeat("a", 63)); err != nil {
		t.Errorf("SiteName() rejected a legal 63-character name: %v", err)
	}
}

func TestProxyNameEmpty(t *testing.T) {
	if err := ProxyName(""); err == nil {
		t.Error("ProxyName(\"\") = nil, want an error")
	}
}

// Domain feeds dnsmasq `address=/<domain>/` lines and Traefik Host() rules
// verbatim, so anything that could terminate or extend those tokens has to be
// rejected at this boundary rather than escaped downstream.
func TestDomainBlocksConfigInjection(t *testing.T) {
	for _, d := range []string{
		"a.test/../b", "a.test b.test", "a.test\nb.test", "a.test/",
		"/a.test", "a.test`", "a.test`)||Host(`b.test", "a test",
	} {
		if err := Domain(d); err == nil {
			t.Errorf("Domain(%q) = nil, want a rejection", d)
		}
	}
}
