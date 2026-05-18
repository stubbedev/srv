package cmd

import (
	"testing"

	"github.com/stubbedev/srv/internal/site"
)

func TestAutoRouteID(t *testing.T) {
	cases := []struct {
		path, regex, want string
	}{
		{"/app", "", "app"},
		{"/api/v1", "", "api-v1"},
		{"/api//v1", "", "api-v1"},
		{"", "^/v1/(.+)$", "v1"},
		{"/", "", ""},
		{"/api-foo_bar", "", "api-foo-bar"},
	}
	for _, c := range cases {
		got := autoRouteID(c.path, c.regex)
		if got != c.want {
			t.Errorf("autoRouteID(%q, %q) = %q, want %q", c.path, c.regex, got, c.want)
		}
	}
}

func TestDescribeUpstream(t *testing.T) {
	cases := []struct {
		kind, container, url string
		port                 int
		want                 string
	}{
		{"localhost", "", "", 8080, "localhost:8080"},
		{"container", "redis", "", 6379, "container redis:6379"},
		{"url", "", "https://api.example.com", 0, "https://api.example.com"},
		{"weird", "", "", 0, "(unknown)"},
	}
	for _, c := range cases {
		got := describeUpstream(c.kind, c.container, c.url, c.port)
		if got != c.want {
			t.Errorf("describeUpstream(%+v) = %q, want %q", c, got, c.want)
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
		name, port, err := splitContainerPort(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("splitContainerPort(%q) expected err", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("splitContainerPort(%q) err: %v", c.in, err)
			continue
		}
		if name != c.name || port != c.port {
			t.Errorf("splitContainerPort(%q) = (%q, %d), want (%q, %d)", c.in, name, port, c.name, c.port)
		}
	}
}

func TestBuildRouteFromFlagsPortOnly(t *testing.T) {
	resetRouteFlags()
	routeAddFlags.path = "/api"
	routeAddFlags.port = 8080
	r, err := buildRouteFromFlags()
	if err != nil {
		t.Fatal(err)
	}
	if r.ID != "api" || r.Path != "/api" {
		t.Errorf("got %+v", r)
	}
	if r.Upstream.Kind != "localhost" || r.Upstream.Port != 8080 {
		t.Errorf("upstream wrong: %+v", r.Upstream)
	}
}

func TestBuildRouteFromFlagsContainer(t *testing.T) {
	resetRouteFlags()
	routeAddFlags.path = "/cache"
	routeAddFlags.container = "redis:6379"
	r, err := buildRouteFromFlags()
	if err != nil {
		t.Fatal(err)
	}
	if r.Upstream.Kind != "container" || r.Upstream.Container != "redis" || r.Upstream.Port != 6379 {
		t.Errorf("upstream wrong: %+v", r.Upstream)
	}
}

func TestBuildRouteFromFlagsRegexRewrite(t *testing.T) {
	resetRouteFlags()
	routeAddFlags.pathRegex = "^/v1/(.+)$"
	routeAddFlags.rewrite = "/$1"
	routeAddFlags.url = "https://api.example.com"
	r, err := buildRouteFromFlags()
	if err != nil {
		t.Fatal(err)
	}
	if r.Rewrite != "/$1" || r.PathRegex != "^/v1/(.+)$" {
		t.Errorf("got %+v", r)
	}
}

func TestBuildRouteFromFlagsErrors(t *testing.T) {
	cases := []struct {
		name string
		set  func()
	}{
		{"both-path-and-regex", func() {
			routeAddFlags.path = "/a"
			routeAddFlags.pathRegex = "x"
			routeAddFlags.port = 80
		}},
		{"neither-path-nor-regex", func() {
			routeAddFlags.port = 80
		}},
		{"rewrite-no-regex", func() {
			routeAddFlags.path = "/a"
			routeAddFlags.rewrite = "/b"
			routeAddFlags.port = 80
		}},
		{"bad-regex", func() {
			routeAddFlags.pathRegex = "(?P<"
			routeAddFlags.port = 80
		}},
		{"no-upstream", func() {
			routeAddFlags.path = "/a"
		}},
		{"multiple-upstreams", func() {
			routeAddFlags.path = "/a"
			routeAddFlags.port = 80
			routeAddFlags.url = "https://x"
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resetRouteFlags()
			c.set()
			if _, err := buildRouteFromFlags(); err == nil {
				t.Errorf("%s: expected err", c.name)
			}
		})
	}
}

func TestRunRouteListMissingSite(t *testing.T) {
	setupSrvRoot(t)
	if err := runRouteList(nil, []string{"ghost"}); err == nil {
		t.Error("expected err")
	}
}

func TestRunRouteListEmpty(t *testing.T) {
	setupSrvRoot(t)
	writeTestSite(t, "blog", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"blog.local"},
		ProjectPath: "/tmp",
		Port:        80,
		NetworkName: "n",
	})
	if err := runRouteList(nil, []string{"blog"}); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunRouteListWithRoutes(t *testing.T) {
	setupSrvRoot(t)
	preserve := true
	writeTestSite(t, "api", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"api.local"},
		ProjectPath: "/tmp",
		Port:        80,
		NetworkName: "n",
		Routes: []site.Route{
			{ID: "v1", Path: "/v1", Upstream: site.Upstream{Kind: "localhost", Port: 8080}, PreserveHost: &preserve},
			{ID: "v2", PathRegex: "^/v2/(.+)$", Rewrite: "/$1", Upstream: site.Upstream{Kind: "container", Container: "api-v2", Port: 80}, PreserveHost: &preserve},
		},
	})
	if err := runRouteList(nil, []string{"api"}); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunRouteRemoveMissingSite(t *testing.T) {
	setupSrvRoot(t)
	if err := runRouteRemove(nil, []string{"ghost", "x"}); err == nil {
		t.Error("expected err")
	}
}

