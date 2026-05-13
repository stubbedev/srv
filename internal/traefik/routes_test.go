package traefik

import (
	"strings"
	"testing"
)

func TestResolveUpstreamURL(t *testing.T) {
	tests := []struct {
		name      string
		kind      string
		container string
		urlStr    string
		port      int
		want      string
		wantErr   string
	}{
		{name: "localhost ok", kind: "localhost", port: 6001, want: "http://host.docker.internal:6001"},
		{name: "localhost requires port", kind: "localhost", wantErr: "requires a port"},
		{name: "container ok", kind: "container", container: "redis", port: 6379, want: "http://redis:6379"},
		{name: "container missing both", kind: "container", wantErr: "requires container and port"},
		{name: "url ok", kind: "url", urlStr: "https://kontainer.com", want: "https://kontainer.com"},
		{name: "url missing", kind: "url", wantErr: "requires url"},
		{name: "url bad scheme", kind: "url", urlStr: "ftp://x", wantErr: "must start with http"},
		{name: "unknown", kind: "bogus", wantErr: "unknown upstream kind"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveUpstreamURL(tc.kind, tc.container, tc.urlStr, tc.port)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("got err=%v, want substring %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRouteMatcher(t *testing.T) {
	tests := []struct {
		name    string
		spec    RouteSpec
		want    string
		wantErr string
	}{
		{
			name: "path prefix",
			spec: RouteSpec{Path: "/app"},
			want: "PathPrefix(`/app`)",
		},
		{
			name: "path regex",
			spec: RouteSpec{PathRegex: `^/videos/([^/]+)/(.+)$`},
			want: "PathRegexp(`^/videos/([^/]+)/(.+)$`)",
		},
		{
			name:    "both",
			spec:    RouteSpec{Path: "/a", PathRegex: "/b"},
			wantErr: "exactly one",
		},
		{
			name:    "neither",
			spec:    RouteSpec{},
			wantErr: "missing path",
		},
		{
			name:    "bad regex",
			spec:    RouteSpec{PathRegex: "(unterminated"},
			wantErr: "invalid path_regex",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := routeMatcher(tc.spec)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("got err=%v, want substring %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
