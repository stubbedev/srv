package httpd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

// newTestServer builds a Server over a temp project directory and returns it
// plus the directory. SetTargets is driven directly so the tests exercise the
// serving layer without site metadata.
func newTestServer(t *testing.T, targets map[string]Target, wildcards map[string]Target) (*Server, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	s := New("127.0.0.1:0")
	s.SetTargets(targets, wildcards)
	return s, root
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func get(t *testing.T, s *Server, host, path string) (int, string, http.Header) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://"+host+path, nil)
	req.Host = host
	s.ServeHTTP(rec, req)
	body, _ := io.ReadAll(rec.Result().Body)
	return rec.Code, string(body), rec.Header()
}

func TestHostDispatch(t *testing.T) {
	s, _ := newTestServer(t, map[string]Target{
		"a.test": {Root: "/tmp/does-not-exist-a"},
	}, nil)
	// Unknown host never reaches a site directory.
	code, body, _ := get(t, s, "other.test", "/")
	if code != http.StatusNotFound {
		t.Errorf("unknown host: code = %d, want 404 (%s)", code, body)
	}
}

func TestServesProjectFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "index.html", "hello start page")
	writeFile(t, root, "app.js", "console.log(1)")
	s := New("127.0.0.1:0")
	s.SetTargets(map[string]Target{"start.local": {Root: root}}, nil)

	code, body, _ := get(t, s, "start.local", "/")
	if code != http.StatusOK || body != "hello start page" {
		t.Errorf("index: code=%d body=%q", code, body)
	}
	code, body, _ = get(t, s, "start.local:443", "/app.js")
	if code != http.StatusOK || body != "console.log(1)" {
		t.Errorf("asset with port-suffixed Host: code=%d body=%q", code, body)
	}
}

func TestWildcardDispatch(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "index.html", "wildcard body")
	s := New("127.0.0.1:0")
	s.SetTargets(map[string]Target{"foo.test": {Root: root}}, map[string]Target{"foo.test": {Root: root}})

	for _, host := range []string{"foo.test", "app.foo.test"} {
		code, body, _ := get(t, s, host, "/")
		if code != http.StatusOK || body != "wildcard body" {
			t.Errorf("%s: code=%d body=%q", host, code, body)
		}
	}
	// A different apex sharing the suffix must not match.
	if code, _, _ := get(t, s, "evilfoo.test", "/"); code != http.StatusNotFound {
		t.Errorf("evilfoo.test: code = %d, want 404", code)
	}
}

func TestDotfilesAndSensitivePathsDenied(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "index.html", "ok")
	writeFile(t, root, ".env", "SECRET=1")
	writeFile(t, root, ".git/config", "[core]")
	writeFile(t, root, "deploy.sh", "rm -rf")
	writeFile(t, root, "compose.yml", "services:")
	writeFile(t, root, "node_modules/leftpad/index.js", "x")
	writeFile(t, root, "assets/app.css", "body{}")
	s := New("127.0.0.1:0")
	s.SetTargets(map[string]Target{"s.test": {Root: root}}, nil)

	for _, path := range []string{"/.env", "/.git/config", "/deploy.sh", "/compose.yml", "/node_modules/leftpad/index.js"} {
		if code, _, _ := get(t, s, "s.test", path); code != http.StatusNotFound {
			t.Errorf("%s: code = %d, want 404", path, code)
		}
	}
	if code, _, _ := get(t, s, "s.test", "/assets/app.css"); code != http.StatusOK {
		t.Errorf("/assets/app.css: code = %d, want 200", code)
	}
}

func TestTraversalCannotEscapeRoot(t *testing.T) {
	base := t.TempDir()
	writeFile(t, base, "outside.html", "escaped")
	s := New("127.0.0.1:0")
	s.SetTargets(map[string]Target{"s.test": {Root: filepath.Join(base, "site")}}, nil)

	for _, path := range []string{"/../outside.html", "/..%2foutside.html", "/%2e%2e/outside.html"} {
		if code, _, _ := get(t, s, "s.test", path); code != http.StatusNotFound {
			t.Errorf("%s: code = %d, want 404", path, code)
		}
	}
}

func TestSPAFallback(t *testing.T) {
	plain, _ := newTestServer(t, map[string]Target{"p.test": {Root: t.TempDir()}}, nil)
	if code, _, _ := get(t, plain, "p.test", "/missing/route"); code != http.StatusNotFound {
		t.Errorf("non-SPA miss: code = %d, want 404", code)
	}

	spa := t.TempDir()
	writeFile(t, spa, "index.html", "spa shell")
	s := New("127.0.0.1:0")
	s.SetTargets(map[string]Target{"s.test": {Root: spa, SPA: true}}, nil)
	code, body, _ := get(t, s, "s.test", "/missing/route")
	if code != http.StatusOK || body != "spa shell" {
		t.Errorf("SPA fallback: code=%d body=%q", code, body)
	}
	// nginx parity: try_files $uri $uri/ /index.html serves the shell for any
	// miss, extension or not — even would-be assets.
	code, body, _ = get(t, s, "s.test", "/nope.js")
	if code != http.StatusOK || body != "spa shell" {
		t.Errorf("SPA fallback for extension miss: code=%d body=%q", code, body)
	}
}

