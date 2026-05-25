package site

import (
	"strings"
	"testing"

	"github.com/stubbedev/srv/internal/constants"
)

func TestGenerateStaticNginxConfSPA(t *testing.T) {
	out := generateStaticNginxConf(StaticSiteOptions{SPA: true})
	if !strings.Contains(out, "try_files $uri $uri/ /index.html") {
		t.Error("SPA mode try_files missing")
	}
}

func TestGenerateStaticNginxConfNoSPA(t *testing.T) {
	out := generateStaticNginxConf(StaticSiteOptions{SPA: false})
	if !strings.Contains(out, "try_files $uri $uri/ =404") {
		t.Error("non-SPA try_files missing")
	}
}

func TestGenerateStaticNginxConfCORS(t *testing.T) {
	out := generateStaticNginxConf(StaticSiteOptions{CORS: true})
	if !strings.Contains(out, "Access-Control-Allow-Origin") {
		t.Error("CORS headers missing")
	}
}

func TestGenerateStaticNginxConfNoCORS(t *testing.T) {
	out := generateStaticNginxConf(StaticSiteOptions{CORS: false})
	if strings.Contains(out, "Access-Control-Allow-Origin") {
		t.Error("CORS headers should be absent")
	}
}

func TestGenerateStaticNginxConfCacheOn(t *testing.T) {
	out := generateStaticNginxConf(StaticSiteOptions{Cache: true})
	if !strings.Contains(out, `Cache-Control "public, immutable"`) {
		t.Error("cache headers missing")
	}
}

func TestGenerateStaticNginxConfCacheOff(t *testing.T) {
	out := generateStaticNginxConf(StaticSiteOptions{Cache: false})
	if !strings.Contains(out, "no-cache, no-store") {
		t.Error("no-cache headers missing")
	}
}

func TestMakeComposeHealthCheck(t *testing.T) {
	hc := makeComposeHealthCheck(8080)
	if hc == nil {
		t.Fatal("nil")
	}
	joined := strings.Join(hc.Test, " ")
	if !strings.Contains(joined, "8080") {
		t.Errorf("port missing: %v", hc.Test)
	}
}

func TestVolumeConsistencyForHost(t *testing.T) {
	v := volumeConsistencyForHost()
	// We can't change runtime.GOOS in a test; just verify it returns either
	// "cached" (macOS) or "" (everything else).
	if v != "cached" && v != "" {
		t.Errorf("got %q, want 'cached' or empty", v)
	}
}

func TestStampSrvLabels(t *testing.T) {
	labels := map[string]string{}
	StampSrvLabels(labels, "blog", "static")
	if labels[constants.LabelSrvSite] != "blog" {
		t.Error("site label missing")
	}
	if labels[constants.LabelSrvType] != "static" {
		t.Error("type label missing")
	}
}

func TestBuildTraefikLabels(t *testing.T) {
	labels := buildTraefikLabels("blog", []string{"blog.local"}, true, false, 80)
	if labels["traefik.enable"] != "true" {
		t.Error("traefik.enable missing")
	}
	if labels["traefik.http.services.blog.loadbalancer.server.port"] != "80" {
		t.Error("port should be 80")
	}
	if _, ok := labels["traefik.http.routers.blog.tls.certresolver"]; ok {
		t.Error("local site should not have certresolver")
	}

	labels = buildTraefikLabels("blog", []string{"blog.com"}, false, false, 80)
	if labels["traefik.http.routers.blog.tls.certresolver"] != "letsencrypt" {
		t.Error("non-local should have letsencrypt resolver")
	}

	labels = buildTraefikLabels("api", []string{"api.test"}, true, false, 8080)
	if labels["traefik.http.services.api.loadbalancer.server.port"] != "8080" {
		t.Error("custom port should win")
	}
}
