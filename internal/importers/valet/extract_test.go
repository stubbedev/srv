package valet

import "testing"

func TestExtractPort(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"http://localhost:6001", 6001},
		{"http://127.0.0.1:8000/path", 8000},
		{"https://api.example.com:443/v1", 443},
		{"http://named-upstream/", 0},
		{"localhost:9000;", 9000},
		{"", 0},
		{"http://no-port", 0},
	}
	for _, c := range cases {
		if got := extractPort(c.in); got != c.want {
			t.Errorf("extractPort(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestStripScheme(t *testing.T) {
	cases := []struct{ in, want string }{
		{"http://localhost:6001;", "localhost:6001"},
		{"https://api.com:443/v1", "api.com:443"},
		{"localhost:9000", "localhost:9000"},
		{"http://api.com/x?y=z", "api.com"},
	}
	for _, c := range cases {
		if got := stripScheme(c.in); got != c.want {
			t.Errorf("stripScheme(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSplitServerBlocks(t *testing.T) {
	src := `# top comment
server {
    listen 80;
    server_name a.test;
}
server {
    listen 443 ssl;
    server_name a.test;
    location / { return 200; }
}
not-a-server-block
`
	blocks := splitServerBlocks(src)
	if len(blocks) != 2 {
		t.Errorf("got %d blocks, want 2", len(blocks))
	}
}

func TestSplitServerBlocksMissingClose(t *testing.T) {
	if got := splitServerBlocks("server { unclosed"); len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

func TestSplitServerBlocksNoBraces(t *testing.T) {
	if got := splitServerBlocks("plain text"); len(got) != 0 {
		t.Errorf("got %v", got)
	}
}
