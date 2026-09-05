package main

import (
	"strings"
	"testing"

	"github.com/invopop/jsonschema"
	"github.com/spf13/cobra"
)

// ─── replaceBlock ────────────────────────────────────────────────────────
//
// replaceBlock is the whole safety story of gen-readme: it must rewrite only
// what sits between a marker pair and never touch the hand-written prose
// around it. CI fails on README drift, so a bug here corrupts the file on
// every run.

func TestReplaceBlockRewritesOnlyBetweenMarkers(t *testing.T) {
	doc := "before\n<!-- BEGIN:cli -->\nstale\n<!-- END:cli -->\nafter\n"
	got, err := replaceBlock(doc, "cli", "fresh")
	if err != nil {
		t.Fatal(err)
	}
	want := "before\n<!-- BEGIN:cli -->\nfresh\n<!-- END:cli -->\nafter\n"
	if got != want {
		t.Errorf("replaceBlock() = %q, want %q", got, want)
	}
}

func TestReplaceBlockIsIdempotent(t *testing.T) {
	doc := "x\n<!-- BEGIN:cli -->\nold\n<!-- END:cli -->\ny\n"
	once, err := replaceBlock(doc, "cli", "body")
	if err != nil {
		t.Fatal(err)
	}
	twice, err := replaceBlock(once, "cli", "body")
	if err != nil {
		t.Fatal(err)
	}
	if once != twice {
		t.Errorf("replaceBlock is not idempotent:\nfirst:  %q\nsecond: %q", once, twice)
	}
}

// Trailing newlines in the generated body must not accumulate: each run would
// otherwise add a blank line and CI would report perpetual drift.
func TestReplaceBlockNormalisesTrailingNewlines(t *testing.T) {
	doc := "<!-- BEGIN:x -->\n\n<!-- END:x -->\n"
	got, err := replaceBlock(doc, "x", "body\n\n\n")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "body\n\n") {
		t.Errorf("replaceBlock() kept extra blank lines: %q", got)
	}
}

func TestReplaceBlockLeavesOtherBlocksAlone(t *testing.T) {
	doc := "<!-- BEGIN:a -->\nA\n<!-- END:a -->\n<!-- BEGIN:b -->\nB\n<!-- END:b -->\n"
	got, err := replaceBlock(doc, "a", "A2")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "<!-- BEGIN:b -->\nB\n<!-- END:b -->") {
		t.Errorf("replaceBlock() disturbed the sibling block: %q", got)
	}
}

func TestReplaceBlockRejectsBadMarkers(t *testing.T) {
	cases := map[string]string{
		"no markers":    "plain document",
		"only begin":    "<!-- BEGIN:cli -->\nbody\n",
		"only end":      "body\n<!-- END:cli -->\n",
		"reversed":      "<!-- END:cli -->\nbody\n<!-- BEGIN:cli -->\n",
		"wrong section": "<!-- BEGIN:other -->\n<!-- END:other -->",
	}
	for name, doc := range cases {
		if _, err := replaceBlock(doc, "cli", "x"); err == nil {
			t.Errorf("%s: replaceBlock() = nil error, want a rejection", name)
		}
	}
}

// ─── cobra helpers ───────────────────────────────────────────────────────

func newTree() *cobra.Command {
	root := &cobra.Command{Use: "srv"}
	root.AddCommand(
		&cobra.Command{Use: "zebra", GroupID: "g1", Short: "Z"},
		&cobra.Command{Use: "alpha PATH", GroupID: "g1", Short: "A"},
		&cobra.Command{Use: "hidden", GroupID: "g1", Hidden: true},
		&cobra.Command{Use: "other", GroupID: "g2"},
	)
	return root
}

func TestVisibleChildrenSortsAndFilters(t *testing.T) {
	root := newTree()
	root.AddCommand(&cobra.Command{Use: "completion"}, &cobra.Command{Use: "help"})

	var names []string
	for _, c := range visibleChildren(root) {
		names = append(names, c.Name())
	}
	want := []string{"alpha", "other", "zebra"}
	if len(names) != len(want) {
		t.Fatalf("visibleChildren() = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("visibleChildren()[%d] = %q, want %q (sorted, no hidden/help/completion)", i, names[i], want[i])
		}
	}
}

func TestCommandsInGroup(t *testing.T) {
	root := newTree()
	g1 := commandsInGroup(root, "g1")
	if len(g1) != 2 {
		t.Fatalf("commandsInGroup(g1) returned %d commands, want 2", len(g1))
	}
	if got := commandsInGroup(root, "nope"); got != nil {
		t.Errorf("commandsInGroup(unknown) = %v, want nil", got)
	}
}

