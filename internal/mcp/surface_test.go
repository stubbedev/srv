package mcp

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/stubbedev/srv/internal/config"
)

// ─── tool manifest ───────────────────────────────────────────────────────
//
// ToolManifest spins up the real server over an in-memory transport, so it is
// the closest thing to an integration test of the advertised surface. It also
// feeds the README table, which CI checks for drift.

func TestToolManifestCoversEveryRegisteredTool(t *testing.T) {
	docs, err := ToolManifest(t.Context())
	if err != nil {
		t.Fatalf("ToolManifest: %v", err)
	}
	if len(docs) == 0 {
		t.Fatal("ToolManifest returned nothing")
	}

	got := make(map[string]ToolDoc, len(docs))
	for _, d := range docs {
		got[d.Name] = d
	}
	for _, want := range slices.Concat(coreToolNames, readToolNames, writeToolNames) {
		if _, ok := got[want]; !ok {
			t.Errorf("tool %q is registered but missing from the manifest", want)
		}
	}
	if len(got) != len(docs) {
		t.Error("ToolManifest returned duplicate tool names")
	}
}

func TestToolManifestTagsEveryToolWithAKnownTier(t *testing.T) {
	docs, err := ToolManifest(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range docs {
		if _, ok := tierRank[d.Tier]; !ok {
			t.Errorf("tool %q has tier %q, which is not one of core/read/write", d.Name, d.Tier)
		}
		if strings.TrimSpace(d.Description) == "" {
			t.Errorf("tool %q has no description — it would render as an empty README cell", d.Name)
		}
	}
}

// Sorting is what keeps the generated README stable; an unstable order would
// show up as perpetual CI drift.
func TestToolManifestIsSortedByTierThenName(t *testing.T) {
	docs, err := ToolManifest(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(docs); i++ {
		prev, cur := docs[i-1], docs[i]
		if tierRank[prev.Tier] > tierRank[cur.Tier] {
			t.Fatalf("tier order broken at %d: %s(%s) before %s(%s)", i, prev.Name, prev.Tier, cur.Name, cur.Tier)
		}
		if prev.Tier == cur.Tier && prev.Name > cur.Name {
			t.Fatalf("name order broken within tier %s: %q before %q", cur.Tier, prev.Name, cur.Name)
		}
	}
}

func TestToolManifestIsDeterministic(t *testing.T) {
	a, err := ToolManifest(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	b, err := ToolManifest(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != len(b) {
		t.Fatalf("manifest length differs between runs: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Errorf("manifest entry %d differs between runs: %+v vs %+v", i, a[i], b[i])
		}
	}
}

// ─── tierOf ──────────────────────────────────────────────────────────────

func TestTierOf(t *testing.T) {
	m := tierOf()
	for _, n := range coreToolNames {
		if m[n] != "core" {
			t.Errorf("tierOf()[%q] = %q, want core", n, m[n])
		}
	}
	for _, n := range readToolNames {
		if m[n] != "read" {
			t.Errorf("tierOf()[%q] = %q, want read", n, m[n])
		}
	}
	for _, n := range writeToolNames {
		if m[n] != "write" {
			t.Errorf("tierOf()[%q] = %q, want write", n, m[n])
		}
	}
	if len(m) != len(coreToolNames)+len(readToolNames)+len(writeToolNames) {
		t.Error("a tool name appears in more than one tier")
	}
}

// Every mutating tool must be in the write tier, never read: the read tier is
// what a client activates when it only wants inspection.
func TestNoToolIsInBothReadAndWriteTiers(t *testing.T) {
	for _, w := range writeToolNames {
		if slices.Contains(readToolNames, w) {
			t.Errorf("tool %q is in both the read and write tiers", w)
		}
	}
}

// ─── resources ───────────────────────────────────────────────────────────

func readResource(t *testing.T, uri string, fn func(context.Context, *mcpsdk.ReadResourceRequest) (*mcpsdk.ReadResourceResult, error)) map[string]any {
	t.Helper()
	res, err := fn(t.Context(), &mcpsdk.ReadResourceRequest{
		Params: &mcpsdk.ReadResourceParams{URI: uri},
	})
	if err != nil {
		t.Fatalf("resource %s: %v", uri, err)
	}
	if len(res.Contents) != 1 {
		t.Fatalf("resource %s returned %d contents, want 1", uri, len(res.Contents))
	}
	c := res.Contents[0]
	if c.URI != uri {
		t.Errorf("content URI = %q, want %q", c.URI, uri)
	}
	if c.MIMEType != "application/json" {
		t.Errorf("content MIME = %q, want application/json", c.MIMEType)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(c.Text), &out); err != nil {
		t.Fatalf("resource %s is not valid JSON: %v\n%s", uri, err, c.Text)
	}
	return out
}

func TestPathsResource(t *testing.T) {
	root := withRoot(t)
	out := readResource(t, resourcePaths, pathsResource)
	if out["config_root"] != root {
		t.Errorf("config_root = %v, want %v", out["config_root"], root)
	}
	for _, k := range []string{"sites_dir", "proxies_dir", "traefik_dir", "traefik_conf_dir", "user_config_file"} {
		if v, ok := out[k]; !ok || v == "" {
			t.Errorf("paths resource missing %q", k)
		}
	}
}

func TestUserConfigResource(t *testing.T) {
	withRoot(t)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SaveUserConfig(&config.UserConfig{
		ContainerEngine: "podman",
		ParkedPaths:     []string{"/srv/projects"},
	}); err != nil {
		t.Fatal(err)
	}

	out := readResource(t, resourceUserConfig, userConfigResource)
	// The resource goes through ops.UserConfigJSON, so the keys are the ones in
	// config.yml and in the published schema. It used to emit Go field names —
	// UserConfig carries only yaml tags — and an agent that read this resource
	// would have written back keys srv ignores.
	if out["container_engine"] != "podman" {
		t.Errorf("container_engine = %v, want podman (keys: %v)", out["container_engine"], slices.Sorted(maps.Keys(out)))
	}
	for _, goName := range []string{"ContainerEngine", "ParkedPaths", "UpstreamDNS"} {
		if _, leaked := out[goName]; leaked {
			t.Errorf("resource leaked the Go field name %q", goName)
		}
	}
}

// An absent config.yml is the normal first-run state, not an error.
func TestUserConfigResourceWithNoConfigFile(t *testing.T) {
	withRoot(t)
	readResource(t, resourceUserConfig, userConfigResource)
}

func TestResourceJSONShape(t *testing.T) {
	res := resourceJSON("srv://x", []byte(`{"a":1}`))
	if len(res.Contents) != 1 {
		t.Fatalf("got %d contents, want 1", len(res.Contents))
	}
	c := res.Contents[0]
	if c.URI != "srv://x" || c.MIMEType != "application/json" || c.Text != `{"a":1}` {
		t.Errorf("resourceJSON produced %+v", c)
	}
}

// ─── confirmDestructive ──────────────────────────────────────────────────
//
// This gate stands between an agent and `srv remove`. Its contract is
// deliberately permissive — a client that cannot elicit must not be blocked —
// so each short-circuit needs pinning, or a future "safer" edit could either
// break non-interactive agents or silently stop asking.

func TestConfirmDestructiveShortCircuits(t *testing.T) {
	cases := []struct {
		name        string
		dryRun, ack bool
		req         *mcpsdk.CallToolRequest
		wantOK      bool
	}{
		{name: "dry run never prompts", dryRun: true, wantOK: true},
		{name: "ack pre-authorizes", ack: true, wantOK: true},
		{name: "nil request proceeds", req: nil, wantOK: true},
		{name: "no session proceeds", req: &mcpsdk.CallToolRequest{}, wantOK: true},
	}
	for _, c := range cases {
		ok, reason := confirmDestructive(t.Context(), c.req, c.dryRun, c.ack, "remove it?")
		if ok != c.wantOK {
			t.Errorf("%s: ok = %v, want %v", c.name, ok, c.wantOK)
		}
		if ok && reason != "" {
			t.Errorf("%s: reason = %q, want empty on approval", c.name, reason)
		}
	}
}

// ─── write-tool gating ───────────────────────────────────────────────────

// The destructive-tool set is derived from writeToolNames so the two cannot
// drift; assert the derivation rather than a hand-copied list.
func TestWriteToolsAreRecognisedAsMutating(t *testing.T) {
	for _, n := range writeToolNames {
		if !isWriteTool(n) {
			t.Errorf("write-tier tool %q is not seen as a write by the middleware", n)
		}
	}
	for _, n := range readToolNames {
		if isWriteTool(n) {
			t.Errorf("read-tier tool %q is seen as a write by the middleware", n)
		}
	}
}
