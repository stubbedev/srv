package site

import (
	"errors"
	"strings"
	"testing"

	"github.com/stubbedev/srv/internal/docker"
)

// stubEngine keeps the lifecycle tests off a real container engine. The
// defaults in internal/docker now refuse to run from a test binary, so these
// swaps are what make the calls succeed rather than merely what makes them
// safe.
func stubEngine(t *testing.T) {
	t.Helper()
	t.Cleanup(docker.SwapNewClientWithNetwork("test_traefik"))
	t.Cleanup(docker.SwapComposeExec(func(string, bool, ...string) error { return nil }))
	t.Cleanup(docker.SwapComposePrefixedExec(func(string, string, ...string) error { return nil }))
	t.Cleanup(docker.SwapDockerExec(func(bool, ...string) error { return nil }))
	t.Cleanup(docker.SwapComposePSOutput(func(string) ([]byte, error) { return []byte("running\n"), nil }))
}

// ─── DropRoute ───────────────────────────────────────────────────────────
//
// DropRoute filters in place (out := routes[:0]), which is the kind of reuse
// that quietly corrupts the caller's slice if it ever stops being the last
// read of the input.

func TestDropRoute(t *testing.T) {
	routes := []Route{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	got, removed := DropRoute(routes, "b")
	if !removed {
		t.Fatal("removed = false, want true")
	}
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "c" {
		t.Errorf("DropRoute() = %+v, want [a c]", got)
	}
}

func TestDropRouteMissingID(t *testing.T) {
	routes := []Route{{ID: "a"}}
	got, removed := DropRoute(routes, "nope")
	if removed {
		t.Error("removed = true for an id that is not present")
	}
	if len(got) != 1 || got[0].ID != "a" {
		t.Errorf("DropRoute() = %+v, want the input unchanged", got)
	}
}

func TestDropRouteEmptyAndAll(t *testing.T) {
	if got, removed := DropRoute(nil, "a"); removed || len(got) != 0 {
		t.Errorf("DropRoute(nil) = %+v, %v", got, removed)
	}
	got, removed := DropRoute([]Route{{ID: "only"}}, "only")
	if !removed || len(got) != 0 {
		t.Errorf("DropRoute(single match) = %+v, %v, want empty and true", got, removed)
	}
}

// Duplicate ids should all go: leaving one behind would make a second remove
// necessary for what looks like one route.
func TestDropRouteRemovesEveryMatch(t *testing.T) {
	got, removed := DropRoute([]Route{{ID: "x"}, {ID: "y"}, {ID: "x"}}, "x")
	if !removed {
		t.Fatal("removed = false")
	}
	if len(got) != 1 || got[0].ID != "y" {
		t.Errorf("DropRoute() = %+v, want just [y]", got)
	}
}

// ─── SplitContainerPort ──────────────────────────────────────────────────

// route_test.go covers the common shapes; these are the edges it leaves out.
func TestSplitContainerPortEdgeCases(t *testing.T) {
	for _, bad := range []string{"", "api:", ":3000", "api:70000", "api:-1"} {
		if _, _, err := SplitContainerPort(bad); err == nil {
			t.Errorf("SplitContainerPort(%q) = nil error, want a rejection", bad)
		}
	}
}

// ─── AddRoute / RemoveRoute ──────────────────────────────────────────────

func routeFor(id string, port int) Route {
	return Route{ID: id, Path: "/" + id, Upstream: Upstream{Kind: "localhost", Port: port}}
}

func TestAddRoutePersistsToMetadata(t *testing.T) {
	withSRVRoot(t)
	stubEngine(t)
	seedSite(t, "blog", []string{"blog.test"})

	if err := AddRoute("blog", routeFor("api", 3000)); err != nil {
		t.Fatalf("AddRoute() = %v", err)
	}
	meta, err := requireMeta("blog")
	if err != nil {
		t.Fatal(err)
	}
	if len(meta.Routes) != 1 || meta.Routes[0].ID != "api" {
		t.Errorf("routes = %+v, want one route with id api", meta.Routes)
	}
}

func TestAddRouteRejectsDuplicateID(t *testing.T) {
	withSRVRoot(t)
	stubEngine(t)
	seedSite(t, "blog", []string{"blog.test"})

	if err := AddRoute("blog", routeFor("api", 3000)); err != nil {
		t.Fatal(err)
	}
	err := AddRoute("blog", routeFor("api", 4000))
	if err == nil {
		t.Fatal("AddRoute() = nil for a duplicate id, want a refusal")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("err = %v, want an 'already exists' refusal", err)
	}

	// The rejected route must not have been written.
	meta, _ := requireMeta("blog")
	if len(meta.Routes) != 1 {
		t.Errorf("routes = %+v, want the duplicate not to have been added", meta.Routes)
	}
}

// A route that would make the metadata invalid must be rejected *before* it is
// written, or the site is left unloadable.
func TestAddRouteRejectsInvalidRouteWithoutWriting(t *testing.T) {
	withSRVRoot(t)
	stubEngine(t)
	seedSite(t, "blog", []string{"blog.test"})

	err := AddRoute("blog", Route{ID: "broken", Upstream: Upstream{Kind: "container"}})
	if err == nil {
		t.Fatal("AddRoute() = nil for an invalid route, want a rejection")
	}
	meta, _ := requireMeta("blog")
	if len(meta.Routes) != 0 {
		t.Errorf("an invalid route was persisted: %+v", meta.Routes)
	}
}

func TestAddRouteUnknownSite(t *testing.T) {
	withSRVRoot(t)
	stubEngine(t)
	if err := AddRoute("ghost", routeFor("api", 3000)); err == nil {
		t.Error("AddRoute(unknown site) = nil, want an error")
	}
}

func TestRemoveRoute(t *testing.T) {
	withSRVRoot(t)
	stubEngine(t)
	seedSite(t, "blog", []string{"blog.test"})

	if err := AddRoute("blog", routeFor("api", 3000)); err != nil {
		t.Fatal(err)
	}
	if err := AddRoute("blog", routeFor("admin", 4000)); err != nil {
		t.Fatal(err)
	}
	if err := RemoveRoute("blog", "api"); err != nil {
		t.Fatalf("RemoveRoute() = %v", err)
	}
	meta, _ := requireMeta("blog")
	if len(meta.Routes) != 1 || meta.Routes[0].ID != "admin" {
		t.Errorf("routes = %+v, want only admin left", meta.Routes)
	}
}

func TestRemoveRouteUnknownID(t *testing.T) {
	withSRVRoot(t)
	stubEngine(t)
	seedSite(t, "blog", []string{"blog.test"})

	err := RemoveRoute("blog", "ghost")
	if err == nil {
		t.Fatal("RemoveRoute(unknown id) = nil, want an error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("err = %v, want a not-found error", err)
	}
}

func TestAddRemoveRouteRoundTrip(t *testing.T) {
	withSRVRoot(t)
	stubEngine(t)
	seedSite(t, "blog", []string{"blog.test"})

	for i := range 2 {
		if err := AddRoute("blog", routeFor("api", 3000)); err != nil {
			t.Fatalf("AddRoute round %d = %v", i, err)
		}
		if err := RemoveRoute("blog", "api"); err != nil {
			t.Fatalf("RemoveRoute round %d = %v", i, err)
		}
	}
	meta, _ := requireMeta("blog")
	if len(meta.Routes) != 0 {
		t.Errorf("routes = %+v, want none left", meta.Routes)
	}
}

// ─── lifecycle ───────────────────────────────────────────────────────────

func TestStopSite(t *testing.T) {
	withSRVRoot(t)
	stubEngine(t)
	seedSite(t, "blog", []string{"blog.test"})

	if err := StopSite("blog"); err != nil {
		t.Errorf("StopSite() = %v", err)
	}
}

func TestLifecycleRejectsUnknownSite(t *testing.T) {
	withSRVRoot(t)
	stubEngine(t)
	for name, call := range map[string]func() error{
		"start":   func() error { return StartSite("ghost", false) },
		"stop":    func() error { return StopSite("ghost") },
		"restart": func() error { return RestartSite("ghost", false) },
	} {
		if err := call(); err == nil {
			t.Errorf("%s(unknown site) = nil, want an error", name)
		}
	}
}

// Every lifecycle entry point checks the engine first, so an unreachable
// engine must surface as an error rather than a partial start.
func TestLifecycleFailsWhenEngineIsUnreachable(t *testing.T) {
	withSRVRoot(t)
	stubEngine(t)
	seedSite(t, "blog", []string{"blog.test"})
	t.Cleanup(docker.SwapNewClientErr(errors.New("engine down")))

	for name, call := range map[string]func() error{
		"start":   func() error { return StartSite("blog", false) },
		"stop":    func() error { return StopSite("blog") },
		"restart": func() error { return RestartSite("blog", false) },
	} {
		if err := call(); err == nil {
			t.Errorf("%s() = nil with the engine down, want an error", name)
		}
	}
}

func TestRequireSiteUnknown(t *testing.T) {
	withSRVRoot(t)
	if _, err := requireSite("ghost"); err == nil {
		t.Error("requireSite(ghost) = nil, want an error")
	}
}
