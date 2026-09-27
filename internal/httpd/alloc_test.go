package httpd

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/rs/zerolog"

	"github.com/stubbedev/srv/internal/allocbudget"
)

// discardWriter is a minimal ResponseWriter that allocates nothing itself,
// so allocation counts measure the server, not the recorder.
type discardWriter struct{ h http.Header }

func (w *discardWriter) Header() http.Header         { return w.h }
func (w *discardWriter) Write(p []byte) (int, error) { return len(p), nil }
func (w *discardWriter) WriteHeader(int)             {}
func (w *discardWriter) ReadFrom(r io.Reader) (int64, error) {
	return io.Copy(io.Discard, r)
}

func (w *discardWriter) reset() { clear(w.h) }

func benchServer(tb testing.TB, logged bool) (*Server, *http.Request) {
	tb.Helper()
	root := tb.TempDir()
	writeFile(tb, root, "index.html", "<h1>hi</h1>")
	s := New("127.0.0.1:0")
	if logged {
		zl := zerolog.New(io.Discard).With().Timestamp().Logger()
		s.Logger = &zl
	}
	s.SetTargets(map[string]Target{"s.test": {Name: "s.test", Root: root}}, nil)
	req := httptest.NewRequest(http.MethodGet, "http://s.test/index.html", nil)
	return s, req
}

// serveHitAllocBudget is the allocation count of one cache-hit file request,
// access log included. All of it sits in the stdlib: os.Root.Open (path
// split and join, the *os.File, Stat) and http.ServeContent (its header
// values, Last-Modified formatting, the LimitReader around the body). The
// server's own code — dispatch, headers, logging — allocates nothing, and
// this guard keeps it that way.
var serveHitAllocBudget = allocbudget.Budget{Allocs: 11, FileOpens: 1}

func TestServeHTTPAllocationBudget(t *testing.T) {
	for _, logged := range []bool{false, true} {
		s, req := benchServer(t, logged)
		w := &discardWriter{h: http.Header{}}
		allocbudget.Check(t, fmt.Sprintf("file hit, logged=%v", logged), serveHitAllocBudget, func() {
			w.reset()
			s.ServeHTTP(w, req)
		})
	}
}

func BenchmarkServeHTTPHit(b *testing.B) {
	s, req := benchServer(b, true)
	w := &discardWriter{h: http.Header{}}
	b.ReportAllocs()
	for b.Loop() {
		w.reset()
		s.ServeHTTP(w, req)
	}
}

// Every extension gets exactly the Content-Type http.ServeContent would pick:
// the mime table's answer when it has one, a sniffed one otherwise.
func TestContentTypeMatchesServeContent(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"a.html": "<h1>x</h1>", "b.CSS": "body{}", "c.wasm": "\x00asm", "d.avif": "x",
		"e.webmanifest": "{}", "f.csv": "a,b", "g.unknownext": "plain words", "h.bin": "\x00\x01\x02",
	}
	targets := map[string]Target{"s.test": {Name: "s.test", Root: root}}
	for name, body := range files {
		writeFile(t, root, name, body)
	}
	s := New("127.0.0.1:0")
	s.SetTargets(targets, nil)
	for name := range files {
		for range 2 { // miss, then cached hit
			_, _, h := get(t, s, "s.test", "/"+name)
			want := httptest.NewRecorder()
			f, err := os.Open(root + "/" + name)
			if err != nil {
				t.Fatal(err)
			}
			st, _ := f.Stat()
			http.ServeContent(want, httptest.NewRequest(http.MethodGet, "/"+name, nil), st.Name(), st.ModTime(), f)
			_ = f.Close()
			if got, w := h.Get("Content-Type"), want.Header().Get("Content-Type"); got != w {
				t.Errorf("%s: Content-Type %q, ServeContent picks %q", name, got, w)
			}
		}
	}
}
