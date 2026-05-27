// Command gen-docs walks the srv cobra command tree declared by
// cmd.RootCmd and emits a single Markdown CLI reference to docs/cli.md.
// CI re-runs this and fails when the output drifts so the docs cannot
// fall behind the binary.
//
// Run via `just sync-docs` or `go run ./cmd/gen-docs`.
package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	srvcmd "github.com/stubbedev/srv/cmd"
)

const defaultOut = "docs/cli.md"

func main() {
	out := defaultOut
	if len(os.Args) > 1 {
		out = os.Args[1]
	}

	root := srvcmd.RootCmd
	if root.Use == "" {
		// RootCmd has no Use set; fall back to the binary name.
		root.Use = "srv"
	}

	var b strings.Builder
	writeHeader(&b)
	writeGlobalFlags(&b, root)
	writeIndex(&b, root)
	writeCommand(&b, root, nil)

	if err := os.WriteFile(out, []byte(b.String()), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "gen-docs:", err)
		os.Exit(1)
	}
	fmt.Println("wrote", out)
}

func writeHeader(b *strings.Builder) {
	b.WriteString("# srv CLI reference\n\n")
	b.WriteString("[← back to README](../README.md)\n\n")
	b.WriteString("> Auto-generated from the `srv` command tree by `go run ./cmd/gen-docs`.\n")
	b.WriteString("> Run `just sync-docs` after touching any subcommand to refresh.\n\n")
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
		if c.Aliases != nil && len(c.Aliases) > 0 {
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
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
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
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
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
