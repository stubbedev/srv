package validate

import "testing"

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
	for i := 0; i < 64; i++ {
		long += "a"
	}
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
