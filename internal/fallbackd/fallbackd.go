// Package fallbackd is srv's embedded fallback proxy: for every proxy created
// with `srv proxy add --fallback`, the daemon runs a small in-process HTTP
// server that forwards to the primary upstream and transparently re-proxies
// to the fallback URL when the primary answers 5xx (or is unreachable). This
// replaces the nginx:alpine sidecar container for localhost-primary proxies —
// the daemon is already resident, so a container (and an image) per proxy was
// one moving part too many.
//
// Reachability: on Linux, Traefik runs host-networked and dials the listener
// on 127.0.0.1; on Docker Desktop (macOS), it reaches the same host loopback
// listener via the host.docker.internal alias. The listener's port is
// allocated once at `srv proxy add` time and persisted in the proxy metadata,
// so the Traefik route keeps working across daemon restarts.
//
// Requests are served by httputil.ReverseProxy, so WebSocket upgrades pass
// through untouched (a 101 response can never be a 5xx). Fallback interception
// happens in ModifyResponse: a 5xx from the primary is swapped, synchronously,
// for the fallback's response. As with the nginx error_page it replaces, a
// request body already streamed to the primary is not replayed to the
// fallback (GET/HEAD are unaffected; nginx had the same limitation).
package fallbackd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Spec configures one embedded fallback proxy.
type Spec struct {
	Name string // proxy name; keys the manager's server map
	// PrimaryURL is the upstream to forward to, e.g. http://127.0.0.1:3000.
	PrimaryURL string
	// FallbackURL is the remote site 5xx responses re-proxy to.
	FallbackURL string
	// Timeout bounds the dial to the primary before falling back.
	Timeout time.Duration
	// ListenPort is the 127.0.0.1 port to serve on. It is allocated once by
	// the caller and persisted in proxy metadata for stability.
	ListenPort int
}

// AllocatePort asks the OS for an unused TCP port on 127.0.0.1 by binding
// port 0 and reading back the assignment. There is an unavoidable race
// between releasing the port and the daemon binding it, but the window is
// tiny and the daemon reconciles immediately after.
func AllocatePort() (int, error) {
	var lc net.ListenConfig
	l, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("allocate loopback port: %w", err)
	}
	defer func() { _ = l.Close() }()
	addr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("unexpected listener address type %T", l.Addr())
	}
	return addr.Port, nil
}

// Manager owns the daemon's set of embedded fallback proxies. It is
// safe for concurrent use; the daemon reconciles from proxy metadata on
// startup and on every proxies-dir change.
type Manager struct {
	mu      sync.Mutex
	servers map[string]*http.Server
}

func NewManager() *Manager {
	return &Manager{servers: map[string]*http.Server{}}
}

// Ensure starts (or reconfigures) the embedded proxy named spec.Name and
// returns the local address it serves, e.g. "127.0.0.1:41233". Reconciling is
// idempotent: a server already running with the same configuration is left
// alone.
func (m *Manager) Ensure(spec Spec) (string, error) {
	if spec.Name == "" || spec.ListenPort <= 0 {
		return "", errors.New("fallbackd: name and listen port are required")
	}
	if spec.Timeout <= 0 {
		spec.Timeout = 2 * time.Second
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if old, ok := m.servers[spec.Name]; ok {
		// Same config → no-op; different → restart the listener.
		if old.Addr == listenerAddr(spec.ListenPort) {
			return old.Addr, nil
		}
		_ = old.Close()
		delete(m.servers, spec.Name)
	}

	handler, err := newHandler(spec)
	if err != nil {
		return "", err
	}
	srv := &http.Server{
		Addr:              listenerAddr(spec.ListenPort),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	listenCfg := &net.ListenConfig{}
	ln, err := listenCfg.Listen(context.Background(), "tcp", srv.Addr)
	if err != nil {
		return "", fmt.Errorf("fallback proxy %s: bind %s: %w", spec.Name, srv.Addr, err)
	}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			// The listener died (crash, port stolen). Log loudly; the next
			// metadata change or daemon restart reconciles it back.
			log.Printf("fallbackd: proxy %s stopped: %v", spec.Name, err)
			m.mu.Lock()
			delete(m.servers, spec.Name)
			m.mu.Unlock()
		}
	}()
	m.servers[spec.Name] = srv
	return srv.Addr, nil
}

// Remove stops the embedded proxy for name. A missing proxy is a no-op.
func (m *Manager) Remove(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if srv, ok := m.servers[name]; ok {
		_ = srv.Close()
		delete(m.servers, name)
	}
}

// Active lists the names of the embedded proxies currently served.
func (m *Manager) Active() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	names := make([]string, 0, len(m.servers))
	for name := range m.servers {
		names = append(names, name)
	}
	return names
}

// Shutdown stops every embedded proxy; the daemon calls it on exit.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for name, srv := range m.servers {
		_ = srv.Close()
		delete(m.servers, name)
	}
}

