// Command gen-schema emits JSON Schema for the YAML files srv writes
// (metadata.yml and config.yml) so editors with yaml-language-server can offer
// completion and validation.
//
// Output is committed to schemas/ in the repo root. The CI workflow re-runs
// this and fails if the output is dirty.
//
// Run via `just schemas` or `go run ./cmd/gen-schema`.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/invopop/jsonschema"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/proxy"
	"github.com/stubbedev/srv/internal/redirect"
	"github.com/stubbedev/srv/internal/site"
)

type target struct {
	title    string
	id       string
	filename string
	value    any
}

func main() {
	outDir := "schemas"
	if len(os.Args) > 1 {
		outDir = os.Args[1]
	}
	if err := os.MkdirAll(outDir, constants.DirPermDefault); err != nil {
		fail(err)
	}

	targets := []target{
		{
			title:    "srv site metadata",
			id:       constants.MetadataSchemaURL,
			filename: "metadata.schema.json",
			value:    &site.SiteMetadata{},
		},
		{
			title:    "srv user config",
			id:       constants.UserConfigSchemaURL,
			filename: "config.schema.json",
			value:    &config.UserConfig{},
		},
		{
			title:    "srv proxy metadata",
			id:       constants.ProxyMetadataSchemaURL,
			filename: "proxy-metadata.schema.json",
			value:    &proxy.Metadata{},
		},
		{
			title:    "srv DNS-only redirect",
			id:       constants.RedirectDNSSchemaURL,
			filename: "redirect-dns.schema.json",
			value:    &redirect.DNSOnlyConfig{},
		},
	}

	for _, t := range targets {
		if err := emit(outDir, t); err != nil {
			fail(fmt.Errorf("%s: %w", t.filename, err))
		}
	}
}

// modulePath is the Go module path; used to key harvested doc comments.
const modulePath = "github.com/stubbedev/srv"

// newReflector builds a reflector that harvests Go doc comments from the repo
// source so every struct field carries its comment as a JSON Schema
// `description` — that is what editors show as hover/completion hints. gen-schema
// always runs from the repo root (`go run ./cmd/gen-schema`), so the source is
// on disk and AddGoComments can walk it. A harvest failure is non-fatal: the
// schema is still emitted, just without descriptions.
func newReflector() *jsonschema.Reflector {
	r := &jsonschema.Reflector{
		FieldNameTag:               "yaml",
		RequiredFromJSONSchemaTags: true,
		ExpandedStruct:             true,
		DoNotReference:             false,
	}
	// WithFullComment keeps the entire doc comment as the description (the
	// default truncates type comments at the first sentence), so hover hints
	// carry the full rationale — matching how treeman renders its schema.
	if err := r.AddGoComments(modulePath, "./", jsonschema.WithFullComment()); err != nil {
		fmt.Fprintf(os.Stderr, "gen-schema: warning: could not harvest doc comments for hints: %v\n", err)
	}
	return r
}

func emit(dir string, t target) error {
	r := newReflector()
	schema := r.Reflect(t.value)
	schema.ID = jsonschema.ID(t.id)
	schema.Title = t.title

	data, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return err
	}
	// JSON files conventionally end with a newline.
	data = append(data, '\n')

	path := filepath.Join(dir, t.filename)
	if err := os.WriteFile(path, data, constants.FilePermDefault); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", path)
	return nil
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "gen-schema:", err)
	os.Exit(constants.ExitCodeError)
}
