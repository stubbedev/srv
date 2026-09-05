package nginx

import (
	"strings"
	"testing"
)

// The whole point of this package is that no output byte comes from a
// hand-written text fragment, so the tests assert on rendered text: if the
// lowering to gonginx or the dumper style changes, these catch it.

func TestRenderSimpleDirective(t *testing.T) {
	got := Render(Dir("proxy_set_header", "Host", "$host"))
	want := "proxy_set_header Host $host;\n"
	if got != want {
		t.Errorf("Render() = %q, want %q", got, want)
	}
}

func TestRenderDirectiveWithoutArgs(t *testing.T) {
	if got := Render(Dir("sendfile")); got != "sendfile;\n" {
		t.Errorf("Render() = %q, want %q", got, "sendfile;\n")
	}
}

func TestRenderBlock(t *testing.T) {
	got := Render(Block("location", []string{"/"},
		Dir("try_files", "$uri", "=404"),
	))
	for _, want := range []string{"location / {", "try_files $uri =404;", "}"} {
		if !strings.Contains(got, want) {
			t.Errorf("Render() = %q, missing %q", got, want)
		}
	}
}

// Block with no children must still render braces — an empty `server {}` is a
// meaningful nginx construct, not an omission.
func TestRenderEmptyBlockStillHasBraces(t *testing.T) {
	got := Render(Block("server", nil))
	if !strings.Contains(got, "server") || !strings.Contains(got, "{") || !strings.Contains(got, "}") {
		t.Errorf("Render() = %q, want an empty block with braces", got)
	}
}

func TestRenderNestedBlocks(t *testing.T) {
	got := Render(Block("http", nil,
		Block("server", nil,
			Dir("listen", "80"),
			Block("location", []string{"/api"}, Dir("proxy_pass", "http://up")),
		),
	))
	for _, want := range []string{"http {", "server {", "listen 80;", "location /api {", "proxy_pass http://up;"} {
		if !strings.Contains(got, want) {
			t.Errorf("Render() missing %q in:\n%s", want, got)
		}
	}
}

func TestWithCommentPrefixesHash(t *testing.T) {
	got := Render(Dir("listen", "80").WithComment("plain text"))
	if !strings.Contains(got, "# plain text") {
		t.Errorf("Render() = %q, want a hash-prefixed comment", got)
	}
}

// An already-hashed line must not gain a second hash, and an empty line stays
// empty so it renders as a visual separator rather than a bare "# ".
func TestWithCommentPreservesExistingHashAndBlankLines(t *testing.T) {
	got := Render(Dir("listen", "80").WithComment("# already", "", "second"))
	if strings.Contains(got, "## already") {
		t.Errorf("Render() double-hashed an existing comment:\n%s", got)
	}
	if !strings.Contains(got, "# already") || !strings.Contains(got, "# second") {
		t.Errorf("Render() = %q, want both comment lines", got)
	}
}

func TestWithCommentDoesNotMutateReceiver(t *testing.T) {
	base := Dir("listen", "80")
	_ = base.WithComment("note")
	if base.Comment != nil {
		t.Errorf("WithComment mutated the receiver: %v", base.Comment)
	}
}

// Render must end with exactly one newline and no leading blank lines, since
// the result is written straight to a config file.
func TestRenderTrimsSurroundingBlankLines(t *testing.T) {
	got := Render(Block("server", nil, Dir("listen", "80")))
	if strings.HasPrefix(got, "\n") {
		t.Errorf("Render() has a leading blank line: %q", got)
	}
	if !strings.HasSuffix(got, "\n") || strings.HasSuffix(got, "\n\n") {
		t.Errorf("Render() = %q, want exactly one trailing newline", got)
	}
}

// gonginx indents blank separator lines; trimLineWhitespace exists to undo
// that, so no rendered line may carry trailing whitespace.
func TestRenderLeavesNoTrailingWhitespace(t *testing.T) {
	got := Render(
		Dir("listen", "80").WithComment("", "a separated comment"),
		Block("server", nil, Dir("root", "/srv")),
	)
	for i, line := range strings.Split(got, "\n") {
		if line != strings.TrimRight(line, " \t") {
			t.Errorf("line %d has trailing whitespace: %q", i+1, line)
		}
	}
}

func TestRenderMultipleTopLevelDirectives(t *testing.T) {
	got := Render(Dir("worker_processes", "auto"), Dir("pid", "/run/nginx.pid"))
	if !strings.Contains(got, "worker_processes auto;") || !strings.Contains(got, "pid /run/nginx.pid;") {
		t.Errorf("Render() = %q, want both directives", got)
	}
}

func TestRenderNothing(t *testing.T) {
	if got := Render(); got != "\n" {
		t.Errorf("Render() = %q, want a lone newline", got)
	}
}

// Values that would break a hand-built config — quotes, spaces, nginx
// variables — must survive as written, because callers pass user data here
// (proxy hosts, header values).
func TestRenderPreservesAwkwardArgValues(t *testing.T) {
	for _, arg := range []string{`"upgrade"`, "$proxy_add_x_forwarded_for", "1y", "~*\\.(jpg)$"} {
		got := Render(Dir("set", "$x", arg))
		if !strings.Contains(got, arg) {
			t.Errorf("Render() dropped or mangled %q:\n%s", arg, got)
		}
	}
}

func TestHashComments(t *testing.T) {
	got := hashComments([]string{"", "# kept", "plain"})
	want := []string{"", "# kept", "# plain"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("hashComments()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestTrimLineWhitespace(t *testing.T) {
	if got := trimLineWhitespace("a  \n\tb\t\nc"); got != "a\n\tb\nc" {
		t.Errorf("trimLineWhitespace() = %q", got)
	}
}

func TestParams(t *testing.T) {
	got := params("a", "b")
	if len(got) != 2 || got[0].Value != "a" || got[1].Value != "b" {
		t.Errorf("params() = %+v", got)
	}
	if len(params()) != 0 {
		t.Error("params() with no args should be empty")
	}
}
