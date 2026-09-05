package cmd

import (
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/srv/internal/site"
)

// ─── formatting helpers ──────────────────────────────────────────────────

func TestPlural(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{{0, "sites"}, {1, "site"}, {2, "sites"}, {-1, "sites"}}
	for _, c := range cases {
		if got := plural(c.n, "site", "sites"); got != c.want {
			t.Errorf("plural(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

func TestRoSuffix(t *testing.T) {
	if got := roSuffix(true); got != " (ro)" {
		t.Errorf("roSuffix(true) = %q", got)
	}
	if got := roSuffix(false); got != "" {
		t.Errorf("roSuffix(false) = %q, want empty", got)
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("", "", "third"); got != "third" {
		t.Errorf("firstNonEmpty() = %q, want third", got)
	}
	if got := firstNonEmpty("first", "second"); got != "first" {
		t.Errorf("firstNonEmpty() = %q, want first", got)
	}
	if got := firstNonEmpty(); got != "" {
		t.Errorf("firstNonEmpty() = %q, want empty", got)
	}
	if got := firstNonEmpty("", ""); got != "" {
		t.Errorf("firstNonEmpty() = %q, want empty", got)
	}
}

// ─── json-mode label helpers ─────────────────────────────────────────────
//
// These feed `--format json`, so they must emit raw machine values with no
// styling — and a broken site must produce an empty string rather than a
// guess, so a consumer can tell "unknown" from "compose".

func TestPlainSiteTypeLabel(t *testing.T) {
	cases := []struct {
		name string
		in   site.Site
		want string
	}{
		{"static", site.Site{Type: site.SiteTypeStatic}, "static"},
		{"dockerfile", site.Site{Type: site.SiteTypeDockerfile}, "dockerfile"},
		{"compose", site.Site{Type: site.SiteTypeCompose}, "compose"},
		{"unknown falls back to compose", site.Site{Type: "weird"}, "compose"},
		{"broken is empty", site.Site{Type: site.SiteTypeStatic, IsBroken: true}, ""},
	}
	for _, c := range cases {
		if got := plainSiteTypeLabel(c.in); got != c.want {
			t.Errorf("%s: plainSiteTypeLabel() = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestPlainSSLStatusForBrokenAndRemoteSites(t *testing.T) {
	if got := plainSSLStatus(site.Site{IsBroken: true}); got != "" {
		t.Errorf("plainSSLStatus(broken) = %q, want empty", got)
	}
	// A non-local site's cert is Let's Encrypt's problem, so there is nothing
	// on disk to inspect — this must not touch the certs directory.
	if got := plainSSLStatus(site.Site{IsLocal: false}); got != "auto" {
		t.Errorf("plainSSLStatus(remote) = %q, want auto", got)
	}
}

// The json label must never carry ANSI styling, or `--format json` output
// becomes unparseable when stdout is a TTY.
func TestPlainLabelsCarryNoANSI(t *testing.T) {
	vals := []string{
		plainSiteTypeLabel(site.Site{Type: site.SiteTypeStatic}),
		plainSSLStatus(site.Site{IsLocal: false}),
	}
	for _, v := range vals {
		if strings.Contains(v, "\033") {
			t.Errorf("%q contains an ANSI escape", v)
		}
	}
}

// ─── validation re-exports ───────────────────────────────────────────────
//
// cmd re-exports internal/validate so the CLI and completion code have one
// import. Thin as they are, a mis-wired re-export would silently disable a
// check, so each one gets a positive and a negative.

func TestValidationReExports(t *testing.T) {
	cases := []struct {
		name  string
		ok    func() error
		notOK func() error
	}{
		{"domain", func() error { return ValidateDomain("app.test") }, func() error { return ValidateDomain("bad domain") }},
		{"port", func() error { return ValidatePort(8080) }, func() error { return ValidatePort(70000) }},
		{"port string", func() error { return ValidatePortString("8080") }, func() error { return ValidatePortString("nope") }},
		{"site name", func() error { return ValidateSiteName("blog") }, func() error { return ValidateSiteName("-blog") }},
		{"container name", func() error { return ValidateContainerName("web-1") }, func() error { return ValidateContainerName("web 1") }},
		{"proxy name", func() error { return ValidateProxyName("app.test") }, func() error { return ValidateProxyName("app/test") }},
	}
	for _, c := range cases {
		if err := c.ok(); err != nil {
			t.Errorf("%s: valid input rejected: %v", c.name, err)
		}
		if err := c.notOK(); err == nil {
			t.Errorf("%s: invalid input accepted", c.name)
		}
	}
}

// ─── completion helpers ──────────────────────────────────────────────────
//
// Completion runs on every <TAB>, including against a machine with no srv
// state at all, so these must return empty rather than error or panic.

func TestCompletionHelpersOnEmptyState(t *testing.T) {
	setupSrvRoot(t)
	if got := GetSiteVolumeTargets("ghost"); got != nil {
		t.Errorf("GetSiteVolumeTargets(unknown) = %v, want nil", got)
	}
	if got := GetSiteExtraNetworks("ghost"); got != nil {
		t.Errorf("GetSiteExtraNetworks(unknown) = %v, want nil", got)
	}
	if got := routeTargetNames(); len(got) != 0 {
		t.Errorf("routeTargetNames() = %v, want empty on a fresh root", got)
	}
}

func TestGetSiteVolumeTargets(t *testing.T) {
	setupSrvRoot(t)
	if err := site.WriteSiteMetadata("blog", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"blog.test"},
		ProjectPath: "/tmp",
		Port:        80,
		Volumes: []site.VolumeMount{
			{Source: "/tmp/a", Target: "/data"},
			{Source: "/tmp/b", Target: "/cache", ReadOnly: true},
		},
	}); err != nil {
		t.Fatal(err)
	}

	got := GetSiteVolumeTargets("blog")
	if !slices.Equal(got, []string{"/data", "/cache"}) {
		t.Errorf("GetSiteVolumeTargets() = %v, want [/data /cache]", got)
	}
}

func TestRouteTargetNamesIncludesSites(t *testing.T) {
	setupSrvRoot(t)
	if err := site.WriteSiteMetadata("blog", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"blog.test"},
		ProjectPath: "/tmp",
		Port:        80,
	}); err != nil {
		t.Fatal(err)
	}
	if got := routeTargetNames(); !slices.Contains(got, "blog") {
		t.Errorf("routeTargetNames() = %v, want it to include blog", got)
	}
}
