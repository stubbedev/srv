package cmd

import (
	"testing"
)

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
