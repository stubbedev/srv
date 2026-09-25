package mcp

import (
	"encoding/json"
	"reflect"

	"github.com/invopop/jsonschema"
)

// toolInputSchema derives a tool's input schema from its handler's input type
// by reflection, instead of hand-writing one per tool. Property names come
// from the json tags and descriptions from the jsonschema tags of the
// underlying structs — for the add tools those structs are the shared headless
// specs (site.AddOptions, site.RouteInput, proxy.AddSpec, redirect.AddSpec),
// so each field is described in exactly one place and the CLI, the published
// YAML schemas, and the MCP surface cannot drift.
//
// The MCP SDK's own inference cannot be used for this: google/jsonschema-go
// treats the jsonschema tag as the verbatim description and rejects any tag
// beginning with `WORD=` — including the repo's `description=…` convention.
// The schema is therefore built here with invopop/jsonschema and handed to
// AddTool as an explicit Tool.InputSchema; the handler still decodes into its
// own input struct, which embeds the shared spec so json flattening keeps the
// wire names identical.
//
// The output mirrors what the SDK's inference used to produce for these tools:
// embedded spec structs are flattened (ExpandedStruct), nested structs are
// inlined rather than $ref'd (DoNotReference), no $id/$schema scaffolding is
// added, and structs are closed with additionalProperties:false.
func toolInputSchema[In any]() json.RawMessage {
	r := &jsonschema.Reflector{
		Anonymous:      true, // no $id derived from the Go package path
		ExpandedStruct: true, // flatten the fields of embedded shared specs
		DoNotReference: true, // inline nested structs (site.VolumeMount), no $defs
	}
	s := r.Reflect(new(In))
	s.Version = "" // tool input schemas carry no draft declaration
	b, err := json.Marshal(s)
	if err != nil {
		// Registration runs at server start; an unmarshalable schema is a
		// programming error, not a runtime condition.
		panic("mcp: input schema for " + reflect.TypeFor[In]().Name() + " does not marshal: " + err.Error())
	}
	return b
}
