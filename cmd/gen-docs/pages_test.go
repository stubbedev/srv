package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/invopop/jsonschema"
)

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

func TestConfigReferenceCoversEveryConfigFile(t *testing.T) {
	out := renderConfigReference()
	for _, want := range []string{
		"# Config file reference",
		"metadata.yml", "proxy-<name>.yml", "redirect-<name>.yml", "config.yml",
		"| Field | Type | Required | Description |",
		"container_engine",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("renderConfigReference() missing %q", want)
		}
	}
}

func TestMCPToolsListsEveryTier(t *testing.T) {
	out, err := renderMCPTools()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# MCP tools", "| core | `srv_activate` |", "| read |", "| write |"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderMCPTools() missing %q", want)
		}
	}
}

// Every generated cell must be a single line: a stray newline from a doc
// comment would break the markdown table silently.
func TestGeneratedTablesHaveNoBrokenRows(t *testing.T) {
	tools, err := renderMCPTools()
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"mcp-tools": tools, "config-reference": renderConfigReference()} {
		for i, line := range strings.Split(body, "\n") {
			if strings.HasPrefix(line, "|") && !strings.HasSuffix(strings.TrimSpace(line), "|") {
				t.Errorf("%s line %d is a truncated table row: %q", name, i+1, line)
			}
		}
	}
}

func TestWriteAtomicReplacesAndLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "page.md")
	for _, body := range []string{"first\n", "second\n"} {
		if err := writeAtomic(path, body); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "second\n" {
		t.Errorf("page = %q, want %q", got, "second\n")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("dir holds %d entries, want only page.md", len(entries))
	}
}
