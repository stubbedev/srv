package proxy

import (
	"path/filepath"
	"testing"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/site"
)

func setupSrvRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("SRV_ROOT", root)
	config.ResetCache()
	t.Cleanup(config.ResetCache)
	return root
}

func TestReadMissing(t *testing.T) {
	setupSrvRoot(t)
	m, err := Read("ghost")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if m != nil {
		t.Errorf("expected nil for missing, got %+v", m)
	}
}

func TestWriteAndRead(t *testing.T) {
	root := setupSrvRoot(t)
	in := Metadata{
		Name:     "api",
		Domains:  []string{"api.test"},
		Wildcard: false,
		IsLocal:  true,
		Routes: []site.Route{
			{
				ID:       "ws",
				Path:     "/app",
				Upstream: site.Upstream{Kind: "localhost", Port: 6001},
			},
		},
	}
	if err := Write(in); err != nil {
		t.Fatalf("write: %v", err)
	}

	out, err := Read("api")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if out == nil {
		t.Fatal("read returned nil")
	}
	if out.Name != "api" || out.Domains[0] != "api.test" || len(out.Routes) != 1 {
		t.Errorf("round-trip mismatch: %+v", out)
	}
	if out.Routes[0].ID != "ws" || out.Routes[0].Upstream.Port != 6001 {
		t.Errorf("route round-trip mismatch: %+v", out.Routes[0])
	}

	// Stored path follows the documented layout.
	wanted := filepath.Join(root, "proxies", "api", "metadata.yml")
	if !Exists("api") {
		t.Errorf("Exists() = false; expected metadata at %s", wanted)
	}
}

func TestRemove(t *testing.T) {
	setupSrvRoot(t)
	if err := Write(Metadata{Name: "x", Domains: []string{"x.test"}}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !Exists("x") {
		t.Fatal("expected exists")
	}
	if err := Remove("x"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if Exists("x") {
		t.Error("expected gone")
	}
}

func TestListNames(t *testing.T) {
	setupSrvRoot(t)
	if names := ListNames(); len(names) != 0 {
		t.Errorf("expected empty, got %v", names)
	}
	for _, n := range []string{"a", "b"} {
		if err := Write(Metadata{Name: n, Domains: []string{n + ".test"}}); err != nil {
			t.Fatal(err)
		}
	}
	names := ListNames()
	if len(names) != 2 {
		t.Errorf("expected 2 names, got %v", names)
	}
}
