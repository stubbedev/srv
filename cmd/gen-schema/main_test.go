package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/srv/internal/site"
)

func TestEmit(t *testing.T) {
	dir := t.TempDir()
	tg := target{
		title:    "test",
		id:       "https://example.com/x.json",
		filename: "x.json",
		value:    &site.SiteMetadata{},
	}
	if err := emit(dir, tg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "x.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Error("empty schema")
	}
}

func TestEmitFails(t *testing.T) {
	tg := target{filename: "x.json", value: &site.SiteMetadata{}, id: "https://example.com/x.json"}
	// Write to a path under a non-existent dir → os.WriteFile fails.
	if err := emit("/no/such/dir/srv-gen-schema", tg); err == nil {
		t.Error("expected err")
	}
}

func TestMain(t *testing.T) {
	dir := t.TempDir()
	prevArgs := os.Args
	defer func() { os.Args = prevArgs }()
	os.Args = []string{"gen-schema", dir}
	main()
	for _, f := range []string{"metadata.schema.json", "config.schema.json"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("missing %s: %v", f, err)
		}
	}
}