// A leaf keeps its Use line (which carries arg hints); a parent collapses to
// an escaped pipe list, because an unescaped | would split the markdown cell.
func TestCommandInvocation(t *testing.T) {
	leaf := &cobra.Command{Use: "add PATH"}
	if got := commandInvocation(leaf); got != "add PATH" {
		t.Errorf("commandInvocation(leaf) = %q, want %q", got, "add PATH")
	}

	parent := &cobra.Command{Use: "site"}
	parent.AddCommand(&cobra.Command{Use: "remove"}, &cobra.Command{Use: "add"})
	got := commandInvocation(parent)
	if got != `site <add\|remove>` {
		t.Errorf("commandInvocation(parent) = %q, want %q", got, `site <add\|remove>`)
	}
	if strings.Contains(strings.ReplaceAll(got, `\|`, ""), "|") {
		t.Errorf("commandInvocation() left an unescaped pipe: %q", got)
	}
}

// ─── cell formatting ─────────────────────────────────────────────────────

func TestOneLine(t *testing.T) {
	cases := map[string]string{
		"a  b":            "a b",
		"a\nb\tc":         "a b c",
		"  padded  ":      "padded",
		"pipe | here":     `pipe \| here`,
		"":                "",
		"multi\n\n\nline": "multi line",
	}
	for in, want := range cases {
		if got := oneLine(in); got != want {
			t.Errorf("oneLine(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFirstSentence(t *testing.T) {
	cases := map[string]string{
		"One. Two. Three.":         "One.",
		"No trailing period":       "No trailing period",
		"  Leading space. Rest.":   "Leading space.",
		"Ends with period.":        "Ends with period.",
		"Has a | pipe. And more.":  `Has a \| pipe.`,
		"":                         "",
		"Abbrev e.g. still splits": "Abbrev e.g.",
	}
	for in, want := range cases {
		if got := firstSentence(in); got != want {
			t.Errorf("firstSentence(%q) = %q, want %q", in, got, want)
		}
	}
}

// A leading ". " must not produce an empty cell — the i > 0 guard exists for it.
func TestFirstSentenceIgnoresLeadingSeparator(t *testing.T) {
	if got := firstSentence(". starts oddly"); got == "" {
		t.Error("firstSentence() returned empty for a leading separator")
	}
}

// ─── schema rendering ────────────────────────────────────────────────────

func TestSchemaType(t *testing.T) {
	cases := []struct {
		name string
		in   *jsonschema.Schema
		want string
	}{
		{"nil", nil, "—"},
		{"string", &jsonschema.Schema{Type: "string"}, "string"},
		{"boolean", &jsonschema.Schema{Type: "boolean"}, "boolean"},
		{"empty means object", &jsonschema.Schema{Type: ""}, "object"},
		{"typed array", &jsonschema.Schema{Type: "array", Items: &jsonschema.Schema{Type: "string"}}, "array<string>"},
		{"array without items", &jsonschema.Schema{Type: "array"}, "array"},
		{"array with untyped items", &jsonschema.Schema{Type: "array", Items: &jsonschema.Schema{}}, "array"},
	}
	for _, c := range cases {
		if got := schemaType(c.in); got != c.want {
			t.Errorf("%s: schemaType() = %q, want %q", c.name, got, c.want)
		}
	}
}

// ─── generators ──────────────────────────────────────────────────────────
//
// These run against the real command tree and the real config structs, so they
// double as a smoke test that the README generator cannot panic on the shapes
// it actually ships with.

func TestGenCLIProducesTables(t *testing.T) {
	out := genCLI()
	if out == "" {
		t.Fatal("genCLI() returned nothing")
	}
	if !strings.Contains(out, "| Command | Description |") {
		t.Errorf("genCLI() has no table header:\n%s", out)
	}
	if !strings.HasSuffix(out, "\n") || strings.HasSuffix(out, "\n\n") {
		t.Error("genCLI() should end with exactly one newline")
	}
}

func TestGenConfigCoversEveryConfigFile(t *testing.T) {
	out := genConfig()
	for _, want := range []string{
		"metadata.yml", "proxy-<name>.yml", "redirect-<name>.yml", "config.yml",
		"| Field | Type | Required | Description |",
		"container_engine",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("genConfig() missing %q", want)
		}
	}
}

// Every generated cell must be a single line: a stray newline from a doc
// comment would break the markdown table silently.
func TestGeneratedTablesHaveNoBrokenRows(t *testing.T) {
	for name, body := range map[string]string{"cli": genCLI(), "config": genConfig()} {
		for i, line := range strings.Split(body, "\n") {
			if strings.HasPrefix(line, "|") && !strings.HasSuffix(strings.TrimSpace(line), "|") {
				t.Errorf("%s line %d is a truncated table row: %q", name, i+1, line)
			}
		}
	}
}
