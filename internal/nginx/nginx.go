// Package nginx is a typed model for the nginx configs srv generates.
//
// A config is built as a tree of Directive values and marshalled with Render —
// the same way a docker-compose.yml is built as structs and marshalled with
// yaml.Marshal. There are no raw nginx text fragments: every byte of the output
// derives from struct fields. The lowering is a small text renderer: nginx
// directives are uniform, so the whole grammar is "name args;" and
// "name args { children }" with comments above.
//
// Directive is deliberately generic — Name, Args, optional leading Comment, and
// an optional nested Block — because nginx directives are themselves uniform.
// The Dir and Block constructors keep call sites readable.
package nginx

import (
	"strings"
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
// string with a trailing newline. Layout rules: four-space indent per block
// level, one blank line before every block directive (the visual grouping
// nginx configs are conventionally written with), comments above their
// directive, and no trailing whitespace anywhere.
func Render(directives ...Directive) string {
	lines := renderDirectives(directives, 0)
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = strings.TrimRight(l, " \t")
	}
	return strings.TrimRight(strings.TrimLeft(strings.Join(out, "\n"), "\n"), "\n") + "\n"
}

func renderDirectives(ds []Directive, depth int) []string {
	indent := strings.Repeat("    ", depth)
	var out []string
	for _, d := range ds {
		if d.Block != nil {
			// Blank separator before every block — placed before its comments,
			// which is where the previous library-based dumper put it.
			out = append(out, "")
		}
		for _, c := range hashComments(d.Comment) {
			out = append(out, indent+c)
		}
		if d.Block != nil {
			header := d.Name
			if len(d.Args) > 0 {
				header += " " + strings.Join(d.Args, " ")
			}
			out = append(out, indent+header+" {")
			out = append(out, renderDirectives(d.Block, depth+1)...)
			if len(d.Block) == 0 {
				// An empty block still renders as a meaningful construct; keep
				// a blank line inside so the braces don't collapse.
				out = append(out, "")
			}
			out = append(out, indent+"}")
			continue
		}
		stmt := d.Name
		if len(d.Args) > 0 {
			stmt += " " + strings.Join(d.Args, " ")
		}
		out = append(out, indent+stmt+";")
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
