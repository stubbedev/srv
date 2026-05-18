package cmd

import (
	"testing"
)

func TestFallbackContainerName(t *testing.T) {
	if got := fallbackContainerName("blog"); got != "srv-proxy-blog-fallback" {
		t.Errorf("got %q", got)
	}
}

func TestExtractContainerFromURL(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"http://redis:6379", "redis"},
		{"http://localhost:8080", ""},
		{"http://127.0.0.1:80", ""},
		{"http://host.docker.internal:9000", ""},
		{"http://[::1]:80", ""},
		{"not-a-url", ""},
		{"", ""},
		{"https://my-app:3000/path", "my-app"},
	}
	for _, c := range cases {
		if got := extractContainerFromURL(c.in); got != c.want {
			t.Errorf("extractContainerFromURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFindFreeLoopbackPort(t *testing.T) {
	port, err := findFreeLoopbackPort()
	if err != nil {
		t.Fatal(err)
	}
	if port <= 0 || port > 65535 {
		t.Errorf("port out of range: %d", port)
	}
}
