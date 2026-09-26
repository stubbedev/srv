// Package httpd serves daemon-served static sites ("srv add --daemon") from
// the srv daemon process — the sibling of the embedded DNS server. One
// listener multiplexes every daemon-served site by Host header, so a static
// site needs no nginx container and no Docker at all: Traefik terminates TLS
// and forwards to this loopback listener, which picks the site's project
// directory from its metadata.
package httpd

import (
	"errors"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/stubbedev/srv/internal/site"
)

// Target is one daemon-served site: the project directory to serve plus the
// static options mirrored from the nginx renderer.
type Target struct {
	Name  string
	Root  string
	SPA   bool
	Cache bool
	CORS  bool
}

// wildcard pairs a wildcard domain (apex form, e.g. "foo.test") with the
// target served for any single-level subdomain of it.
type wildcard struct {
	suffix string
	target Target
}

// Server multiplexes daemon-served static sites by Host header.
type Server struct {
	mu        sync.RWMutex
	exact     map[string]Target
	wildcards []wildcard
	srv       *http.Server
	// Logger, when set, receives one structured access event per request,
	// tagged with the site name. The daemon points it at the daemon log file;
	// `srv logs <site>` filters those lines back out.
	Logger *zerolog.Logger
}

// New creates a server bound to addr. Targets start empty; call Reload once
// serving (the daemon re-Reloads whenever site metadata changes).
func New(addr string) *Server {
	s := &Server{exact: make(map[string]Target)}
	s.srv = &http.Server{
		Addr:              addr,
		Handler:           s,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s
}

// SetTargets replaces the host table. Exact domains win over wildcard
// suffixes, matching the router precedence of the Traefik rules.
func (s *Server) SetTargets(exact map[string]Target, wildcards map[string]Target) {
	w := make([]wildcard, 0, len(wildcards))
	for suffix, t := range wildcards {
		w = append(w, wildcard{suffix: strings.ToLower(suffix), target: t})
	}
	s.mu.Lock()
	s.exact = exact
	s.wildcards = w
	s.mu.Unlock()
}

// Reload rebuilds the host table from the registered site metadata: every
// static site flagged daemon-served becomes a target keyed by all of its
// domains. A wildcard site matches its literal domains exactly plus any
// single-level subdomain — the same split as the Traefik Host/HostRegexp rule.
func (s *Server) Reload() error {
	sites, err := site.ListBasic()
	if err != nil {
		return err
	}
	exact := make(map[string]Target)
	wildcards := make(map[string]Target)
	for _, st := range sites {
		if !st.DaemonServed || st.IsBroken || st.Dir == "" {
			continue
		}
		t := Target{Name: st.Name, Root: st.Dir, SPA: st.SPA, Cache: st.Cache, CORS: st.CORS}
		for _, d := range st.Domains {
			d = strings.ToLower(d)
			exact[d] = t
			if st.Wildcard {
				wildcards[d] = t
			}
		}
	}
	s.SetTargets(exact, wildcards)
	return nil
}

// Addr returns the bound address (fixed once Serve is running).
func (s *Server) Addr() string { return s.srv.Addr }

// Serve blocks serving requests until Shutdown.
func (s *Server) Serve() error {
	err := s.srv.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Shutdown stops the server.
func (s *Server) Shutdown() { _ = s.srv.Close() }

// ServeHTTP dispatches one request to its site's project directory.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	t, ok := s.lookup(hostnameOnly(r.Host))
	if !ok {
		http.Error(w, "no daemon-served site for this host", http.StatusNotFound)
		return
	}
	if s.Logger == nil {
		t.serve(w, r)
		return
	}
	lw := &logResponseWriter{ResponseWriter: w, status: http.StatusOK}
	start := time.Now()
	t.serve(lw, r)
	s.logRequest(t.Name, r, lw.status, lw.bytes, time.Since(start))
}

// logRequest emits one access event; the level follows the response class so
// a noisy 404 scanner can be filtered out of the daemon log by level.
func (s *Server) logRequest(siteName string, r *http.Request, status int, bytes int64, dur time.Duration) {
	var event *zerolog.Event
	switch {
	case status >= 500:
		event = s.Logger.Error()
	case status >= 400:
		event = s.Logger.Warn()
	default:
		event = s.Logger.Info()
	}
	event.Str("site", siteName).
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Int("status", status).
		Int64("bytes", bytes).
		Float64("dur_ms", float64(dur.Microseconds())/1000).
		Msg("request")
}

// lookup resolves a hostname to its target via the exact table first, then
// the single-level wildcard suffixes (mirroring Traefik's HostRegexp rules).
func (s *Server) lookup(host string) (Target, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if t, ok := s.exact[host]; ok {
		return t, true
	}
	for _, wc := range s.wildcards {
		if strings.HasSuffix(host, "."+wc.suffix) {
			return wc.target, true
		}
	}
	return Target{}, false
}

// hostnameOnly lowercases r.Host and strips any port.
func hostnameOnly(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}

// =============================================================================
// Per-target file serving — mirrors the generated nginx.conf semantics
// =============================================================================

// sensitiveExtensions is the deny list from the nginx renderer: files that
// belong to the project, not the published site.
var sensitiveExtensions = map[string]bool{
	"env": true, "git": true, "gitignore": true, "gitmodules": true,
	"htaccess": true, "htpasswd": true, "ds_store": true,
	"yml": true, "yaml": true, "toml": true, "ini": true, "log": true,
	"sh": true, "sql": true, "bak": true, "swp": true, "tmp": true,
}

// sensitiveDirectories are directory names never served.
var sensitiveDirectories = map[string]bool{
	"node_modules": true, "vendor": true, ".git": true, ".svn": true, ".hg": true,
}

// cacheableExtensions get year-long immutable caching when the site opts
// into asset caching; everything else is served uncached.
var cacheableExtensions = map[string]bool{
	"css": true, "js": true, "png": true, "jpg": true, "jpeg": true,
	"gif": true, "ico": true, "svg": true, "woff": true, "woff2": true,
	"ttf": true, "eot": true,
}

func (t Target) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions && t.CORS {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	t.setHeaders(w, r.URL.Path)

	root, err := os.OpenRoot(t.Root)
	if err != nil {
		// The project directory is gone (removed site, unmounted path): no
		// custom 404 page is reachable either, so answer plainly.
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}
	defer func() { _ = root.Close() }()

	name := path.Clean("/" + r.URL.Path)
	if denied(name) {
		t.serveError(w, r, root, http.StatusNotFound)
		return
	}

	// "/" addresses the root itself; os.Root needs the relative form ".".
	rel := strings.TrimPrefix(name, "/")
	if rel == "" {
		rel = "."
	}

	f, st, err := openForRead(root, rel)
	if err != nil {
		// SPA fallback serves every miss from index.html, matching the nginx
		// renderer's try_files $uri $uri/ /index.html.
		if !t.serveSPA(w, r, root) {
			t.serveError(w, r, root, http.StatusNotFound)
		}
		return
	}
	defer func() { _ = f.Close() }()

	if st.IsDir() {
		_ = f.Close()
		// Directories without an index are never listed (autoindex off).
		f, st, err = openForRead(root, path.Join(rel, "index.html"))
		if err != nil {
			t.serveError(w, r, root, http.StatusNotFound)
			return
		}
		defer func() { _ = f.Close() }()
	}

	http.ServeContent(w, r, st.Name(), st.ModTime(), f)
}

// openForRead opens name under root and stats it. os.Root makes traversal
// structurally impossible: names are resolved inside the site directory.
func openForRead(root *os.Root, name string) (*os.File, fs.FileInfo, error) {
	f, err := root.Open(name)
	if err != nil {
		return nil, nil, err
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	return f, st, nil
}

// serveSPA falls back to the site's index.html when the site opts in.
func (t Target) serveSPA(w http.ResponseWriter, r *http.Request, root *os.Root) bool {
	return t.SPA && t.serveNamed(w, r, root, "index.html", http.StatusOK)
}

// serveError answers with the site's custom 404.html when present.
func (t Target) serveError(w http.ResponseWriter, r *http.Request, root *os.Root, code int) {
	if t.serveNamed(w, r, root, "404.html", code) {
		return
	}
	http.Error(w, http.StatusText(code), code)
}

// serveNamed serves one file from the site root with the given status.
func (t Target) serveNamed(w http.ResponseWriter, r *http.Request, root *os.Root, name string, code int) bool {
	f, st, err := openForRead(root, name)
	if err != nil || st.IsDir() {
		if f != nil {
			_ = f.Close()
		}
		return false
	}
	defer func() { _ = f.Close() }()
	http.ServeContent(&statusPreservingWriter{ResponseWriter: w, code: code}, r, st.Name(), st.ModTime(), f)
	return true
}

// setHeaders applies the header set the nginx renderer emits: security
// headers always, cache policy and CORS per the site's options.
func (t Target) setHeaders(w http.ResponseWriter, reqPath string) {
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-XSS-Protection", "1; mode=block")

	if t.CORS {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, HEAD")
		w.Header().Set("Access-Control-Allow-Headers", "Origin, X-Requested-With, Content-Type, Accept, Authorization")
	}

	ext := strings.ToLower(strings.TrimPrefix(path.Ext(reqPath), "."))
	switch {
	case t.Cache && cacheableExtensions[ext]:
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	case !t.Cache:
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
	}
}

// denied reports whether a cleaned absolute path must never be served:
// hidden files, sensitive extensions, and sensitive directories.
func denied(name string) bool {
	for seg := range strings.SplitSeq(strings.TrimPrefix(name, "/"), "/") {
		if seg == "" {
			continue
		}
		if strings.HasPrefix(seg, ".") || sensitiveDirectories[seg] {
			return true
		}
		if sensitiveExtensions[strings.ToLower(strings.TrimPrefix(path.Ext(seg), "."))] {
			return true
		}
	}
	return false
}

// statusPreservingWriter keeps a handler-set status code (the 404 of a custom
// 404.html) across http.ServeContent, which would otherwise write 200.
type statusPreservingWriter struct {
	http.ResponseWriter
	code int
}

func (w *statusPreservingWriter) WriteHeader(code int) {
	if code == http.StatusOK {
		code = w.code
	}
	w.ResponseWriter.WriteHeader(code)
}

// logResponseWriter records the status and byte count of one request for the
// access log.
type logResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *logResponseWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *logResponseWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	w.bytes += int64(n)
	return n, err
}
