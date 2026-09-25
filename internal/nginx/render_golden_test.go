package nginx

import "testing"

// Golden bytes: nginx consumes the rendered file directly and the e2e suite
// asserts on it, so the exact layout (blank line before every block, blank
// line inside empty blocks, comments above their directive, four-space
// indent) is pinned here.
func TestRenderGolden(t *testing.T) {
	got := Render(
		Dir("listen", "80").WithComment("", "a separated comment"),
		Dir("worker_processes", "auto"),
		Block("server", nil,
			Dir("root", "/srv"),
			Block("location", []string{"/api"}, Dir("proxy_pass", "http://up")),
			Block("location", []string{"/"}, Dir("try_files", "$uri", "=404")),
		).WithComment("the server", "", "more"),
		Block("server", nil),
		Block("upstream", []string{"app"}, Block("server", []string{"127.0.0.1:8080"})),
		Dir("gzip", "on").WithComment("after block"),
	)
	want := `# a separated comment
listen 80;
worker_processes auto;

# the server

# more
server {
    root /srv;

    location /api {
        proxy_pass http://up;
    }

    location / {
        try_files $uri =404;
    }
}

server {

}

upstream app {

    server 127.0.0.1:8080 {

    }
}
# after block
gzip on;
`
	if got != want {
		t.Errorf("Render() mismatch:\n got: %q\nwant: %q", got, want)
	}
}