func TestDirectoryWithoutIndexNotListed(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "sub/file.txt", "x")
	s, _ := newTestServer(t, map[string]Target{"s.test": {Root: root}}, nil)
	if code, _, _ := get(t, s, "s.test", "/sub/"); code != http.StatusNotFound {
		t.Errorf("dir without index: code = %d, want 404", code)
	}
}

func TestCustom404Page(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "404.html", "custom not found")
	s, _ := newTestServer(t, map[string]Target{"s.test": {Root: root}}, nil)
	code, body, _ := get(t, s, "s.test", "/nothing")
	if code != http.StatusNotFound || body != "custom not found" {
		t.Errorf("custom 404: code=%d body=%q", code, body)
	}
}

func TestCacheHeaders(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "app.css", "body{}")
	writeFile(t, root, "index.html", "hi")
	s := New("127.0.0.1:0")
	s.SetTargets(map[string]Target{"s.test": {Root: root, Cache: true}}, nil)

	_, _, h := get(t, s, "s.test", "/app.css")
	if cc := h.Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("asset Cache-Control = %q, want immutable", cc)
	}
	_, _, h = get(t, s, "s.test", "/")
	if cc := h.Get("Cache-Control"); cc != "" {
		t.Errorf("non-asset Cache-Control = %q, want unset", cc)
	}

	nocache := t.TempDir()
	writeFile(t, nocache, "app.css", "body{}")
	s2 := New("127.0.0.1:0")
	s2.SetTargets(map[string]Target{"s.test": {Root: nocache}}, nil)
	_, _, h = get(t, s2, "s.test", "/app.css")
	if cc := h.Get("Cache-Control"); !strings.Contains(cc, "no-cache") {
		t.Errorf("no-cache mode Cache-Control = %q", cc)
	}
}

func TestCORS(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "index.html", "hi")
	s := New("127.0.0.1:0")
	s.SetTargets(map[string]Target{"s.test": {Root: root, CORS: true}}, nil)

	_, _, h := get(t, s, "s.test", "/")
	if aco := h.Get("Access-Control-Allow-Origin"); aco != "*" {
		t.Errorf("Allow-Origin = %q, want *", aco)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "http://s.test/", nil)
	req.Host = "s.test"
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Errorf("OPTIONS preflight: code = %d, want 204", rec.Code)
	}
}

func TestSecurityHeaders(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "index.html", "hi")
	s, _ := newTestServer(t, map[string]Target{"s.test": {Root: root}}, nil)
	_, _, h := get(t, s, "s.test", "/")
	for _, name := range []string{"X-Frame-Options", "X-Content-Type-Options", "X-XSS-Protection"} {
		if h.Get(name) == "" {
			t.Errorf("missing security header %s", name)
		}
	}
}

func TestAccessLogging(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "index.html", "hi")
	var buf bytes.Buffer
	zl := zerolog.New(&buf)
	s := New("127.0.0.1:0")
	s.Logger = &zl
	s.SetTargets(map[string]Target{"s.test": {Name: "s.test", Root: root}}, nil)

	get(t, s, "s.test", "/")
	get(t, s, "s.test", "/missing")

	var events []map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(buf.String()), "\n") {
		var e map[string]any
		if err := json.Unmarshal([]byte(line), &e); err == nil {
			events = append(events, e)
		}
	}
	if len(events) != 2 {
		t.Fatalf("want 2 access events, got %d: %s", len(events), buf.String())
	}
	hit := events[0]
	if hit["site"] != "s.test" || hit["method"] != "GET" || hit["path"] != "/" || hit["status"] != float64(http.StatusOK) {
		t.Errorf("unexpected hit event: %v", hit)
	}
	if hit["level"] != "info" {
		t.Errorf("hit level = %v, want info", hit["level"])
	}
	miss := events[1]
	if miss["path"] != "/missing" || miss["status"] != float64(http.StatusNotFound) {
		t.Errorf("unexpected miss event: %v", miss)
	}
	if miss["level"] != "warn" {
		t.Errorf("miss level = %v, want warn", miss["level"])
	}
	if _, ok := hit["dur_ms"].(float64); !ok {
		t.Errorf("dur_ms missing or not a number: %v", hit["dur_ms"])
	}
}

func TestMethodNotAllowed(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "index.html", "hi")
	s := New("127.0.0.1:0")
	s.SetTargets(map[string]Target{"s.test": {Root: root}}, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://s.test/", nil)
	req.Host = "s.test"
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST: code = %d, want 405", rec.Code)
	}
}

func TestSetTargetsReplacesTable(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "index.html", "hi")
	s := New("127.0.0.1:0")
	s.SetTargets(map[string]Target{"old.test": {Root: root}}, nil)
	if code, _, _ := get(t, s, "old.test", "/"); code != http.StatusOK {
		t.Fatalf("old.test: code = %d, want 200", code)
	}
	s.SetTargets(map[string]Target{"new.test": {Root: root}}, nil)
	if code, _, _ := get(t, s, "old.test", "/"); code != http.StatusNotFound {
		t.Errorf("old.test after re-target: code = %d, want 404", code)
	}
	if code, _, _ := get(t, s, "new.test", "/"); code != http.StatusOK {
		t.Errorf("new.test after re-target: code = %d, want 200", code)
	}
}
