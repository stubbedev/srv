// Command gen-readme regenerates the marker-delimited sections of README.md
// that would otherwise need manual upkeep: the CLI command surface (from the
// cobra tree), the MCP tool table (from the live MCP server), and the config
// file field reference (from the jsonschema-tagged Go structs).
//
// Each generated region is bounded by HTML comment markers:
//
//	<!-- BEGIN:cli -->    … <!-- END:cli -->
//	<!-- BEGIN:mcp -->    … <!-- END:mcp -->
//	<!-- BEGIN:config --> … <!-- END:config -->
//
// gen-readme rewrites only the text between each pair; everything else in the
// README is hand-written and left untouched. CI re-runs this and fails on drift
// so the documented surfaces can never fall behind the binary.
//
// Run via `just sync-readme` or `go run ./cmd/gen-readme`.
package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/invopop/jsonschema"
	"github.com/spf13/cobra"

	srvcmd "github.com/stubbedev/srv/cmd"
	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/mcp"
	"github.com/stubbedev/srv/internal/proxy"
	"github.com/stubbedev/srv/internal/redirect"
	"github.com/stubbedev/srv/internal/site"
)

const defaultReadme = "README.md"

func main() {
	path := defaultReadme
	if len(os.Args) > 1 {
		path = os.Args[1]
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		fail(err)
	}
	doc := string(raw)

	cli, err := genCLI()
	if err != nil {
		fail(fmt.Errorf("cli: %w", err))
	}
	tools, err := genMCP()
	if err != nil {
		fail(fmt.Errorf("mcp: %w", err))
	}
	cfg, err := genConfig()
	if err != nil {
		fail(fmt.Errorf("config: %w", err))
	}

	for _, blk := range []struct{ name, body string }{
		{"cli", cli},
		{"mcp", tools},
		{"config", cfg},
	} {
		doc, err = replaceBlock(doc, blk.name, blk.body)
		if err != nil {
			fail(err)
		}
	}

	if string(raw) == doc {
		fmt.Println("gen-readme: already in sync")
		return
	}
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		fail(err)
	}
	fmt.Println("wrote", path)
}

// replaceBlock swaps the text between <!-- BEGIN:name --> and <!-- END:name -->
// for body, preserving the marker lines. It is an error for either marker to be
// missing — that means the README is structurally out of date with this tool.
func replaceBlock(doc, name, body string) (string, error) {
	begin := fmt.Sprintf("<!-- BEGIN:%s -->", name)
	end := fmt.Sprintf("<!-- END:%s -->", name)
	bi := strings.Index(doc, begin)
	ei := strings.Index(doc, end)
	if bi < 0 || ei < 0 || ei < bi {
		return "", fmt.Errorf("missing or malformed markers for %q (need %s … %s)", name, begin, end)
	}
	var b strings.Builder
	b.WriteString(doc[:bi+len(begin)])
	b.WriteString("\n")
	b.WriteString(strings.TrimRight(body, "\n"))
	b.WriteString("\n")
	b.WriteString(doc[ei:])
	return b.String(), nil
}

// ─── CLI surface ─────────────────────────────────────────────────────────

