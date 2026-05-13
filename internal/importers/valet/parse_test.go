package valet

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseFile_PHP(t *testing.T) {
	dir := t.TempDir()
	nginxPath := filepath.Join(dir, "cms-kontainer.test")
	conf := `server {
    listen 80;
    server_name cms-kontainer.test www.cms-kontainer.test *.cms-kontainer.test;
    return 301 https://$host$request_uri;
}
server {
    listen 443 ssl http2;
    server_name cms-kontainer.test www.cms-kontainer.test *.cms-kontainer.test;
    client_max_body_size 2G;

    location / {
        rewrite ^ /valet/server.php last;
    }
    location ~ \.php$ {
        fastcgi_pass unix:/tmp/valet.sock;
        fastcgi_read_timeout 300s;
        fastcgi_send_timeout 250s;
        fastcgi_connect_timeout 10s;
    }
}
server {
    listen 88;
    server_name cms-kontainer.test;
    location ~ \.php$ {
        fastcgi_pass unix:/tmp/valet.sock;
    }
}
`
	if err := os.WriteFile(nginxPath, []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}

	// Fake sites dir with a "kontainer" symlink pointing to a real path.
	sitesDir := filepath.Join(dir, "Sites")
	if err := os.MkdirAll(sitesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	projectDir := filepath.Join(dir, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(projectDir, filepath.Join(sitesDir, "kontainer")); err != nil {
		t.Fatal(err)
	}

	site, err := ParseFile(nginxPath, sitesDir, nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if site.Domain != "cms-kontainer.test" {
		t.Errorf("Domain = %q, want cms-kontainer.test", site.Domain)
	}
	if !site.Wildcard {
		t.Error("expected wildcard=true")
	}
	if !site.IsPHP {
		t.Error("expected IsPHP=true")
	}
	if !site.Internal {
		t.Error("expected Internal=true (listen 88 block present)")
	}
	if site.MaxBody != "2G" {
		t.Errorf("MaxBody = %q, want 2G", site.MaxBody)
	}
	if site.ReadTimeout != "300s" {
		t.Errorf("ReadTimeout = %q, want 300s", site.ReadTimeout)
	}
	if site.SendTimeout != "250s" {
		t.Errorf("SendTimeout = %q, want 250s", site.SendTimeout)
	}
	if site.ConnTimeout != "10s" {
		t.Errorf("ConnTimeout = %q, want 10s", site.ConnTimeout)
	}
	if site.ProjectPath != projectDir {
		t.Errorf("ProjectPath = %q, want %q", site.ProjectPath, projectDir)
	}
}

func TestParseFile_ProxyWithFallback(t *testing.T) {
	dir := t.TempDir()
	nginxPath := filepath.Join(dir, "kontainer.com")
	conf := `server {
    listen 443 ssl http2;
    server_name kontainer.com www.kontainer.com *.kontainer.com;
    location / {
        proxy_pass http://localhost:3001;
        proxy_intercept_errors on;
        error_page 502 503 504 = @prod_fallback;
    }
    location @prod_fallback {
        set $prod_upstream "kontainer.com";
        proxy_pass https://$prod_upstream;
    }
}
`
	if err := os.WriteFile(nginxPath, []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	site, err := ParseFile(nginxPath, "", nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if site.IsPHP {
		t.Error("IsPHP should be false")
	}
	if site.ProxyTarget != "localhost:3001" {
		t.Errorf("ProxyTarget = %q, want localhost:3001", site.ProxyTarget)
	}
	if site.FallbackURL != "https://kontainer.com" {
		t.Errorf("FallbackURL = %q, want https://kontainer.com", site.FallbackURL)
	}
}

func TestParseFile_RouteSplits(t *testing.T) {
	dir := t.TempDir()
	nginxPath := filepath.Join(dir, "kontainer.test")
	conf := `server {
    listen 443 ssl;
    server_name kontainer.test;
    location /app {
        proxy_pass http://127.0.0.1:6001;
    }
    location ~ ^/videos/([^/]+)/(.+)$ {
        rewrite ^/videos/([^/]+)/(.+)$ /abs/videos/$1/$2 break;
        proxy_pass http://127.0.0.1:9080;
    }
    location / {
        rewrite ^ /valet/server.php last;
    }
    location ~ \.php$ {
        fastcgi_pass unix:/tmp/valet.sock;
    }
}
`
	if err := os.WriteFile(nginxPath, []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	site, err := ParseFile(nginxPath, "", nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(site.Routes) < 2 {
		t.Fatalf("expected >=2 routes, got %d", len(site.Routes))
	}

	// /app prefix
	var found bool
	for _, r := range site.Routes {
		if r.Path == "/app" && r.Port == 6001 {
			found = true
		}
	}
	if !found {
		t.Errorf("missing /app → 6001 route, got %+v", site.Routes)
	}
	// regex rewrite
	found = false
	for _, r := range site.Routes {
		if r.PathRegex != "" && r.Port == 9080 && r.Rewrite != "" {
			found = true
		}
	}
	if !found {
		t.Errorf("missing regex rewrite → 9080, got %+v", site.Routes)
	}
}

func TestResolveValetProjectPath(t *testing.T) {
	dir := t.TempDir()
	sitesDir := filepath.Join(dir, "Sites")
	if err := os.MkdirAll(sitesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(dir, "kontainer-project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(project, filepath.Join(sitesDir, "kontainer")); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		domain string
		want   string
	}{
		{"kontainer.test", project},               // exact label
		{"cms-kontainer.test", project},           // suffix peel
		{"kontainer-8080.test", project},          // prefix peel
		{"jira.konform.com", ""},                  // no symlink
		{"site-kontainer-extra.test", project},    // multi-segment, kontainer is in the middle
	}
	for _, tc := range tests {
		got := resolveValetProjectPath(tc.domain, sitesDir, nil)
		if got != tc.want {
			t.Errorf("resolveValetProjectPath(%q) = %q, want %q", tc.domain, got, tc.want)
		}
	}
}

func TestResolveValetProjectPath_ParkedPath(t *testing.T) {
	dir := t.TempDir()
	parked := filepath.Join(dir, "parked")
	myapp := filepath.Join(parked, "myapp")
	if err := os.MkdirAll(myapp, 0o755); err != nil {
		t.Fatal(err)
	}
	got := resolveValetProjectPath("myapp.test", "", []string{parked})
	if got != myapp {
		t.Errorf("got %q, want %q", got, myapp)
	}
	// Subdomain stripping for parked mode too.
	got = resolveValetProjectPath("api-myapp.test", "", []string{parked})
	if got != myapp {
		t.Errorf("strip: got %q, want %q", got, myapp)
	}
	// Unknown host returns "".
	got = resolveValetProjectPath("nothing.test", "", []string{parked})
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}
