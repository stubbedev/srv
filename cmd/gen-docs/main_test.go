package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	srvcmd "github.com/stubbedev/srv/cmd"
)

func TestSlugify(t *testing.T) {
	cases := []struct {
		in, out string
	}{
		{"add", "add"},
		{"Proxy Add", "proxy-add"},
		{"alias_list", "alias-list"},
		{"add PATH", "add-path"},
		{"--Some$Flag!", "someflag"},
		{"", ""},
		{"---", ""},
		{" trim ", "trim"},
	}
	for _, c := range cases {
		if got := slugify(c.in); got != c.out {
			t.Errorf("slugify(%q) = %q, want %q", c.in, got, c.out)
		}
	}
}

func TestEscapePipes(t *testing.T) {
	cases := []struct{ in, out string }{
		{"a|b", `a\|b`},
		{"no pipe", "no pipe"},
		{"||", `\|\|`},
		{"", ""},
	}
	for _, c := range cases {
		if got := escapePipes(c.in); got != c.out {
			t.Errorf("escapePipes(%q) = %q, want %q", c.in, got, c.out)
		}
	}
}

func TestOneLineSummary(t *testing.T) {
	t.Run("from-short", func(t *testing.T) {
		c := &cobra.Command{Short: "Add a thing"}
		if got := oneLineSummary(c); got != "Add a thing" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("fall-back-to-long-first-line", func(t *testing.T) {
		c := &cobra.Command{Long: "First line\nSecond line\nThird"}
		if got := oneLineSummary(c); got != "First line" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("escapes-pipes", func(t *testing.T) {
		c := &cobra.Command{Short: "uses |pipe| chars"}
		if got := oneLineSummary(c); got != `uses \|pipe\| chars` {
			t.Errorf("got %q", got)
		}
	})
}

func TestVisibleChildren(t *testing.T) {
	root := &cobra.Command{Use: "root"}
	root.AddCommand(&cobra.Command{Use: "beta"})
	root.AddCommand(&cobra.Command{Use: "alpha"})
	root.AddCommand(&cobra.Command{Use: "hidden", Hidden: true})
	root.AddCommand(&cobra.Command{Use: "help"})
	root.AddCommand(&cobra.Command{Use: "completion"})

	got := visibleChildren(root)
	if len(got) != 2 {
		t.Fatalf("expected 2 visible children, got %d (%v)", len(got), got)
	}
	if got[0].Name() != "alpha" || got[1].Name() != "beta" {
		t.Errorf("expected [alpha, beta], got [%s, %s]", got[0].Name(), got[1].Name())
	}
}

func TestFullUseLine(t *testing.T) {
	t.Run("top-level-no-args", func(t *testing.T) {
		c := &cobra.Command{Use: "list"}
		if got := fullUseLine(c, nil); got != "srv list" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("with-flags", func(t *testing.T) {
		c := &cobra.Command{Use: "list"}
		c.Flags().Bool("verbose", false, "")
		if got := fullUseLine(c, nil); got != "srv list [flags]" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("nested", func(t *testing.T) {
		c := &cobra.Command{Use: "add PATH"}
		c.Flags().Bool("force", false, "")
		got := fullUseLine(c, []string{"proxy"})
		if got != "srv proxy add PATH [flags]" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("already-has-flags-token", func(t *testing.T) {
		c := &cobra.Command{Use: "add [flags] NAME"}
		c.Flags().Bool("x", false, "")
		got := fullUseLine(c, nil)
		// Must not double-append [flags].
		if strings.Count(got, "[flags]") != 1 {
			t.Errorf("got %q (expected exactly one [flags])", got)
		}
	})
}

// ─── document writers ────────────────────────────────────────────────────
//
// docs/cli.md is regenerated in CI and the job fails on drift, so these
// writers have to be both correct and stable. They are pure string builders,
// so a hand-built cobra tree exercises them without touching the real CLI.

func docTree() *cobra.Command {
	root := &cobra.Command{Use: "srv"}
	root.PersistentFlags().BoolP("verbose", "v", false, "Enable verbose output")
	root.PersistentFlags().String("format", "table", "Output format")
	hidden := root.PersistentFlags().Bool("secret", false, "hidden flag")
	_ = hidden
	_ = root.PersistentFlags().MarkHidden("secret")

	proxy := &cobra.Command{Use: "proxy", Short: "Manage proxy routes", Aliases: []string{"px"}}
	add := &cobra.Command{Use: "add DOMAIN", Short: "Add a proxy", Long: "Add a proxy route.\nSecond line."}
	add.Flags().String("port", "", "Port to proxy | with a pipe")
	add.Flags().BoolP("wildcard", "w", false, "Match subdomains")
	proxy.AddCommand(add, &cobra.Command{Use: "remove NAME", Short: "Remove a proxy"})

	root.AddCommand(proxy, &cobra.Command{Use: "version", Short: "Show version info"})
	root.AddCommand(&cobra.Command{Use: "internal-only", Hidden: true})
	return root
}

func TestWriteHeader(t *testing.T) {
	var b strings.Builder
	writeHeader(&b)
	for _, want := range []string{"# srv CLI reference", "back to README", "gen-docs"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("writeHeader() missing %q", want)
		}
	}
}

func TestWriteIndexListsCommandsAndSubcommands(t *testing.T) {
	var b strings.Builder
	writeIndex(&b, docTree())
	out := b.String()
	for _, want := range []string{
		"## Index",
		"[`srv proxy`](#srv-proxy)",
		"[`srv proxy add`](#srv-proxy-add)",
		"[`srv version`](#srv-version)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("writeIndex() missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "internal-only") {
		t.Error("writeIndex() listed a hidden command")
	}
}

func TestWriteGlobalFlagsSkipsHidden(t *testing.T) {
	var b strings.Builder
	writeGlobalFlags(&b, docTree())
	out := b.String()
	if !strings.Contains(out, "## Global flags") {
		t.Errorf("missing the Global flags heading:\n%s", out)
	}
	if !strings.Contains(out, "`--verbose`, `-v`") {
		t.Errorf("shorthand not rendered:\n%s", out)
	}
	if strings.Contains(out, "secret") {
		t.Error("writeGlobalFlags() rendered a hidden flag")
	}
}

func TestWriteGlobalFlagsEmptyWhenNoFlags(t *testing.T) {
	var b strings.Builder
	writeGlobalFlags(&b, &cobra.Command{Use: "bare"})
	if b.String() != "" {
		t.Errorf("writeGlobalFlags() = %q, want nothing for a flagless root", b.String())
	}
}

// A default of "" must render as an em dash, not an empty cell, and a pipe in
// the usage text must be escaped or it splits the markdown row.
func TestWriteFlagRow(t *testing.T) {
	fs := pflag.NewFlagSet("t", pflag.ContinueOnError)
	fs.String("empty", "", "no default | piped")
	fs.String("filled", "table", "has a default")

	var b strings.Builder
	writeFlagRow(&b, fs.Lookup("empty"))
	writeFlagRow(&b, fs.Lookup("filled"))
	out := b.String()

	if !strings.Contains(out, "| — |") {
		t.Errorf("empty default should render as an em dash:\n%s", out)
	}
	if !strings.Contains(out, "`table`") {
		t.Errorf("non-empty default should be backticked:\n%s", out)
	}
	if strings.Contains(out, "no default | piped") {
		t.Errorf("pipe in usage was not escaped:\n%s", out)
	}
}

func TestWriteFlagsSkipsCommandsWithNoLocalFlags(t *testing.T) {
	var b strings.Builder
	writeFlags(&b, &cobra.Command{Use: "bare"})
	if b.String() != "" {
		t.Errorf("writeFlags() = %q, want nothing", b.String())
	}
}

func TestWriteChildrenList(t *testing.T) {
	root := docTree()
	proxy := root.Commands()[0]
	for _, c := range root.Commands() {
		if c.Name() == "proxy" {
			proxy = c
		}
	}
	var b strings.Builder
	writeChildrenList(&b, proxy)
	out := b.String()
	if !strings.Contains(out, "Subcommands:") {
		t.Errorf("missing the Subcommands heading:\n%s", out)
	}
	if !strings.Contains(out, "`srv proxy add`") || !strings.Contains(out, "`srv proxy remove`") {
		t.Errorf("children not listed:\n%s", out)
	}

	var empty strings.Builder
	writeChildrenList(&empty, &cobra.Command{Use: "leaf"})
	if empty.String() != "" {
		t.Errorf("writeChildrenList() = %q for a leaf, want nothing", empty.String())
	}
}

// writeCommand skips the root's own section and renders the full path for
// nested commands — `srv proxy add`, not the bare `add` cobra reports.
func TestWriteCommandRendersFullPathsAndSkipsRoot(t *testing.T) {
	var b strings.Builder
	writeCommand(&b, docTree(), nil)
	out := b.String()

	if strings.Contains(out, "## `srv srv`") {
		t.Error("writeCommand() emitted a section for the root command")
	}
	for _, want := range []string{
		"## `srv proxy`",
		"## `srv proxy add`",
		"Aliases: `px`",
		"srv proxy add DOMAIN [flags]",
		"Add a proxy route.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("writeCommand() missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "internal-only") {
		t.Error("writeCommand() documented a hidden command")
	}
}

func TestWriteCommandIsDeterministic(t *testing.T) {
	var a, b strings.Builder
	writeCommand(&a, docTree(), nil)
	writeCommand(&b, docTree(), nil)
	if a.String() != b.String() {
		t.Error("writeCommand() output differs between runs — docs/cli.md would drift in CI")
	}
}

// A Use line that already carries [flags] must not gain a second one.
func TestFullUseLineDoesNotDuplicateFlagsSuffix(t *testing.T) {
	c := &cobra.Command{Use: "add PATH [flags]"}
	c.Flags().String("port", "", "port")
	got := fullUseLine(c, []string{"site"})
	if strings.Count(got, "[flags]") != 1 {
		t.Errorf("fullUseLine() = %q, want exactly one [flags]", got)
	}
}

func TestVisibleFlagsSortedAndFiltered(t *testing.T) {
	fs := pflag.NewFlagSet("t", pflag.ContinueOnError)
	fs.String("zebra", "", "")
	fs.String("alpha", "", "")
	fs.String("hidden", "", "")
	_ = fs.MarkHidden("hidden")

	got := visibleFlags(fs)
	if len(got) != 2 {
		t.Fatalf("visibleFlags() returned %d flags, want 2", len(got))
	}
	if got[0].Name != "alpha" || got[1].Name != "zebra" {
		t.Errorf("visibleFlags() not sorted: %s, %s", got[0].Name, got[1].Name)
	}
}

func TestVisibleChildrenFiltersHelpAndCompletion(t *testing.T) {
	root := &cobra.Command{Use: "srv"}
	root.AddCommand(
		&cobra.Command{Use: "help"},
		&cobra.Command{Use: "completion"},
		&cobra.Command{Use: "zebra"},
		&cobra.Command{Use: "alpha"},
	)
	got := visibleChildren(root)
	if len(got) != 2 || got[0].Name() != "alpha" || got[1].Name() != "zebra" {
		names := make([]string, len(got))
		for i, c := range got {
			names[i] = c.Name()
		}
		t.Errorf("visibleChildren() = %v, want [alpha zebra]", names)
	}
}

// The real tree is the one that ships; rendering it must not panic and must
// produce every section the index promises.
func TestFullDocumentAgainstTheRealCommandTree(t *testing.T) {
	root := srvcmd.RootCmd
	if root.Use == "" {
		root.Use = "srv"
	}
	var b strings.Builder
	writeHeader(&b)
	writeGlobalFlags(&b, root)
	writeIndex(&b, root)
	writeCommand(&b, root, nil)
	out := b.String()

	for _, c := range visibleChildren(root) {
		if !strings.Contains(out, "## `srv "+c.Name()+"`") {
			t.Errorf("no section rendered for `srv %s`", c.Name())
		}
	}
	for i, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "|") && !strings.HasSuffix(strings.TrimSpace(line), "|") {
			t.Errorf("line %d is a truncated table row: %q", i+1, line)
		}
	}
}
