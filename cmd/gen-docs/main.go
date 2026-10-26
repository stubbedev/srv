// Command gen-docs regenerates the reference pages of the docs site from
// source: docs/cli.md from the cobra command tree, docs/mcp-tools.md from the
// live MCP server, and docs/config-reference.md from the jsonschema-tagged
// config structs. CI re-runs this and fails when the output drifts so the
// docs cannot fall behind the binary.
//
// Run via `just sync-docs` or `go run ./cmd/gen-docs [dir]`.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	srvcmd "github.com/stubbedev/srv/cmd"
)

const defaultDir = "docs"

func main() {
	dir := defaultDir
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}

	tools, err := renderMCPTools()
	if err != nil {
		fail(fmt.Errorf("mcp tools: %w", err))
	}
	pages := []struct{ name, body string }{
		{"cli.md", renderCLI(srvcmd.RootCmd)},
		{"mcp-tools.md", tools},
		{"config-reference.md", renderConfigReference()},
	}
	for _, p := range pages {
		path := filepath.Join(dir, p.name)
		if err := writeAtomic(path, p.body); err != nil {
			fail(err)
		}
		fmt.Println("wrote", path)
	}
}

// writeAtomic replaces path with body via a temp file in the same directory,
// so an interrupted run never leaves a half-written page behind.
func writeAtomic(path, body string) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", path, err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.WriteString(body); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", tmp.Name(), err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod %s: %w", tmp.Name(), err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmp.Name(), err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("rename %s to %s: %w", tmp.Name(), path, err)
	}
	return nil
}

func renderCLI(root *cobra.Command) string {
	if root.Use == "" {
		// RootCmd has no Use set; fall back to the binary name.
		root.Use = "srv"
	}
	var b strings.Builder
	writeHeader(&b)
	writeGlobalFlags(&b, root)
	writeIndex(&b, root)
	writeCommand(&b, root, nil)
	return b.String()
}

func writeHeader(b *strings.Builder) {
	b.WriteString("# CLI reference\n\n")
	writeGeneratedNote(b, "the `srv` command tree")
}

// writeGeneratedNote marks a page as generated, naming its source, so nobody
// hand-edits a file the next sync overwrites.
func writeGeneratedNote(b *strings.Builder, source string) {
	fmt.Fprintf(b, "> Auto-generated from %s by `go run ./cmd/gen-docs`.\n", source)
	b.WriteString("> Run `just sync-docs` after changing it to refresh.\n\n")
}

func writeIndex(b *strings.Builder, root *cobra.Command) {
	b.WriteString("## Index\n\n")
	for _, c := range visibleChildren(root) {
		fmt.Fprintf(b, "- [`srv %s`](#srv-%s) — %s\n", c.Name(), slugify(c.Name()), oneLineSummary(c))
		for _, sub := range visibleChildren(c) {
			fmt.Fprintf(b, "  - [`srv %s %s`](#srv-%s-%s) — %s\n",
				c.Name(), sub.Name(),
				slugify(c.Name()), slugify(sub.Name()),
				oneLineSummary(sub))
		}
	}
	b.WriteString("\n")
}

// writeCommand emits the section for `c` and recurses into its visible
// children. `parents` is the chain of command names from root (excluding
// `c`'s own name).
func writeCommand(b *strings.Builder, c *cobra.Command, parents []string) {
	// Skip the root command's own section — the index plus per-subcommand
	// sections cover everything useful. Recurse into children directly.
	if c.HasParent() {
		path := strings.Join(append(append([]string{}, parents...), c.Name()), " ")
		fmt.Fprintf(b, "## `srv %s`\n\n", path)
		if len(c.Aliases) > 0 {
			fmt.Fprintf(b, "Aliases: `%s`\n\n", strings.Join(c.Aliases, "`, `"))
		}
		if s := strings.TrimSpace(c.Short); s != "" {
			fmt.Fprintf(b, "%s\n\n", s)
		}
		if l := strings.TrimSpace(c.Long); l != "" && l != strings.TrimSpace(c.Short) {
			fmt.Fprintf(b, "```\n%s\n```\n\n", l)
		}
		if u := strings.TrimSpace(c.UseLine()); u != "" {
			fmt.Fprintf(b, "Usage:\n\n```\n%s\n```\n\n", fullUseLine(c, parents))
		}
		writeFlags(b, c)
		writeChildrenList(b, c)
	}

	for _, child := range visibleChildren(c) {
		nextParents := parents
		if c.HasParent() {
			nextParents = append(append([]string{}, parents...), c.Name())
		}
		writeCommand(b, child, nextParents)
	}
}

