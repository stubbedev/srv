package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/shell"
)

// httpSessionTimeout closes a session that has gone idle this long, freeing its
// per-session server (and its workspace memo). Clients reconnect transparently.
const httpSessionTimeout = 30 * time.Minute

// HTTPOptions configures the Streamable HTTP transport.
type HTTPOptions struct {
	// Addr is the host:port to listen on. Empty → DefaultMCPHTTPAddr (loopback).
	Addr string
	// Path is the endpoint path MCP is mounted at. Empty → DefaultMCPHTTPPath.
	Path string
	// TrustedOrigins are browser Origins allowed through cross-origin
	// protection, for browser-based MCP clients hitting an off-host bind. CLI
	// clients send no Origin and are always allowed; this is rarely needed.
	TrustedOrigins []string
}

// ServeHTTP boots the MCP server over Streamable HTTP and blocks until ctx is
// cancelled. One long-running daemon can serve every MCP client on the host
// (each Claude Code instance, etc.); per-request workspace context is resolved
// from MCP roots and request headers (see reqctx.go), so a single instance is
// shared safely. A GET /healthz liveness endpoint is always mounted.
//
// The endpoint binds loopback with no auth by default — it mutates a privileged
// Traefik edge, so it trusts local processes only. Front it with a reverse
// proxy for TLS + auth before binding off-host. The SDK's localhost/DNS-rebind
// protection and stdlib cross-origin (CSRF) protection are both active.
//
// Like stdio, the HTTP transport has no TTY, so sudo is forced non-interactive.
func ServeHTTP(ctx context.Context, opts HTTPOptions) error {
	shell.SetNonInteractive(true)

	addr := opts.Addr
	if addr == "" {
		addr = constants.DefaultMCPHTTPAddr
	}
	path := opts.Path
	if path == "" {
		path = constants.DefaultMCPHTTPPath
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// A fresh server per session keeps lazy activation per-client: each client's
	// srv_activate only unlocks tiers for its own session, exactly as a
	// dedicated stdio process would, and its workspace memo is freed with it.
	handler := mcpsdk.NewStreamableHTTPHandler(
		func(*http.Request) *mcpsdk.Server { return newServer() },
		&mcpsdk.StreamableHTTPOptions{
			SessionTimeout: httpSessionTimeout,
			Logger:         logger,
		},
	)

	mux := http.NewServeMux()
	mux.Handle(path, handler)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// Cross-origin (CSRF) protection: reject cross-origin *browser* requests.
	// Requests with no Origin / Sec-Fetch-Site (CLI MCP clients) and safe
	// methods (the GET /healthz probe) pass through untouched.
	cop := http.NewCrossOriginProtection()
	for _, o := range opts.TrustedOrigins {
		if err := cop.AddTrustedOrigin(o); err != nil {
			return fmt.Errorf("invalid trusted origin %q: %w", o, err)
		}
	}

	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           cop.Handler(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("srv mcp serving", "transport", "streamable-http", "url", fmt.Sprintf("http://%s%s", addr, path))
		errCh <- httpSrv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		// Cobra cancels ctx on SIGINT/SIGTERM; drain in-flight requests briefly.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("mcp http shutdown: %w", err)
		}
		logger.Info("srv mcp stopped")
		return nil
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("mcp http server: %w", err)
		}
		return nil
	}
}
