// Package httpd serves daemon-served static sites ("srv add --daemon") from
// the srv daemon process — the sibling of the embedded DNS server. One
// listener multiplexes every daemon-served site by Host header, so a static
// site needs no nginx container and no Docker at all: Traefik terminates TLS
// and forwards to this loopback listener, which picks the site's project
// directory from its metadata.
package httpd

import (
	"errors"
	"io"
	"io/fs"
	"maps"
	"mime"
	"net"
	"net/http"
	"net/textproto"
	"os"
	"path"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"

	"github.com/stubbedev/srv/internal/domain"
	"github.com/stubbedev/srv/internal/pool"
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

	// headers is the response header set derived from the options above,
	// compiled once by SetTargets so a request copies slice headers into
	// the response instead of canonicalizing keys and allocating values.
	headers []headerField
}

// idleTimeout bounds how long a keep-alive connection may sit idle.
const idleTimeout = 2 * time.Minute

// Server multiplexes daemon-served static sites by Host header.
type Server struct {
	mu    sync.RWMutex
	exact map[string]Target
	// wildcards maps a wildcard site's apex domain ("foo.test") to the target
	// served for its subdomains. lookup walks the host's parent domains, so
	// dispatch costs O(labels) map probes however many sites are registered.
	wildcards map[string]Target
	roots     *rootCache
	srv       *http.Server
	// Logger, when set, receives one structured access event per request,
	// tagged with the site name. The daemon points it at the daemon log file;
	// `srv logs <site>` filters those lines back out.
	Logger *zerolog.Logger
}

// New creates a server bound to addr. Targets start empty; call Reload once
// serving (the daemon re-Reloads whenever site metadata changes).
func New(addr string) *Server {
	s := &Server{
		exact:     make(map[string]Target),
		wildcards: make(map[string]Target),
		roots:     newRootCache(),
	}
	s.srv = &http.Server{
		Addr:              addr,
		Handler:           s,
		ReadHeaderTimeout: 10 * time.Second,
		// Reap idle keep-alive connections: without a bound every client
		// that stops talking holds a socket and a goroutine forever.
		IdleTimeout: idleTimeout,
	}
	return s
}

