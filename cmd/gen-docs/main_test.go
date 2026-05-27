package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
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