func TestRunRouteRemoveMissingRoute(t *testing.T) {
	setupSrvRoot(t)
	writeTestSite(t, "api", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"api.local"},
		ProjectPath: "/tmp",
		Port:        80,
		NetworkName: "n",
	})
	if err := runRouteRemove(nil, []string{"api", "missing"}); err == nil {
		t.Error("expected err")
	}
}

func TestRunRouteAddMissingSite(t *testing.T) {
	setupSrvRoot(t)
	resetRouteFlags()
	if err := runRouteAdd(nil, []string{"ghost"}); err == nil {
		t.Error("expected err")
	}
}

func TestRunRouteAddHappy(t *testing.T) {
	root := setupSrvRoot(t)
	projectDir := root + "/p"
	mkAllDir(projectDir)
	writeTestSite(t, "api", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"api.local"},
		ProjectPath: projectDir,
		Port:        80,
		NetworkName: "n",
	})
	resetRouteFlags()
	routeAddFlags.path = "/v1"
	routeAddFlags.port = 8080
	defer resetRouteFlags()
	if err := runRouteAdd(nil, []string{"api"}); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunRouteAddDuplicate(t *testing.T) {
	setupSrvRoot(t)
	writeTestSite(t, "api", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"api.local"},
		ProjectPath: "/tmp",
		Port:        80,
		NetworkName: "n",
		Routes: []site.Route{
			{ID: "v1", Path: "/v1", Upstream: site.Upstream{Kind: "localhost", Port: 80}},
		},
	})
	resetRouteFlags()
	routeAddFlags.id = "v1"
	routeAddFlags.path = "/v2"
	routeAddFlags.port = 8080
	defer resetRouteFlags()
	if err := runRouteAdd(nil, []string{"api"}); err == nil {
		t.Error("expected duplicate-id err")
	}
}

func TestRunRouteRemoveHappy(t *testing.T) {
	root := setupSrvRoot(t)
	projectDir := root + "/p"
	mkAllDir(projectDir)
	writeTestSite(t, "api", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"api.local"},
		ProjectPath: projectDir,
		Port:        80,
		NetworkName: "n",
		Routes: []site.Route{
			{ID: "v1", Path: "/v1", Upstream: site.Upstream{Kind: "localhost", Port: 80}},
		},
	})
	if err := runRouteRemove(nil, []string{"api", "v1"}); err != nil {
		t.Errorf("err: %v", err)
	}
}

func resetRouteFlags() {
	routeAddFlags.id = ""
	routeAddFlags.path = ""
	routeAddFlags.pathRegex = ""
	routeAddFlags.rewrite = ""
	routeAddFlags.port = 0
	routeAddFlags.container = ""
	routeAddFlags.url = ""
	routeAddFlags.preserveHost = true
	routeAddFlags.rangeHeaders = false
	routeAddFlags.priority = 0
}