// SetTargets replaces the host table. Exact domains win over wildcard
// suffixes, matching the router precedence of the Traefik rules.
func (s *Server) SetTargets(exact map[string]Target, wildcards map[string]Target) {
	// Copy both tables: the server owns what it serves from, and every
	// target gets its header set compiled on the way in.
	dirs := make(map[string]bool, len(exact))
	compile := func(src map[string]Target) map[string]Target {
		dst := make(map[string]Target, len(src))
		for host, t := range src {
			t.headers = compileHeaders(t)
			dst[strings.ToLower(host)] = t
			dirs[t.Root] = true
		}
		return dst
	}
	e, w := compile(exact), compile(wildcards)
	s.mu.Lock()
	s.exact = e
	s.wildcards = w
	s.mu.Unlock()
	s.roots.keepOnly(dirs)
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

// Shutdown stops the server and releases the cached site directories.
func (s *Server) Shutdown() {
	_ = s.srv.Close()
	s.roots.closeAll()
}

// ServeHTTP dispatches one request to its site's project directory.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	t, ok := s.lookup(hostnameOnly(r.Host))
	if !ok {
		http.Error(w, "no daemon-served site for this host", http.StatusNotFound)
		return
	}
	if s.Logger == nil {
		t.serve(w, r, s.roots)
		return
	}
	lw := logWriters.Get()
	*lw = logResponseWriter{ResponseWriter: w, status: http.StatusOK}
	start := time.Now()
	t.serve(lw, r, s.roots)
	s.logRequest(t.Name, r, lw.status, lw.bytes, time.Since(start))
	*lw = logResponseWriter{} // drop the ResponseWriter before pooling
	logWriters.Put(lw)
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
// the wildcard apexes: each parent domain of host is probed, nearest first,
// so the most specific wildcard site wins. Traefik's HostRegexp rules decide
// which subdomain depths reach this server at all.
func (s *Server) lookup(host string) (Target, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if t, ok := s.exact[host]; ok {
		return t, true
	}
	return domain.MatchParent(s.wildcards, host)
}

// hostnameOnly lowercases r.Host and strips any port. It allocates nothing
// for the common already-lower-case host: SplitHostPort is only consulted
// when a colon is present, because its error for a port-less host allocates.
func hostnameOnly(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if strings.IndexByte(host, ':') < 0 {
		return host
	}
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

func (t Target) serve(w http.ResponseWriter, r *http.Request, roots *rootCache) {
	// Base headers precede every early return: a preflight 204 without the
	// CORS set always fails the browser's check, and a 405 must keep the
	// security headers (the nginx renderer adds them with `always`).
	t.applyBaseHeaders(w)

	if r.Method == http.MethodOptions && t.CORS {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// The root handle is shared across requests and owned by the cache.
	root, err := roots.get(t.Root)
	if err != nil {
		// The project directory is gone (removed site, unmounted path): no
		// custom 404 page is reachable either, so answer plainly.
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}

	name := cleanPath(r.URL.Path)
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

	// Immutable caching is keyed on the request actually resolving to the
	// named asset. SPA fallbacks and custom 404 pages serve different content
	// under that URL (nginx scopes `expires 1y` to the asset location, which an
	// internal redirect to /index.html or /404.html does not match); stamping
	// them immutable would pin the shell or error page for a year.
	if t.Cache && cacheableExtensions[extLower(r.URL.Path)] {
		h := w.Header()
		h[immutableCacheHeader.key] = immutableCacheHeader.value
	}
	serveFile(w, r, st, f)
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
	serveFile(&statusPreservingWriter{ResponseWriter: w, code: code}, r, st, f)
	return true
}

// headerField is one precompiled response header: a canonical key and a
// shared value slice. Sharing is safe because a value slice has len == cap:
// a later Header().Add appends into a fresh array, never into this one.
type headerField struct {
	key   string
	value []string
}

func field(key, value string) headerField {
	return headerField{key: textproto.CanonicalMIMEHeaderKey(key), value: []string{value}}
}

// The header sets the nginx renderer emits, compiled once per process.
var (
	securityHeaders = []headerField{
		field("X-Frame-Options", "SAMEORIGIN"),
		field("X-Content-Type-Options", "nosniff"),
		field("X-XSS-Protection", "1; mode=block"),
	}
	corsHeaders = []headerField{
		field("Access-Control-Allow-Origin", "*"),
		field("Access-Control-Allow-Methods", "GET, POST, OPTIONS, HEAD"),
		field("Access-Control-Allow-Headers", "Origin, X-Requested-With, Content-Type, Accept, Authorization"),
	}
	noCacheHeaders = []headerField{
		field("Cache-Control", "no-cache, no-store, must-revalidate"),
		field("Pragma", "no-cache"),
		field("Expires", "0"),
	}
	immutableCacheHeader = field("Cache-Control", "public, max-age=31536000, immutable")
)

// compileHeaders derives a target's per-request header set from its options.
// The one path-dependent header (immutable caching for asset extensions) is
// applied by serve, only when the request resolves to that exact asset.
func compileHeaders(t Target) []headerField {
	var cors, noCache []headerField
	if t.CORS {
		cors = corsHeaders
	}
	if !t.Cache {
		noCache = noCacheHeaders
	}
	return slices.Concat(securityHeaders, cors, noCache)
}

// applyBaseHeaders sets the target's compiled header set: security headers
// always, cache policy and CORS per the site's options.
func (t Target) applyBaseHeaders(w http.ResponseWriter) {
	h := w.Header()
	for _, f := range t.headers {
		h[f.key] = f.value
	}
}

// extLower returns p's extension without the dot, lower-cased. It allocates
// only when the extension actually contains upper-case letters.
func extLower(p string) string {
	return strings.ToLower(strings.TrimPrefix(path.Ext(p), "."))
}

// cleanPath is path.Clean of the rooted request path. A request path that is
// already rooted and clean is returned as is, with no allocation.
func cleanPath(p string) string {
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return path.Clean(p)
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
		if sensitiveExtensions[extLower(seg)] {
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

// ReadFrom keeps the sendfile path of the wrapped writer; see copyTo.
func (w *statusPreservingWriter) ReadFrom(r io.Reader) (int64, error) {
	return copyTo(w.ResponseWriter, r)
}

// Unwrap exposes the wrapped writer to http.ResponseController.
func (w *statusPreservingWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// logWriters recycles access-log wrappers: one per request otherwise.
var logWriters pool.Pool[logResponseWriter]

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

// ReadFrom keeps the sendfile path of the wrapped writer; see copyTo.
func (w *logResponseWriter) ReadFrom(r io.Reader) (int64, error) {
	n, err := copyTo(w.ResponseWriter, r)
	w.bytes += n
	return n, err
}

// Unwrap exposes the wrapped writer to http.ResponseController.
func (w *logResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// copyTo copies r into the underlying writer through its io.ReaderFrom when
// it has one. http.ServeContent copies file bodies with io.CopyN, which only
// reaches net/http's sendfile(2) fast path when every wrapper between it and
// the connection implements io.ReaderFrom; a wrapper that only has Write
// silently turns each file into a userspace read/write loop.
func copyTo(w http.ResponseWriter, r io.Reader) (int64, error) {
	if rf, ok := w.(io.ReaderFrom); ok {
		return rf.ReadFrom(r)
	}
	return io.Copy(writerOnly{w}, r)
}

// writerOnly hides any ReadFrom on the target so io.Copy cannot recurse.
type writerOnly struct{ io.Writer }

// contentTypes caches the Content-Type header value for every file extension
// the server has served, resolved with mime.TypeByExtension — the lookup
// http.ServeContent itself makes, so every type the mime table (or the
// system's mime.types) knows is covered, and anything it does not know is
// still sniffed by ServeContent. Reads are a lock-free map probe; a new
// extension copies the map once. Entries are added only for files that
// exist, so the cache is bounded by the extensions on disk, not by the
// names clients request; maxContentTypes caps it regardless.
var contentTypes atomic.Pointer[map[string][]string]

const maxContentTypes = 1024

// contentType returns the shared Content-Type value for ext, or nil when the
// mime table has none (ServeContent then sniffs the body).
func contentType(ext string) []string {
	if m := contentTypes.Load(); m != nil {
		if ct, ok := (*m)[ext]; ok {
			return ct
		}
	}
	var ct []string
	if t := mime.TypeByExtension(ext); t != "" {
		ct = []string{t}
	}
	for {
		cur := contentTypes.Load()
		var old map[string][]string
		if cur != nil {
			old = *cur
		}
		if len(old) >= maxContentTypes {
			return ct
		}
		next := make(map[string][]string, len(old)+1)
		maps.Copy(next, old)
		next[ext] = ct // nil records "unknown" so it is not looked up again
		if contentTypes.CompareAndSwap(cur, &next) {
			return ct
		}
	}
}

// serveFile serves an opened file through http.ServeContent, which owns
// ranges, conditional requests and HEAD. The Content-Type is set first from
// the shared cache: ServeContent keeps a Content-Type it finds instead of
// looking one up and allocating a fresh header value per response.
func serveFile(w http.ResponseWriter, r *http.Request, st fs.FileInfo, f *os.File) {
	if ct := contentType(path.Ext(st.Name())); ct != nil {
		w.Header()["Content-Type"] = ct
	}
	http.ServeContent(w, r, st.Name(), st.ModTime(), f)
}