func genCLI() (string, error) {
	root := srvcmd.RootCmd
	var b strings.Builder
	for _, g := range root.Groups() {
		title := strings.TrimSuffix(strings.TrimSpace(g.Title), ":")
		cmds := commandsInGroup(root, g.ID)
		if len(cmds) == 0 {
			continue
		}
		fmt.Fprintf(&b, "### %s\n\n", title)
		b.WriteString("| Command | Description |\n|---------|-------------|\n")
		for _, c := range cmds {
			fmt.Fprintf(&b, "| `srv %s` | %s |\n", commandInvocation(c), oneLine(c.Short))
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n") + "\n", nil
}

// commandInvocation renders a compact invocation for a top-level command. A
// command with subcommands collapses to `name <sub1|sub2|…>`; a leaf keeps its
// cobra Use line (which carries arg hints like `add PATH`). Pipes inside the
// backticked cell are escaped for GitHub-flavored Markdown tables.
func commandInvocation(c *cobra.Command) string {
	if subs := visibleChildren(c); len(subs) > 0 {
		names := make([]string, len(subs))
		for i, s := range subs {
			names[i] = s.Name()
		}
		return fmt.Sprintf("%s <%s>", c.Name(), strings.Join(names, `\|`))
	}
	return strings.TrimSpace(c.Use)
}

func commandsInGroup(root *cobra.Command, groupID string) []*cobra.Command {
	var out []*cobra.Command
	for _, c := range visibleChildren(root) {
		if c.GroupID == groupID {
			out = append(out, c)
		}
	}
	return out
}

// ─── MCP tools ─────────────────────────────────────────────────────────

func genMCP() (string, error) {
	docs, err := mcp.ToolManifest(context.Background())
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("Available tools, by tier:\n\n")
	b.WriteString("| Tier | Tool | Description |\n|---|---|---|\n")
	for _, d := range docs {
		fmt.Fprintf(&b, "| %s | `%s` | %s |\n", d.Tier, d.Name, firstSentence(d.Description))
	}
	return b.String(), nil
}

// ─── config file reference ───────────────────────────────────────────────

func genConfig() (string, error) {
	type target struct {
		heading string
		file    string
		value   any
	}
	targets := []target{
		{"Site — `metadata.yml`", "sites/<name>/metadata.yml", &site.SiteMetadata{}},
		{"Proxy — `proxy-<name>.yml`", "proxies/proxy-<name>.yml", &proxy.Metadata{}},
		{"DNS-only redirect", "traefik/conf.d/redirect-<name>.yml", &redirect.DNSOnlyConfig{}},
		{"User config — `config.yml`", "config.yml", &config.UserConfig{}},
	}

	var b strings.Builder
	for _, t := range targets {
		fmt.Fprintf(&b, "#### %s\n\n", t.heading)
		fmt.Fprintf(&b, "_Path: `~/.config/srv/%s`_\n\n", t.file)
		b.WriteString("| Field | Type | Required | Description |\n|---|---|---|---|\n")
		schema := reflectStruct(t.value)
		required := map[string]bool{}
		for _, r := range schema.Required {
			required[r] = true
		}
		if schema.Properties != nil {
			for pair := schema.Properties.Oldest(); pair != nil; pair = pair.Next() {
				req := "no"
				if required[pair.Key] {
					req = "yes"
				}
				fmt.Fprintf(&b, "| `%s` | %s | %s | %s |\n",
					pair.Key, schemaType(pair.Value), req, oneLine(pair.Value.Description))
			}
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n") + "\n", nil
}

// reflectStruct mirrors gen-schema's reflector so the README field reference
// carries the same doc-comment descriptions as the published JSON Schemas.
// DoNotReference inlines nested types so every field has a concrete type
// rather than a $ref the table can't render.
func reflectStruct(v any) *jsonschema.Schema {
	r := &jsonschema.Reflector{
		FieldNameTag:               "yaml",
		RequiredFromJSONSchemaTags: true,
		ExpandedStruct:             true,
		DoNotReference:             true,
	}
	if err := r.AddGoComments("github.com/stubbedev/srv", "./", jsonschema.WithFullComment()); err != nil {
		fmt.Fprintf(os.Stderr, "gen-readme: warning: doc-comment harvest failed: %v\n", err)
	}
	return r.Reflect(v)
}

func schemaType(s *jsonschema.Schema) string {
	if s == nil {
		return "—"
	}
	switch s.Type {
	case "array":
		if s.Items != nil && s.Items.Type != "" {
			return "array<" + s.Items.Type + ">"
		}
		return "array"
	case "":
		return "object"
	default:
		return s.Type
	}
}

// ─── shared helpers ────────────────────────────────────────────────────

func visibleChildren(c *cobra.Command) []*cobra.Command {
	var out []*cobra.Command
	for _, k := range c.Commands() {
		if k.Hidden || k.Name() == "help" || k.Name() == "completion" {
			continue
		}
		out = append(out, k)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

// oneLine collapses whitespace and escapes table-breaking pipes.
func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	return strings.ReplaceAll(s, "|", `\|`)
}

// firstSentence trims a (often agent-verbose) tool description to its first
// sentence so the README table stays scannable, then sanitizes it for a cell.
func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, ". "); i > 0 {
		s = s[:i+1]
	}
	return oneLine(s)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "gen-readme:", err)
	os.Exit(1)
}
