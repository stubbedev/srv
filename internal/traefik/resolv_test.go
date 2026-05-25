package traefik

import "testing"

func TestLoopbackOnlyResolvConf(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"empty", "", false},
		{"only-comment", "# nothing here\n", false},
		{"single-loopback", "nameserver 127.0.0.1\n", true},
		{"multi-loopback", "nameserver 127.0.0.1\nnameserver ::1\n", true},
		{"loopback-with-comment", "# resolv.conf\n  nameserver 127.0.0.1\n", true},
		{"public-only", "nameserver 1.1.1.1\n", false},
		{"mixed", "nameserver 127.0.0.1\nnameserver 8.8.8.8\n", false},
		{"127.0.53.53", "nameserver 127.0.53.53\n", true}, // systemd-resolved
		{"with-search", "search example.com\nnameserver 127.0.0.1\n", true},
		{"no-nameserver", "search example.com\noptions ndots:2\n", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := loopbackOnlyResolvConf(c.in); got != c.want {
				t.Errorf("loopbackOnlyResolvConf(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestIsLoopback(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1":   true,
		"127.0.53.53": true,
		"::1":         true,
		"1.1.1.1":     false,
		"":            false,
		"::":          false,
	}
	for addr, want := range cases {
		if got := isLoopback(addr); got != want {
			t.Errorf("isLoopback(%q) = %v, want %v", addr, got, want)
		}
	}
}
