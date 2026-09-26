package site

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/platform"
)

// daemonStaticURL is the upstream URL the Traefik route must point at: the
// daemon's embedded static server, reached via localhost on Linux
// (host-networked Traefik) and host.docker.internal elsewhere.
func daemonStaticURL() string {
	if platform.IsLinux() {
		return "http://localhost:15380"
	}
	return "http://host.docker.internal:15380"
}

func newDaemonProject(t *testing.T) (root, projectDir string) {
	t.Helper()
	root = withSRVRoot(t)
	projectDir = filepath.Join(root, "page")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "index.html"), []byte("daemon body"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, projectDir
}

func siteDir(t *testing.T, root, name string) string {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	return SiteConfigDir(cfg, name)
}

func TestAddDaemonStaticServesFromDaemon(t *testing.T) {
	root, projectDir := newDaemonProject(t)

	res, err := Add(AddOptions{
		Path:   projectDir,
		Domain: "start.local",
		Name:   "start-local",
		Local:  true,
		Daemon: true,
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	meta, err := ReadSiteMetadata(res.Name)
	if err != nil || meta == nil {
		t.Fatalf("read metadata: %v", err)
	}
	if !meta.DaemonServed || meta.Type != SiteTypeStatic {
		t.Errorf("metadata DaemonServed=%v Type=%v", meta.DaemonServed, meta.Type)
	}

	siteDirectory := siteDir(t, root, res.Name)
	for _, artifact := range []string{"docker-compose.yml", "nginx.conf"} {
		if _, err := os.Stat(filepath.Join(siteDirectory, artifact)); !os.IsNotExist(err) {
			t.Errorf("%s should not exist for a daemon-served site", artifact)
		}
	}

	data, err := os.ReadFile(filepath.Join(root, "traefik", "conf", "site-"+res.Name+".yml"))
	if err != nil {
		t.Fatalf("route config missing: %v", err)
	}
	if !strings.Contains(string(data), daemonStaticURL()) {
		t.Errorf("route config does not point at the daemon:\n%s", data)
	}
	if !strings.Contains(string(data), "Host(`start.local`)") {
		t.Errorf("route config missing the Host rule:\n%s", data)
	}
}

func TestAddDaemonRejectsNonStatic(t *testing.T) {
	root, _ := newDaemonProject(t)
	projectDir := filepath.Join(root, "composeproj")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "docker-compose.yml"), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Add(AddOptions{Path: projectDir, Domain: "c.local", Daemon: true}); err == nil {
		t.Error("expected error adding a compose project with daemon=true")
	}
}

func TestAddDaemonRejectsVolumesAndPort(t *testing.T) {
	_, projectDir := newDaemonProject(t)

	if _, err := Add(AddOptions{Path: projectDir, Domain: "v.local", Daemon: true, Volumes: []VolumeMount{{Source: "/tmp", Target: "/data"}}}); err == nil {
		t.Error("expected error with volumes on a daemon-served site")
	}
	if _, err := Add(AddOptions{Path: projectDir, Domain: "p.local", Daemon: true, Port: 8080}); err == nil {
		t.Error("expected error with an explicit port on a daemon-served site")
	}
	// The CLI always passes the default container port when --port is
	// omitted; that must not read as an explicit port request.
	if _, err := Add(AddOptions{Path: projectDir, Domain: "p.local", Daemon: true, Port: 80}); err != nil {
		t.Errorf("default container port should be accepted with daemon=true: %v", err)
	}
}

func TestReloadDaemonServedWritesRouteNotCompose(t *testing.T) {
	root, projectDir := newDaemonProject(t)
	if err := os.MkdirAll(filepath.Join(root, "traefik", "conf"), 0o755); err != nil {
		t.Fatal(err)
	}
	meta := SiteMetadata{
		Type:         SiteTypeStatic,
		Domains:      []string{"d.local"},
		ProjectPath:  projectDir,
		NetworkName:  "n",
		DaemonServed: true,
	}
	if err := WriteSiteMetadata("d", meta); err != nil {
		t.Fatal(err)
	}

	res, err := Reload("d")
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if res.NeedsRestart {
		t.Error("daemon-served sites never need a container restart")
	}
	data, err := os.ReadFile(filepath.Join(root, "traefik", "conf", "site-d.yml"))
	if err != nil {
		t.Fatalf("route config missing: %v", err)
	}
	if !strings.Contains(string(data), daemonStaticURL()) {
		t.Errorf("route config does not point at the daemon:\n%s", data)
	}
	for _, artifact := range []string{"docker-compose.yml", "nginx.conf"} {
		if _, err := os.Stat(filepath.Join(siteDir(t, root, "d"), artifact)); !os.IsNotExist(err) {
			t.Errorf("%s should not exist for a daemon-served site", artifact)
		}
	}

	// Unchanged metadata short-circuits, like every other site type.
	if res2, err := Reload("d"); err != nil || !res2.Skipped {
		t.Errorf("second Reload: skipped=%v err=%v", res2 != nil && res2.Skipped, err)
	}
}

func TestValidateMetadataDaemonConstraints(t *testing.T) {
	static := &SiteMetadata{Type: SiteTypeStatic, Domains: []string{"d.local"}, DaemonServed: true}
	if err := ValidateMetadata(static); err != nil {
		t.Errorf("valid daemon-served static rejected: %v", err)
	}
	compose := &SiteMetadata{Type: SiteTypeCompose, Domains: []string{"d.local"}, DaemonServed: true}
	if err := ValidateMetadata(compose); err == nil {
		t.Error("daemon-served compose should be rejected")
	}
	volumes := &SiteMetadata{
		Type:         SiteTypeStatic,
		Domains:      []string{"d.local"},
		DaemonServed: true,
		Volumes:      []VolumeMount{{Source: "/tmp", Target: "/data"}},
	}
	if err := ValidateMetadata(volumes); err == nil {
		t.Error("daemon-served with volumes should be rejected")
	}
}

// TestDaemonStartRestoresRouteAfterStop is the regression test for start/stop
// semantics: Stop removes the route file, metadata stays unchanged, and the
// next Start must still restore it — the metadata-hash short-circuit in
// Reload would skip the write, so Start goes through ForceReload.
func TestDaemonStartRestoresRouteAfterStop(t *testing.T) {
	root, projectDir := newDaemonProject(t)
	if err := os.MkdirAll(filepath.Join(root, "traefik", "conf"), 0o755); err != nil {
		t.Fatal(err)
	}
	meta := SiteMetadata{
		Type:         SiteTypeStatic,
		Domains:      []string{"d.local"},
		ProjectPath:  projectDir,
		NetworkName:  "n",
		DaemonServed: true,
	}
	if err := WriteSiteMetadata("d", meta); err != nil {
		t.Fatal(err)
	}

	routePath := filepath.Join(root, "traefik", "conf", "site-d.yml")
	if err := StartSite("d", false); err != nil {
		t.Fatalf("StartSite: %v", err)
	}
	if _, err := os.Stat(routePath); err != nil {
		t.Fatalf("start did not write the route config: %v", err)
	}

	if err := StopSite("d"); err != nil {
		t.Fatalf("StopSite: %v", err)
	}
	if _, err := os.Stat(routePath); !os.IsNotExist(err) {
		t.Fatal("stop did not remove the route config")
	}

	// The metadata never changed, so a plain Reload would short-circuit; the
	// start must restore the route anyway.
	if err := StartSite("d", false); err != nil {
		t.Fatalf("StartSite after stop: %v", err)
	}
	if _, err := os.Stat(routePath); err != nil {
		t.Fatalf("start after stop did not restore the route config: %v", err)
	}
}
