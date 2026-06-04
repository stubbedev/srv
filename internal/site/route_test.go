package site

import "testing"

func TestAutoRouteID(t *testing.T) {
	cases := []struct{ path, regex, want string }{
		{"/app", "", "app"},
		{"/api/v1", "", "api-v1"},
		{"/api//v1", "", "api-v1"},
		{"", "^/v1/(.+)$", "v1"},
		{"/", "", ""},
		{"/api-foo_bar", "", "api-foo-bar"},
	}
	for _, c := range cases {
		if got := autoRouteID(c.path, c.regex); got != c.want {
			t.Errorf("autoRouteID(%q, %q) = %q, want %q", c.path, c.regex, got, c.want)
		}
	}
}

func TestSplitContainerPort(t *testing.T) {
	cases := []struct {
		in      string
		name    string
		port    int
		wantErr bool
	}{
		{"redis:6379", "redis", 6379, false},
		{"app:3000", "app", 3000, false},
		{"missing", "", 0, true},
		{"x:notnum", "", 0, true},
		{"x:0", "", 0, true},
	}
	for _, c := range cases {
		name, port, err := SplitContainerPort(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("SplitContainerPort(%q) expected err", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("SplitContainerPort(%q) err: %v", c.in, err)
			continue
		}
		if name != c.name || port != c.port {
			t.Errorf("SplitContainerPort(%q) = (%q, %d), want (%q, %d)", c.in, name, port, c.name, c.port)
		}
	}
}

func TestBuildRoute(t *testing.T) {
	// Positive: port upstream, id derived from path.
	r, err := BuildRoute(RouteInput{Path: "/api", Port: 8080})
	if err != nil {
		t.Fatal(err)
	}
	if r.ID != "api" || r.Upstream.Kind != "localhost" || r.Upstream.Port != 8080 {
		t.Errorf("got %+v", r)
	}
	if r.PreserveHost == nil || !*r.PreserveHost {
		t.Error("PreserveHost should default to true")
	}

	// Negative cases.
	bad := []RouteInput{
		{Path: "/a", PathRegex: "x", Port: 80},   // both path forms
		{Port: 80},                               // neither path form
		{Path: "/a", Rewrite: "/b", Port: 80},    // rewrite without regex
		{PathRegex: "(?P<", Port: 80},            // invalid regex
		{Path: "/a"},                             // no upstream
		{Path: "/a", Port: 80, URL: "https://x"}, // multiple upstreams
		{Path: "/a", Port: 80, ID: "Bad_ID!"},    // invalid id
	}
	for i, in := range bad {
		if _, err := BuildRoute(in); err == nil {
			t.Errorf("case %d: expected error for %+v", i, in)
		}
	}
}
