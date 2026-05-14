package site

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
)

func TestWriteSiteMetadataStampsSchemaModeline(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SRV_ROOT", tmp)
	config.ResetCache()
	t.Cleanup(config.ResetCache)

	meta := SiteMetadata{
		Type:        SiteTypeStatic,
		Domains:     []string{"foo.test"},
		ProjectPath: tmp,
		Port:        80,
		IsLocal:     true,
		NetworkName: "test_net",
	}
	if err := WriteSiteMetadata("foo", meta); err != nil {
		t.Fatalf("WriteSiteMetadata: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(tmp, "sites", "foo", constants.MetadataFile))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	got := string(data)
	wantLine := "# yaml-language-server: $schema=" + constants.MetadataSchemaURL
	if !strings.Contains(got, wantLine) {
		t.Errorf("metadata.yml missing schema modeline; got:\n%s", got)
	}
	if !strings.HasPrefix(got, "# yaml-language-server:") {
		t.Errorf("schema modeline must be the first line; got:\n%s", got)
	}
}

func TestWriteSiteMetadataRoundtrip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SRV_ROOT", tmp)
	config.ResetCache()
	t.Cleanup(config.ResetCache)

	want := SiteMetadata{
		Type:        SiteTypePHP,
		Domains:     []string{"app.test", "alias.test"},
		ProjectPath: tmp,
		Port:        9000,
		IsLocal:     true,
		NetworkName: "test_net",
		Upstream: &Upstream{
			Kind: "container",
			Port: 80,
		},
	}
	if err := WriteSiteMetadata("app", want); err != nil {
		t.Fatalf("WriteSiteMetadata: %v", err)
	}
	got, err := ReadSiteMetadata("app")
	if err != nil {
		t.Fatalf("ReadSiteMetadata: %v", err)
	}
	if got == nil {
		t.Fatal("ReadSiteMetadata returned nil")
	}
	if got.Type != want.Type || got.Port != want.Port || got.NetworkName != want.NetworkName {
		t.Errorf("roundtrip mismatch:\n got: %+v\nwant: %+v", got, want)
	}
	if got.Upstream == nil || got.Upstream.Kind != "container" || got.Upstream.Port != 80 {
		t.Errorf("Upstream lost in roundtrip: %+v", got.Upstream)
	}
	if len(got.Domains) != 2 || got.Domains[0] != "app.test" {
		t.Errorf("Domains lost in roundtrip: %+v", got.Domains)
	}
	if got.SchemaVersion != CurrentMetadataSchema {
		t.Errorf("SchemaVersion = %d, want %d", got.SchemaVersion, CurrentMetadataSchema)
	}
}
