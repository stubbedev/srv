package cmd

import (
	"strings"
	"testing"
)

func TestRenderFallbackNginx(t *testing.T) {
	conf, err := renderFallbackNginx(fallbackSpec{
		Name:            "kontainer",
		PrimaryHost:     "host.docker.internal",
		PrimaryPort:     "3001",
		FallbackURL:     "https://kontainer.com",
		FallbackTimeout: "3s",
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	mustContain := []string{
		"proxy_pass http://host.docker.internal:3001;",
		"error_page 502 503 504 = @fallback;",
		"location @fallback {",
		"proxy_pass https://kontainer.com;",
		`set $fb_host "kontainer.com";`,
		"proxy_connect_timeout 3s;",
		"proxy_ssl_server_name on;",
	}
	for _, want := range mustContain {
		if !strings.Contains(conf, want) {
			t.Errorf("missing %q in:\n%s", want, conf)
		}
	}
}

func TestRenderFallbackNginx_BadURL(t *testing.T) {
	tests := []string{
		"ftp://x",
		"notaurl",
	}
	for _, u := range tests {
		_, err := renderFallbackNginx(fallbackSpec{
			Name:        "x",
			PrimaryHost: "host.docker.internal",
			PrimaryPort: "80",
			FallbackURL: u,
		})
		if err == nil {
			t.Errorf("expected error for %q", u)
		}
	}
}
