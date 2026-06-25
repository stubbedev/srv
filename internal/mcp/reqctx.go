package mcp

// reqctx.go derives per-request context for the shared HTTP transport. A single
// `srv mcp --http` daemon serves many MCP clients (e.g. every running Claude
// Code instance), so a tool cannot assume the daemon's own working directory is
// the caller's. The caller's workspace root is resolved per request — from an
// HTTP header or the client's MCP roots — and relative paths in path-bearing
// tools are anchored to it.
//
// The middleware resolves the root once per call (see newToolMiddleware),
// BEFORE the mutation lock is taken, and stashes it on the context: a
// roots/list round-trip to a slow client must never block other clients. The
// MCP-roots lookup is memoized per session via sessionWorkspace, which the
// middleware holds for the life of the session.
//
// Over stdio (one daemon per client) there is no shared ambiguity: no headers,
// roots are optional, and an empty workspace root preserves the historical
// cwd-relative behavior.

import (
	"context"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// rootHeaders are the HTTP headers a proxy or harness may set to pin the
// caller's workspace root without a roots/list round-trip. Checked in order;
// the first non-empty path wins. Mirrors the convention used by other
// HTTP-served MCP servers in this fleet (jenkins-mcp's X-Repo-Root).
var rootHeaders = []string{"X-Repo-Root", "X-Mcp-Root", "X-Mcp-Roots"}

// wsRootKey is the context key under which the middleware stashes the resolved
// workspace root for a tool call.
type wsRootKey struct{}

// sessionWorkspace memoizes the workspace root derived from a session's MCP
// roots. One lives per session (in the middleware closure), so the client is
// asked at most once and the memo is freed with the session.
type sessionWorkspace struct {
	once sync.Once
	root string
}

// resolve returns the caller's workspace directory for one call. Precedence:
// HTTP header (per-request, never cached) > the session's first MCP root
// (file:// URI, cached) > "" (unknown → anchor to the daemon's cwd, the stdio
// behavior).
func (sw *sessionWorkspace) resolve(ctx context.Context, header http.Header, sess *mcpsdk.ServerSession) string {
	if r := headerRoot(header); r != "" {
		return r
	}
	if sess == nil {
		return ""
	}
	sw.once.Do(func() {
		res, err := sess.ListRoots(ctx, nil)
		if err != nil || res == nil {
			return
		}
		for _, r := range res.Roots {
			if r == nil {
				continue
			}
			if p := normalizeRoot(r.URI); p != "" {
				sw.root = p
				return
			}
		}
	})
	return sw.root
}

// headerRoot extracts a workspace root from the recognized request headers, or
// "" if none carry a usable absolute path.
func headerRoot(header http.Header) string {
	if header == nil {
		return ""
	}
	for _, h := range rootHeaders {
		if v := strings.TrimSpace(header.Get(h)); v != "" {
			// A header value may be a comma-separated list (X-Mcp-Roots); take
			// the first entry.
			if p := normalizeRoot(strings.SplitN(v, ",", 2)[0]); p != "" {
				return p
			}
		}
	}
	return ""
}

// workspaceRoot returns the workspace root for the current tool call. It reads
// the value the middleware resolved and stashed on ctx; if absent (a direct
// call in tests) it falls back to header-only resolution.
func workspaceRoot(ctx context.Context, req *mcpsdk.CallToolRequest) string {
	if v, ok := ctx.Value(wsRootKey{}).(string); ok {
		return v
	}
	if req == nil || req.Extra == nil {
		return ""
	}
	return headerRoot(req.Extra.Header)
}

// normalizeRoot turns a root value (a file:// URI or a plain path) into an
// absolute filesystem path, or "" if it isn't usable.
func normalizeRoot(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if strings.HasPrefix(v, "file://") {
		u, err := url.Parse(v)
		if err != nil {
			return ""
		}
		v = u.Path
	}
	if !filepath.IsAbs(v) {
		return ""
	}
	return filepath.Clean(v)
}

// anchorPath resolves a possibly-relative user-supplied path against the
// caller's workspace root. Absolute paths and ~-prefixed paths pass through
// unchanged (site.ResolvePath expands ~). A relative path with no known
// workspace root also passes through, preserving the daemon-cwd behavior used
// over stdio.
func anchorPath(ctx context.Context, req *mcpsdk.CallToolRequest, path string) string {
	if path == "" || filepath.IsAbs(path) || strings.HasPrefix(path, "~") {
		return path
	}
	if root := workspaceRoot(ctx, req); root != "" {
		return filepath.Join(root, path)
	}
	return path
}
