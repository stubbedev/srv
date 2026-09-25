package fallbackd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// startTarget runs a test upstream returning the given status + body, plus
// a channel recording the Host header of each request it saw.
func startTarget(t *testing.T, status int, body string, hosts chan<- string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hosts != nil {
			hosts <- r.Host
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFailoverOn5xx(t *testing.T) {
	var primaryHosts, fallbackHosts chan string
	primaryHosts = make(chan string, 4)
	fallbackHosts = make(chan string, 4)
	primary := startTarget(t, http.StatusServiceUnavailable, "primary down", primaryHosts)
	fallback := startTarget(t, http.StatusOK, "fallback content", fallbackHosts)

	m := NewManager()
	t.Cleanup(m.Shutdown)

	port, err := AllocatePort()
	if err != nil {
		t.Fatal(err)
	}
	addr, err := m.Ensure(Spec{
		Name:        "p1",
		PrimaryURL:  primary.URL,
		FallbackURL: fallback.URL,
		Timeout:     2 * time.Second,
		ListenPort:  port,
	})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get("http://" + addr + "/some/path?x=1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(got) != "fallback content" {
		t.Errorf("got %d %q, want 200 fallback content", resp.StatusCode, got)
	}

	// The fallback request must carry the fallback's Host (correct SNI /
	// virtual host) and the original path.
	select {
	case h := <-fallbackHosts:
		if want := hostOf(fallback.URL); h != want {
			t.Errorf("fallback Host = %q, want %q", h, want)
		}
	default:
		t.Fatal("fallback never received a request")
	}
}

func TestPassesThroughHealthyPrimary(t *testing.T) {
	primary := startTarget(t, http.StatusOK, "hello primary", nil)
	fallback := startTarget(t, http.StatusOK, "should not be hit", nil)

	m := NewManager()
	t.Cleanup(m.Shutdown)
	port, _ := AllocatePort()
	addr, err := m.Ensure(Spec{
		Name:        "p2",
		PrimaryURL:  primary.URL,
		FallbackURL: fallback.URL,
		Timeout:     time.Second,
		ListenPort:  port,
	})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get("http://" + addr + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "hello primary" {
		t.Errorf("body = %q, want the primary's", body)
	}
}

func TestFailoverOnUnreachablePrimary(t *testing.T) {
	// Bind a listener and close it: the port is dead by request time.
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := dead.URL
	dead.Close()
	fallback := startTarget(t, http.StatusOK, "from fallback", nil)

	m := NewManager()
	t.Cleanup(m.Shutdown)
	port, _ := AllocatePort()
	addr, err := m.Ensure(Spec{
		Name:        "p3",
		PrimaryURL:  deadURL,
		FallbackURL: fallback.URL,
		Timeout:     time.Second,
		ListenPort:  port,
	})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get("http://" + addr + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "fallback") {
		t.Errorf("got %d %q, want the fallback's 200", resp.StatusCode, body)
	}
}

func TestFallbackDownYields502(t *testing.T) {
	primary := startTarget(t, http.StatusInternalServerError, "down", nil)
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := dead.URL
	dead.Close()

	m := NewManager()
	t.Cleanup(m.Shutdown)
	port, _ := AllocatePort()
	addr, err := m.Ensure(Spec{
		Name:        "p4",
		PrimaryURL:  primary.URL,
		FallbackURL: deadURL,
		Timeout:     time.Second,
		ListenPort:  port,
	})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get("http://" + addr + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 when both upstreams are down", resp.StatusCode)
	}
}

func TestEnsureIdempotentAndReconfiguring(t *testing.T) {
	primary := startTarget(t, http.StatusOK, "p", nil)
	m := NewManager()
	t.Cleanup(m.Shutdown)

	port, _ := AllocatePort()
	spec := Spec{Name: "p5", PrimaryURL: primary.URL, FallbackURL: "https://fb.test", Timeout: time.Second, ListenPort: port}
	addr1, err := m.Ensure(spec)
	if err != nil {
		t.Fatal(err)
	}
	addr2, err := m.Ensure(spec)
	if err != nil {
		t.Fatal(err)
	}
	if addr1 != addr2 {
		t.Errorf("re-Ensure moved the listener: %s vs %s", addr1, addr2)
	}
	if got := m.Active(); len(got) != 1 || got[0] != "p5" {
		t.Errorf("Active() = %v, want [p5]", got)
	}

	// A different port must restart the listener on the new port.
	port2, _ := AllocatePort()
	addr3, err := m.Ensure(Spec{Name: "p5", PrimaryURL: primary.URL, FallbackURL: "https://fb.test", Timeout: time.Second, ListenPort: port2})
	if err != nil {
		t.Fatal(err)
	}
	if addr3 != listenerAddr(port2) {
		t.Errorf("reconfigured addr = %s, want %s", addr3, listenerAddr(port2))
	}

	m.Remove("p5")
	if got := m.Active(); len(got) != 0 {
		t.Errorf("Active() after Remove = %v, want empty", got)
	}
}

func TestEnsureRejectsBadSpecs(t *testing.T) {
	m := NewManager()
	t.Cleanup(m.Shutdown)

	if _, err := m.Ensure(Spec{Name: "", ListenPort: 1}); err == nil {
		t.Error("empty name accepted")
	}
	if _, err := m.Ensure(Spec{Name: "x", ListenPort: 0}); err == nil {
		t.Error("zero port accepted")
	}
	if _, err := m.Ensure(Spec{Name: "x", ListenPort: 1, PrimaryURL: "http://127.0.0.1:1", FallbackURL: "not a url"}); err == nil {
		t.Error("bad fallback url accepted")
	}
	if _, err := m.Ensure(Spec{Name: "x", ListenPort: 1, PrimaryURL: "http://127.0.0.1:1", FallbackURL: "ftp://fb.test"}); err == nil {
		t.Error("non-http fallback scheme accepted")
	}
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Host
}