func writeFlags(b *strings.Builder, c *cobra.Command) {
	// Persistent flags inherited from ancestors are documented once in the
	// "Global flags" section at the top of the page; per-command tables stay
	// focused on flags specific to the command.
	local := visibleFlags(c.LocalFlags())
	if len(local) == 0 {
		return
	}
	b.WriteString("| Flag | Default | Description |\n|---|---|---|\n")
	for _, f := range local {
		writeFlagRow(b, f)
	}
	b.WriteString("\n")
}

// writeGlobalFlags emits the root persistent flag set once at the top so
// readers don't see --verbose / --quiet / --format repeated on every section.
func writeGlobalFlags(b *strings.Builder, root *cobra.Command) {
	flags := visibleFlags(root.PersistentFlags())
	if len(flags) == 0 {
		return
	}
	b.WriteString("## Global flags\n\n")
	b.WriteString("Available on every command:\n\n")
	b.WriteString("| Flag | Default | Description |\n|---|---|---|\n")
	for _, f := range flags {
		writeFlagRow(b, f)
	}
	b.WriteString("\n")
}

func writeFlagRow(b *strings.Builder, f *pflag.Flag) {
	name := "`--" + f.Name + "`"
	if f.Shorthand != "" {
		name += ", `-" + f.Shorthand + "`"
	}
	def := f.DefValue
	if def == "" {
		def = "—"
	} else {
		def = "`" + def + "`"
	}
	usage := escapePipes(strings.ReplaceAll(f.Usage, "\n", " "))
	fmt.Fprintf(b, "| %s | %s | %s |\n", name, def, usage)
}

func writeChildrenList(b *strings.Builder, c *cobra.Command) {
	kids := visibleChildren(c)
	if len(kids) == 0 {
		return
	}
	b.WriteString("Subcommands:\n\n")
	for _, k := range kids {
		fmt.Fprintf(b, "- `srv %s %s` — %s\n", c.Name(), k.Name(), oneLineSummary(k))
	}
	b.WriteString("\n")
}

// fullUseLine reconstructs the use line with the full command path so
// readers see `srv proxy add [flags]` rather than the bare `add [flags]`
// that c.UseLine() emits for nested commands.
func fullUseLine(c *cobra.Command, parents []string) string {
	use := c.Use
	// c.Use may already include arg hints (e.g. "add PATH"); keep it intact.
	parts := append([]string{"srv"}, parents...)
	parts = append(parts, use)
	line := strings.Join(parts, " ")
	// Cobra appends [flags] when local flags exist.
	if c.HasAvailableLocalFlags() && !strings.Contains(line, "[flags]") {
		line += " [flags]"
	}
	return line
}

func visibleChildren(c *cobra.Command) []*cobra.Command {
	var out []*cobra.Command
	for _, k := range c.Commands() {
		if k.Hidden || k.Name() == "help" || k.Name() == "completion" {
			continue
		}
		out = append(out, k)
	}
	slices.SortStableFunc(out, func(a, b *cobra.Command) int { return strings.Compare(a.Name(), b.Name()) })
	return out
}

func visibleFlags(fs *pflag.FlagSet) []*pflag.Flag {
	var out []*pflag.Flag
	fs.VisitAll(func(f *pflag.Flag) {
		if f.Hidden {
			return
		}
		out = append(out, f)
	})
	slices.SortStableFunc(out, func(a, b *pflag.Flag) int { return strings.Compare(a.Name, b.Name) })
	return out
}

func oneLineSummary(c *cobra.Command) string {
	s := strings.TrimSpace(c.Short)
	if s == "" {
		// Fall back to first line of Long.
		if l := strings.TrimSpace(c.Long); l != "" {
			if i := strings.IndexByte(l, '\n'); i > 0 {
				s = strings.TrimSpace(l[:i])
			} else {
				s = l
			}
		}
	}
	return escapePipes(s)
}

func slugify(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ', r == '-', r == '_':
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func escapePipes(s string) string {
	return strings.ReplaceAll(s, "|", `\|`)
}

// oneLine collapses whitespace and escapes table-breaking pipes so a value
// fits in one Markdown table cell.
func oneLine(s string) string {
	return escapePipes(strings.Join(strings.Fields(s), " "))
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "gen-docs:", err)
	os.Exit(1)
}