func listenerAddr(port int) string {
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
}

// newHandler builds the primary/fallback failover handler for one proxy.
func newHandler(spec Spec) (http.Handler, error) {
	primary, err := url.Parse(spec.PrimaryURL)
	if err != nil {
		return nil, fmt.Errorf("fallbackd %s: invalid primary url: %w", spec.Name, err)
	}
	fallback, err := url.Parse(spec.FallbackURL)
	if err != nil {
		return nil, fmt.Errorf("fallbackd %s: invalid fallback url: %w", spec.Name, err)
	}
	if (fallback.Scheme != "http" && fallback.Scheme != "https") || fallback.Host == "" {
		return nil, fmt.Errorf("fallbackd %s: fallback url must be an absolute http(s) URL", spec.Name)
	}

	primaryClient := &http.Client{
		// Timeout stays zero: proxied responses stream for as long as the
		// client stays. The dial is bounded separately.
		Transport: &http.Transport{
			ResponseHeaderTimeout: spec.Timeout,
		},
	}
	fallbackClient := &http.Client{
		Timeout: spec.Timeout + 10*time.Second, // bounded, but roomier than the primary dial
	}

	proxy := httputil.NewSingleHostReverseProxy(primary)
	proxy.Transport = primaryClient.Transport
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		// Primary unreachable (refused, timeout): same treatment as a 5xx.
		log.Printf("fallbackd %s: primary %s unreachable (%v), falling back", spec.Name, primary.Host, err)
		writeFallback(w, r, fallback, fallbackClient)
	}
	proxy.ModifyResponse = func(resp *http.Response) error {
		if resp.StatusCode < 500 {
			return nil
		}
		// Drain and discard the primary's 5xx body, then re-issue the request
		// against the fallback. resp.Request is the outbound request
		// ReverseProxy constructed, with the URL rewritten to the primary.
		_ = resp.Body.Close()
		log.Printf("fallbackd %s: primary answered %d, falling back", spec.Name, resp.StatusCode)
		// The swapped-in response body's ownership continues with ReverseProxy,
		// which closes it (bodyclose cannot follow the transfer).
		*resp = *fallbackResponse(resp.Request, fallback, fallbackClient) //nolint:bodyclose // ownership continues with ReverseProxy, which closes it
		return nil
	}
	return proxy, nil
}

// writeFallback serves the fallback's response for a request whose primary
// was unreachable (the ErrorHandler path; the 5xx path swaps the response in
// ModifyResponse instead).
func writeFallback(w http.ResponseWriter, r *http.Request, fallback *url.URL, client *http.Client) {
	resp := fallbackResponse(r.Clone(r.Context()), fallback, client)
	defer resp.Body.Close()
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// logSafePath renders the request path for the daemon log with control
// characters neutralised — the path is client-controlled and a crafted one
// must not be able to forge log lines.
func logSafePath(r *http.Request) string {
	var b strings.Builder
	for _, ch := range r.URL.Path {
		if ch < 0x20 || ch == 0x7f {
			b.WriteRune('?')
			continue
		}
		b.WriteRune(ch)
	}
	return b.String()
}

// fallbackResponse issues the request against the fallback site. On failure
// it synthesises the 502 the client would have gotten anyway — the fallback
// being down must not turn a proxied 5xx into a hung connection.
func fallbackResponse(outReq *http.Request, fallback *url.URL, client *http.Client) *http.Response {
	req := outReq.Clone(outReq.Context())
	req.URL.Scheme = fallback.Scheme
	req.URL.Host = fallback.Host
	// The Host header carries the fallback's host (and port, when
	// non-default), so the remote TLS handshake and virtual host match.
	req.Host = fallback.Host
	// ReverseProxy's outbound request and any inbound request both carry
	// RequestURI; http.Client refuses to send one.
	req.RequestURI = ""
	// The outbound request's body was streamed to the primary and is gone;
	// re-issue bodyless. nginx's error_page re-proxy had the same limitation.
	req.Body = nil
	req.ContentLength = 0
	// The response body's ownership transfers to the caller: ModifyResponse
	// hands it to ReverseProxy (which closes it); writeFallback closes it.
	// G704: the target is operator-configured (the proxy's fallback URL) —
	// proxying to it is the product.
	resp, err := client.Do(req) //nolint:gosec // G704: the target is the operator-configured fallback URL
	if err != nil {
		log.Printf("fallbackd: fallback request failed: %v (uri=%q)", err, logSafePath(req))
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Status:     "502 Bad Gateway",
			Proto:      "HTTP/1.1",
			ProtoMajor: 1,
			ProtoMinor: 1,
			Header:     http.Header{"Content-Type": []string{"text/plain"}},
			Body:       http.NoBody,
			Request:    outReq,
		}
	}
	return resp
}
