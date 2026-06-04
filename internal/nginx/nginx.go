// Package nginx is a typed model for the nginx configs srv generates.
//
// A config is built as a tree of Directive values and marshalled with Render —
// the same way a docker-compose.yml is built as structs and marshalled with
// yaml.Marshal. There are no raw nginx text fragments: every byte of the output
// derives from struct fields. The lowering targets a gonginx AST so the actual
// formatting, indentation, and escaping are handled by a maintained library
// rather than hand-written string building.
//
// Directive is deliberately generic — Name, Args, optional leading Comment, and
// an optional nested Block — because nginx directives are themselves uniform.
// The Dir and Block constructors keep call sites readable.
package nginx

import (
	"strings"

	"github.com/tufanbarisyildirim/gonginx/config"
	"github.com/tufanbarisyildirim/gonginx/dumper"
)

// Directive is one nginx directive. With Block == nil it renders as a simple
// `Name Args;` statement; with Block non-nil it renders as a
// `Name Args { ... }` block (even when the block is empty).
type Directive struct {
	Name string
	Args []string
	// Comment renders as leading comment line(s) above the directive. A "#"
	// prefix is added where missing; an empty string yields a blank separator
	// line.
	Comment []string
	Block   []Directive
}

// Dir builds a simple (non-block) directive, e.g.
// Dir("proxy_set_header", "Host", "$host") → `proxy_set_header Host $host;`.
func Dir(name string, args ...string) Directive {
	return Directive{Name: name, Args: args}
}

// Block builds a block directive, e.g.
// Block("location", []string{"/"}, Dir("try_files", "$uri", "=404")).
// Passing no children still renders an (empty) block.
func Block(name string, args []string, children ...Directive) Directive {
	if children == nil {
		children = []Directive{}
	}
	return Directive{Name: name, Args: args, Block: children}
}

// WithComment returns a copy of the directive carrying the given leading
// comment lines. A leading "" line renders as a blank separator.
func (d Directive) WithComment(lines ...string) Directive {
	d.Comment = lines
	return d
}

// Render marshals a sequence of top-level directives into an nginx config
// string with a trailing newline.
func Render(directives ...Directive) string {
	cfg := &config.Config{Block: &config.Block{Directives: lower(directives)}}
	style := dumper.NewStyle()
	style.SpaceBeforeBlocks = true // blank line before nested blocks
	out := dumper.DumpConfig(cfg, style)
	out = trimLineWhitespace(out)
	return strings.TrimRight(strings.TrimLeft(out, "\n"), "\n") + "\n"
}

func lower(ds []Directive) []config.IDirective {
	out := make([]config.IDirective, 0, len(ds))
	for _, d := range ds {
		gd := &config.Directive{Name: d.Name, Parameters: params(d.Args...)}
		if d.Block != nil {
			gd.Block = &config.Block{Directives: lower(d.Block)}
		}
		if len(d.Comment) > 0 {
			gd.SetComment(hashComments(d.Comment))
		}
		out = append(out, gd)
	}
	return out
}

// trimLineWhitespace strips trailing spaces from every line — gonginx indents
// blank separator lines, leaving cosmetic trailing whitespace we don't want.
func trimLineWhitespace(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	return strings.Join(lines, "\n")
}

func params(vs ...string) []config.Parameter {
	out := make([]config.Parameter, len(vs))
	for i, v := range vs {
		out[i] = config.Parameter{Value: v}
	}
	return out
}

// hashComments turns plain comment text into nginx comment lines: each line is
// prefixed with "# " unless it is already a comment or is empty (a blank line
// used as a visual separator).
func hashComments(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		switch {
		case l == "":
			out[i] = ""
		case strings.HasPrefix(l, "#"):
			out[i] = l
		default:
			out[i] = "# " + l
		}
	}
	return out
}
