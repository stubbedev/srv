package site

import (
	"os"
	"strings"
	"testing"

	"github.com/stubbedev/srv/internal/constants"
)

func TestParseDockerfileExposePort(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int
	}{
		{"single", "FROM nginx\nEXPOSE 8080\n", 8080},
		{"protocol", "EXPOSE 8080/tcp\n", 8080},
		{"udp", "EXPOSE 53/udp\n", 53},
		{"lowercase", "expose 9000\n", 9000},
		{"no-expose", "FROM nginx\nCMD nginx\n", 0},
		{"first-wins", "EXPOSE 80\nEXPOSE 443\n", 80},
		{"invalid", "EXPOSE abc\nEXPOSE 80\n", 80},
		{"empty-arg", "EXPOSE\n", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseDockerfileExposePort(strings.NewReader(c.in))
			if got != c.want {
				t.Errorf("got %d, want %d", got, c.want)
			}
		})
	}
}

func TestDetectDockerfileSiteMissing(t *testing.T) {
	dir := t.TempDir()
	info, err := DetectDockerfileSite(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info != nil {
		t.Errorf("expected nil, got %+v", info)
	}
}

func TestDetectDockerfileSitePresent(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		constants.DockerfileFile: "FROM nginx\nEXPOSE 8080\n",
	})
	info, err := DetectDockerfileSite(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info == nil {
		t.Fatal("expected info")
	}
	if info.Port != 8080 {
		t.Errorf("Port = %d, want 8080", info.Port)
	}
}

func TestDetectDockerfileSiteDefaultPort(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		constants.DockerfileFile: "FROM nginx\n",
	})
	info, err := DetectDockerfileSite(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info == nil || info.Port != constants.DockerfileDefaultPort {
		t.Errorf("Port = %d, want default %d", info.Port, constants.DockerfileDefaultPort)
	}
}

func TestWriteDockerfileSiteConfigInternalListener(t *testing.T) {
	root := withSRVRoot(t)
	projectDir := root + "/p"
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := SiteMetadata{
		Type:        SiteTypeDockerfile,
		Domains:     []string{"app.local"},
		ProjectPath: projectDir,
		Port:        8080,
		IsLocal:     true,
		NetworkName: "n",
		Listeners:   []string{"internal"},
	}
	if err := WriteDockerfileSiteConfig("app", meta, &DockerfileSiteInfo{Port: 8080}, true); err != nil {
		t.Fatalf("err: %v", err)
	}
}
