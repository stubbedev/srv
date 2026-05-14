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
	}

	for _, t := range targets {
		if err := emit(outDir, t); err != nil {
			fail(fmt.Errorf("%s: %w", t.filename, err))
		}
	}
}

func emit(dir string, t target) error {
	r := &jsonschema.Reflector{
		FieldNameTag:               "yaml",
		RequiredFromJSONSchemaTags: true,
		ExpandedStruct:             true,
		DoNotReference:             false,
	}
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
