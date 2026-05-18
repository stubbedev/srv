package site

import (
	"strings"
	"testing"

	"github.com/stubbedev/srv/internal/constants"
)

func TestParseGemfile(t *testing.T) {
	cases := []struct {
		name    string
		content string
		rails   bool
		sinatra bool
		rack    bool
	}{
		{"rails", `gem "rails", "~> 7.0"`, true, false, false},
		{"sinatra single", `gem 'sinatra'`, false, true, false},
		{"rack", `gem "rack"`, false, false, true},
		{"all", "gem \"rails\"\ngem \"sinatra\"\ngem \"rack\"\n", true, true, true},
		{"comment-skipped", "# gem \"rails\"\n", false, false, false},
		{"none", "gem \"puma\"\n", false, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseGemfile(c.content)
			if got.hasRails != c.rails || got.hasSinatra != c.sinatra || got.hasRack != c.rack {
				t.Errorf("got %+v, want rails=%v sinatra=%v rack=%v", got, c.rails, c.sinatra, c.rack)
			}
		})
	}
}

func TestDetectRubyFramework(t *testing.T) {
	cases := []struct {
		info gemfileInfo
		want string
	}{
		{gemfileInfo{hasRails: true}, constants.RubyFrameworkRails},
		{gemfileInfo{hasSinatra: true}, constants.RubyFrameworkSinatra},
		{gemfileInfo{hasRack: true}, constants.RubyFrameworkRack},
		{gemfileInfo{}, constants.RubyFrameworkGeneric},
		{gemfileInfo{hasRails: true, hasRack: true}, constants.RubyFrameworkRails},
	}
	for _, c := range cases {
		if got := detectRubyFramework(c.info); got != c.want {
			t.Errorf("%+v -> %q, want %q", c.info, got, c.want)
		}
	}
}

func TestDetectRubyVersion(t *testing.T) {
	cases := []struct {
		content string
		want    string
	}{
		{"3.3.0\n", "3.3"},
		{"ruby-3.2.1\n", "3.2"},
		{"3", "3"},
		{"", constants.RubyVersionLatest},
	}
	for _, c := range cases {
		dir := t.TempDir()
		writeFiles(t, dir, map[string]string{".ruby-version": c.content})
		if got := detectRubyVersion(dir); got != c.want {
			t.Errorf("%q -> %q, want %q", c.content, got, c.want)
		}
	}
}

func TestDetectRubyVersionMissing(t *testing.T) {
	dir := t.TempDir()
	if got := detectRubyVersion(dir); got != constants.RubyVersionLatest {
		t.Errorf("missing -> %q", got)
	}
}

func TestBuildRubyStartCmd(t *testing.T) {
	cases := []struct {
		fw   string
		want string
	}{
		{constants.RubyFrameworkRails, "bundle exec rails server"},
		{constants.RubyFrameworkSinatra, "ruby app.rb"},
		{constants.RubyFrameworkRack, "bundle exec rackup"},
		{constants.RubyFrameworkGeneric, "ruby app.rb"},
	}
	for _, c := range cases {
		got := buildRubyStartCmd(c.fw, 3000)
		if !strings.Contains(got, c.want) {
			t.Errorf("%s -> %q, missing %q", c.fw, got, c.want)
		}
	}
}

func TestRubyImageTag(t *testing.T) {
	if got := RubyImageTag(""); got != constants.RubyImageAlpine {
		t.Errorf("empty -> %q", got)
	}
	if got := RubyImageTag(constants.RubyVersionLatest); got != constants.RubyImageAlpine {
		t.Errorf("latest -> %q", got)
	}
	if got := RubyImageTag("3.3"); got != "ruby:3.3-alpine" {
		t.Errorf("3.3 -> %q", got)
	}
}

func TestDetectRubySiteMissing(t *testing.T) {
	dir := t.TempDir()
	info, err := DetectRubySite(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info != nil {
		t.Error("expected nil for empty dir")
	}
}

func TestDetectRubySiteRails(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"Gemfile":       `gem "rails", "~> 7.0"`,
		".ruby-version": "3.3.0\n",
	})
	info, err := DetectRubySite(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info == nil {
		t.Fatal("expected detected")
	}
	if info.Framework != constants.RubyFrameworkRails {
		t.Errorf("Framework = %q", info.Framework)
	}
	if info.RubyVersion != "3.3" {
		t.Errorf("Version = %q", info.RubyVersion)
	}
}

func TestBuildAppTraefikLabels(t *testing.T) {
	labels := buildAppTraefikLabels("api", []string{"api.local"}, true, false, 4000)
	if labels["traefik.enable"] != "true" {
		t.Error("traefik.enable missing")
	}
	if labels["traefik.http.services.api.loadbalancer.server.port"] != "4000" {
		t.Errorf("port wrong: %q", labels["traefik.http.services.api.loadbalancer.server.port"])
	}
	if _, ok := labels["traefik.http.routers.api.tls.certresolver"]; ok {
		t.Error("local site should not have certresolver")
	}

	labels = buildAppTraefikLabels("api", []string{"api.com"}, false, false, 4000)
	if labels["traefik.http.routers.api.tls.certresolver"] != "letsencrypt" {
		t.Error("non-local should have letsencrypt resolver")
	}
}
