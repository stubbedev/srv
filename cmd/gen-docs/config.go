package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/invopop/jsonschema"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/proxy"
	"github.com/stubbedev/srv/internal/redirect"
	"github.com/stubbedev/srv/internal/site"
)

func renderConfigReference() string {
	targets := []struct {
		heading string
		file    string
		value   any
	}{
		{"Site — `metadata.yml`", "sites/<name>/metadata.yml", &site.SiteMetadata{}},
		{"Proxy — `proxy-<name>.yml`", "proxies/proxy-<name>.yml", &proxy.Metadata{}},
		{"DNS-only redirect", "traefik/conf/redirect-<name>.yml", &redirect.DNSOnlyConfig{}},
		{"User config — `config.yml`", "config.yml", &config.UserConfig{}},
	}

	var b strings.Builder
	b.WriteString("# Config file reference\n\n")
	writeGeneratedNote(&b, "the Go structs behind the published JSON Schemas")
	b.WriteString("Every file srv reads, field by field. See\n")
	b.WriteString("[Configuration](configuration.md) for how the files fit together.\n\n")
	for _, t := range targets {
		fmt.Fprintf(&b, "## %s\n\n", t.heading)
		fmt.Fprintf(&b, "_Path: `~/.config/srv/%s`_\n\n", t.file)
		b.WriteString("| Field | Type | Required | Description |\n|---|---|---|---|\n")
		schema := reflectStruct(t.value)
		required := map[string]bool{}
		for _, r := range schema.Required {
			required[r] = true
		}
		if schema.Properties != nil {
			for pair := schema.Properties.Oldest(); pair != nil; pair = pair.Next() {
				req := "no"
				if required[pair.Key] {
					req = "yes"
				}
				fmt.Fprintf(&b, "| `%s` | `%s` | %s | %s |\n",
					pair.Key, schemaType(pair.Value), req, oneLine(pair.Value.Description))
			}
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

// reflectStruct mirrors gen-schema's reflector so the field reference carries
// the same doc-comment descriptions as the published JSON Schemas.
// DoNotReference inlines nested types so every field has a concrete type
// rather than a $ref the table can't render.
func reflectStruct(v any) *jsonschema.Schema {
	r := &jsonschema.Reflector{
		FieldNameTag:               "yaml",
		RequiredFromJSONSchemaTags: true,
		ExpandedStruct:             true,
		DoNotReference:             true,
	}
	if err := r.AddGoComments("github.com/stubbedev/srv", "./", jsonschema.WithFullComment()); err != nil {
		fmt.Fprintf(os.Stderr, "gen-docs: warning: doc-comment harvest failed: %v\n", err)
	}
	return r.Reflect(v)
}

func schemaType(s *jsonschema.Schema) string {
	if s == nil {
		return "—"
	}
	switch s.Type {
	case "array":
		if s.Items != nil && s.Items.Type != "" {
			return "array<" + s.Items.Type + ">"
		}
		return "array"
	case "":
		return "object"
	default:
		return s.Type
	}
}
