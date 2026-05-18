package cmd

import (
	"errors"
	"testing"

	"github.com/stubbedev/srv/internal/docker"
	"github.com/stubbedev/srv/internal/site"
)

func dockerSwapNewClientErrShell() func() {
	return docker.SwapNewClientErr(errors.New("offline"))
}

func dockerSwapNewClientOKShell() func() {
	return docker.SwapNewClientOK()
}

func TestSiteShellContainer(t *testing.T) {
	cases := []struct {
		name string
		s    site.Site
		want string // "" means use phpFPM helper, else exact match
	}{
		{"node", site.Site{Name: "app", Type: site.SiteTypeNode}, "srv-app-node"},
		{"ruby", site.Site{Name: "api", Type: site.SiteTypeRuby}, "srv-api-app"},
		{"python", site.Site{Name: "api", Type: site.SiteTypePython}, "srv-api-app"},
		{"dockerfile", site.Site{Name: "x", Type: site.SiteTypeDockerfile}, "srv-x-app"},
		{"compose", site.Site{Name: "y", Type: site.SiteTypeCompose, ServiceName: "web"}, "web"},
		{"unknown-type-falls-through", site.Site{Name: "z", Type: "", ServiceName: "svc"}, "svc"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := siteShellContainer(c.s); got != c.want {
				t.Errorf("siteShellContainer(%+v) = %q, want %q", c.s, got, c.want)
			}
		})
	}
}

func TestSiteShellContainerPHPFallback(t *testing.T) {
	// PHP path requires reading site metadata. With SRV_ROOT pointed at an
	// empty tmp dir, ReadSiteMetadata returns (nil, nil) and the helper
	// falls back to "srv-<name>-php".
	root := t.TempDir()
	t.Setenv("SRV_ROOT", root)
	got := siteShellContainer(site.Site{Name: "blog", Type: site.SiteTypePHP})
	if got != "srv-blog-php" {
		t.Errorf("PHP fallback = %q, want srv-blog-php", got)
	}
}

func TestPhpFPMContainerForSiteMissing(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SRV_ROOT", root)
	if got := phpFPMContainerForSite("ghost"); got != "srv-ghost-php" {
		t.Errorf("got %q", got)
	}
}

func TestRunShellDockerDown(t *testing.T) {
	setupSrvRoot(t)
	t.Cleanup(dockerSwapNewClientErrShell())
	if err := runShell(nil, []string{"ghost"}); err == nil {
		t.Error("expected err")
	}
}

func TestRunShellMissingSite(t *testing.T) {
	setupSrvRoot(t)
	t.Cleanup(dockerSwapNewClientOKShell())
	if err := runShell(nil, []string{"ghost"}); err == nil {
		t.Error("expected err: site missing")
	}
}

func TestRunOpenMissingSite(t *testing.T) {
	setupSrvRoot(t)
	if err := runOpen(nil, []string{"ghost"}); err == nil {
		t.Error("expected err")
	}
}
