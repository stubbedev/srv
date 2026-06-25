package mcp

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestNormalizeRoot(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"relative/path", ""},                // relative → unusable as a root
		{"/abs/path", "/abs/path"},           // plain absolute path
		{"/abs/path/", "/abs/path"},          // trailing slash cleaned
		{"file:///work/repo", "/work/repo"},  // file:// URI
		{"file:///work/repo/", "/work/repo"}, // file:// URI cleaned
		{"  /abs/path  ", "/abs/path"},       // trimmed
		{"file://relative", ""},              // file:// with no abs path
		{"/work/../work/repo", "/work/repo"}, // cleaned
	}
	for _, c := range cases {
		if got := normalizeRoot(c.in); got != c.want {
			t.Errorf("normalizeRoot(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// reqWithHeader builds a CallToolRequest carrying HTTP headers, mirroring what
// the streamable transport populates in req.Extra.Header.
func reqWithHeader(h http.Header) *mcpsdk.CallToolRequest {
	return &mcpsdk.CallToolRequest{Extra: &mcpsdk.RequestExtra{Header: h}}
}

func TestWorkspaceRootFromHeader(t *testing.T) {
	for _, name := range rootHeaders {
		h := http.Header{}
		h.Set(name, "/work/myrepo")
		if got := workspaceRoot(context.Background(), reqWithHeader(h)); got != "/work/myrepo" {
			t.Errorf("header %s: workspaceRoot = %q, want /work/myrepo", name, got)
		}
	}

	// file:// URI in a header resolves to a path.
	h := http.Header{}
	h.Set("X-Repo-Root", "file:///work/repo")
	if got := workspaceRoot(context.Background(), reqWithHeader(h)); got != "/work/repo" {
		t.Errorf("file:// header: workspaceRoot = %q, want /work/repo", got)
	}

	// Comma-separated list (X-Mcp-Roots): first entry wins.
	h = http.Header{}
	h.Set("X-Mcp-Roots", "/first,/second")
	if got := workspaceRoot(context.Background(), reqWithHeader(h)); got != "/first" {
		t.Errorf("list header: workspaceRoot = %q, want /first", got)
	}
}

func TestWorkspaceRootHeaderPrecedence(t *testing.T) {
	// X-Repo-Root is checked before the other header names.
	h := http.Header{}
	h.Set("X-Mcp-Root", "/second")
	h.Set("X-Repo-Root", "/first")
	if got := workspaceRoot(context.Background(), reqWithHeader(h)); got != "/first" {
		t.Errorf("precedence: workspaceRoot = %q, want /first (X-Repo-Root)", got)
	}
}

func TestWorkspaceRootUnknown(t *testing.T) {
	if got := workspaceRoot(context.Background(), nil); got != "" {
		t.Errorf("nil request: workspaceRoot = %q, want empty", got)
	}
	// No headers, no session → unknown.
	if got := workspaceRoot(context.Background(), reqWithHeader(http.Header{})); got != "" {
		t.Errorf("empty headers: workspaceRoot = %q, want empty", got)
	}
}

func TestAnchorPath(t *testing.T) {
	h := http.Header{}
	h.Set("X-Repo-Root", "/work/repo")
	req := reqWithHeader(h)
	ctx := context.Background()

	// Relative path anchored to the workspace root.
	if got := anchorPath(ctx, req, "sub/dir"); got != filepath.Join("/work/repo", "sub/dir") {
		t.Errorf("relative: anchorPath = %q", got)
	}
	// Absolute path passes through unchanged.
	if got := anchorPath(ctx, req, "/etc/thing"); got != "/etc/thing" {
		t.Errorf("absolute: anchorPath = %q, want /etc/thing", got)
	}
	// ~-prefixed passes through (site.ResolvePath expands it).
	if got := anchorPath(ctx, req, "~/proj"); got != "~/proj" {
		t.Errorf("tilde: anchorPath = %q, want ~/proj", got)
	}
	// Empty stays empty.
	if got := anchorPath(ctx, req, ""); got != "" {
		t.Errorf("empty: anchorPath = %q, want empty", got)
	}
	// Relative path with no known workspace → unchanged (stdio cwd behavior).
	if got := anchorPath(ctx, reqWithHeader(http.Header{}), "sub/dir"); got != "sub/dir" {
		t.Errorf("no workspace: anchorPath = %q, want sub/dir", got)
	}
}
