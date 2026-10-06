package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/stubbedev/srv/internal/site"
)

// preConfirm must not prompt for ack'd or dry-run calls (the handler decides
// those) and must classify non-destructive writes as prompt-free. With no
// elicitation-capable session available it fails open and marks the context
// confirmed, which is the wiring the middleware relies on.
func TestPreConfirmSkipsAckDryRunAndNonDestructive(t *testing.T) {
	base := context.Background()

	acked := fakeRequest{params: &mcpsdk.CallToolParamsRaw{
		Name:      "remove_site",
		Arguments: json.RawMessage(`{"name":"blog","ack":true}`),
	}}
	ctx, ok, reason := preConfirm(base, "remove_site", acked)
	if !ok || reason != "" {
		t.Fatalf("ack'd call prompted: ok=%v reason=%q", ok, reason)
	}
	if ctxConfirmed(ctx) {
		t.Error("ack path must not need a confirmation stamp")
	}

	dry := fakeRequest{params: &mcpsdk.CallToolParamsRaw{
		Name:      "remove_site",
		Arguments: json.RawMessage(`{"name":"blog","dry_run":true}`),
	}}
	if _, ok, _ := preConfirm(base, "remove_site", dry); !ok {
		t.Error("dry_run call prompted")
	}

	if _, ok, _ := preConfirm(base, "add_site", fakeRequest{params: callParams("add_site")}); !ok {
		t.Error("non-destructive write prompted")
	}

	// No session → elicitation impossible → fail open, stamped confirmed so
	// the handler's own confirmDestructive is a no-op.
	ctx, ok, _ = preConfirm(base, "remove_site", fakeRequest{params: callParams("remove_site")})
	if !ok {
		t.Fatal("prompt-free client was refused")
	}
	if !ctxConfirmed(ctx) {
		t.Error("confirmed context not stamped after fail-open elicitation")
	}
}

// The confirmation table must stay inside the write tier (the middleware only
// pre-confirms locked tools) and every message builder must tolerate the
// raw argument shape.
func TestDestructiveConfirmMessagesMatchWriteTier(t *testing.T) {
	for name, build := range destructiveConfirmMessages {
		if !isWriteTool(name) {
			t.Errorf("confirm table entry %q is not a write-tier tool", name)
		}
		if msg := build(map[string]any{}); msg == "" {
			t.Errorf("confirm message for %q is empty", name)
		}
	}
}

// A confirmed context short-circuits the handler-side confirmation, so the
// prompt fires exactly once per call (in the middleware, before the lock).
func TestConfirmDestructiveHonorsConfirmedContext(t *testing.T) {
	if ok, reason := confirmDestructive(withConfirmed(context.Background()), nil, false, false, "msg"); !ok || reason != "" {
		t.Errorf("confirmed context re-prompted: ok=%v reason=%q", ok, reason)
	}
}

// Free-text results (error strings wrapping subprocess output) must be
// scrubbed: api keys and bearer tokens slipped past the key-based redaction.
func TestRedactResultTextScrubsTextContent(t *testing.T) {
	res := &mcpsdk.CallToolResult{Content: []mcpsdk.Content{
		&mcpsdk.TextContent{Text: "start failed: OPENAI_API_KEY=sk-supersecret Authorization: Bearer tok_123"},
	}}
	redactResultText(res)
	text := res.Content[0].(*mcpsdk.TextContent).Text
	if strings.Contains(text, "sk-supersecret") || strings.Contains(text, "tok_123") {
		t.Errorf("secret survived redaction: %q", text)
	}
	if !strings.Contains(text, "[REDACTED]") {
		t.Errorf("placeholder missing: %q", text)
	}
}

func TestRedactStringInlinePatterns(t *testing.T) {
	cases := map[string]string{
		"OPENAI_API_KEY=sk-abc":   "contains redacted",
		"apikey: sk-abc":          "contains redacted",
		"Authorization: Bearer x": "contains redacted",
		"password=hunter2":        "contains redacted",
		"plain text is untouched": "untouched",
	}
	for in, want := range cases {
		got := redactString(in)
		if want == "contains redacted" && !strings.Contains(got, redactedPlaceholder) {
			t.Errorf("redactString(%q) = %q, want a redaction", in, got)
		}
		if want == "untouched" && got != in {
			t.Errorf("redactString(%q) = %q, want unchanged", in, got)
		}
	}
}

// dry_run must resolve the target: {ok:true} for a typo validated nothing.
func TestRemoveSiteDryRunResolvesTarget(t *testing.T) {
	withRoot(t)

	_, out, err := removeSiteTool(context.Background(), nil, removeSiteIn{Name: "ghost", DryRun: true})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if out.OK || !strings.Contains(out.Error, "not found") {
		t.Errorf("dry_run on missing site: %+v — want a not-found error", out)
	}

	if err := site.WriteSiteMetadata("blog", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"blog.test"},
		ProjectPath: t.TempDir(),
		Port:        80,
		IsLocal:     true,
	}); err != nil {
		t.Fatal(err)
	}
	_, out, err = removeSiteTool(context.Background(), nil, removeSiteIn{Name: "blog", DryRun: true})
	if err != nil || !out.OK {
		t.Fatalf("dry_run on existing site: out=%+v err=%v", out, err)
	}
	if out.Preview == nil || out.Preview.Kind != "site" || len(out.Preview.WouldRemove) < 3 {
		t.Errorf("preview = %+v, want the site effects listed", out.Preview)
	}
}

func TestRemoveAliasDryRunChecksRegistration(t *testing.T) {
	withRoot(t)
	if err := site.WriteSiteMetadata("app", site.SiteMetadata{
		Type:        site.SiteTypeStatic,
		Domains:     []string{"app.test", "www.app.test"},
		ProjectPath: t.TempDir(),
		Port:        80,
	}); err != nil {
		t.Fatal(err)
	}

	_, out, _ := removeAliasTool(context.Background(), nil, aliasIn{Name: "app", Alias: "www.app.test", DryRun: true})
	if !out.OK || out.Preview == nil {
		t.Errorf("registered alias preview = %+v", out)
	}
	_, out, _ = removeAliasTool(context.Background(), nil, aliasIn{Name: "app", Alias: "nope.test", DryRun: true})
	if out.OK || !strings.Contains(out.Error, "not registered") {
		t.Errorf("unregistered alias dry_run = %+v, want a rejection", out)
	}
}

func TestRemoveVolumeAndDetachNetworkDryRunCheckMetadata(t *testing.T) {
	withRoot(t)
	if err := site.WriteSiteMetadata("app", site.SiteMetadata{
		Type:          site.SiteTypeStatic,
		Domains:       []string{"app.test"},
		ProjectPath:   t.TempDir(),
		Port:          80,
		Volumes:       []site.VolumeMount{{Source: "/tmp", Target: "/data"}},
		ExtraNetworks: []string{"backnet"},
	}); err != nil {
		t.Fatal(err)
	}

	_, out, _ := removeVolumeTool(context.Background(), nil, removeVolumeIn{Name: "app", Target: "/data", DryRun: true})
	if !out.OK || out.Preview == nil {
		t.Errorf("volume preview = %+v", out)
	}
	_, out, _ = removeVolumeTool(context.Background(), nil, removeVolumeIn{Name: "app", Target: "/nope", DryRun: true})
	if out.OK || !strings.Contains(out.Error, "no volume") {
		t.Errorf("unknown volume dry_run = %+v", out)
	}

	_, out, _ = detachNetworkTool(context.Background(), nil, detachNetworkIn{Name: "app", Network: "backnet", DryRun: true})
	if !out.OK || out.Preview == nil {
		t.Errorf("network preview = %+v", out)
	}
	_, out, _ = detachNetworkTool(context.Background(), nil, detachNetworkIn{Name: "app", Network: "gone", DryRun: true})
	if out.OK || !strings.Contains(out.Error, "not attached") {
		t.Errorf("unknown network dry_run = %+v", out)
	}
}
