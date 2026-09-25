package mcp

import (
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/stubbedev/srv/internal/proxy"
	"github.com/stubbedev/srv/internal/redirect"
	"github.com/stubbedev/srv/internal/site"
)

// advertisedSchemas maps every advertised tool name to its input schema as
// clients receive it over the wire (a generic JSON object).
func advertisedSchemas(t *testing.T) map[string]map[string]any {
	t.Helper()
	tools, err := fullToolSurface(t.Context())
	if err != nil {
		t.Fatalf("advertisedTools: %v", err)
	}
	out := make(map[string]map[string]any, len(tools))
	for _, tool := range tools {
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Fatalf("tool %q: input schema is %T, want a JSON object", tool.Name, tool.InputSchema)
		}
		out[tool.Name] = schema
	}
	return out
}

// schemaProps flattens an input schema into property name → description. A
// nullable property (invopop renders it as oneOf: [T, null]) has no
// top-level description; it is read from the first, non-null branch instead.
func schemaProps(t *testing.T, schema map[string]any) map[string]string {
	t.Helper()
	raw, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("input schema has no properties object: %v", schema)
	}
	props := make(map[string]string, len(raw))
	for name, v := range raw {
		p, ok := v.(map[string]any)
		if !ok {
			t.Fatalf("property %q is %T, want a JSON object", name, v)
		}
		desc, _ := p["description"].(string)
		if desc == "" {
			if oneOf, ok := p["oneOf"].([]any); ok && len(oneOf) > 0 {
				if branch, ok := oneOf[0].(map[string]any); ok {
					desc, _ = branch["description"].(string)
				}
			}
		}
		props[name] = desc
	}
	return props
}

// reflectedProps describes what a shared spec struct advertises through the
// same reflection the tools register with: property name → description.
func reflectedProps(t *testing.T, raw json.RawMessage) map[string]string {
	t.Helper()
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("reflected schema does not unmarshal: %v", err)
	}
	return schemaProps(t, schema)
}

// mergeProps layers MCP-only properties (or description overrides) on top of
// the shared spec's properties.
func mergeProps(shared, extras map[string]string) map[string]string {
	out := maps.Clone(shared)
	maps.Copy(out, extras)
	return out
}

// The add tools must advertise exactly the properties of the shared headless
// specs their handlers decode, with the descriptions of those specs — that is
// the whole point of reflecting the schemas. Anything else (a hand-added
// field here, a CLI-only spec field like the proxy fallback or redirect
// permanent leaking in there) is drift this test fails on.
func TestAddToolInputSchemasDeriveFromSharedSpecs(t *testing.T) {
	schemas := advertisedSchemas(t)

	addSiteShared := reflectedProps(t, toolInputSchema[site.AddOptions]())
	addRouteShared := reflectedProps(t, toolInputSchema[site.RouteInput]())
	addProxyShared := reflectedProps(t, toolInputSchema[proxy.AddSpec]())
	addRedirectShared := reflectedProps(t, toolInputSchema[redirect.AddSpec]())
	for name, shared := range map[string]map[string]string{
		"site.AddOptions": addSiteShared, "site.RouteInput": addRouteShared,
		"proxy.AddSpec": addProxyShared, "redirect.AddSpec": addRedirectShared,
	} {
		if len(shared) == 0 {
			t.Fatalf("%s reflects to zero properties; the helper or its tags are broken", name)
		}
	}

	tests := []struct {
		tool     string
		want     map[string]string // property → description, shared props plus MCP-only extras
		required []string
	}{
		{"add_site", mergeProps(addSiteShared, map[string]string{
			// MCP-only start tri-state: overrides the embedded bool's
			// description, nil still means default-on.
			"start": "start the containers after adding (default true)",
		}), []string{"path", "domain"}},
		{"add_route", mergeProps(addRouteShared, map[string]string{
			"target": "site or proxy name to attach the route to", // MCP-only target selector
		}), []string{"target"}},
		// add_proxy decodes proxy.AddSpec directly; the CLI-only fallback
		// fields are json:"-" and must not leak into the schema.
		{"add_proxy", addProxyShared, []string{"domain"}},
		{"add_redirect", mergeProps(addRedirectShared, map[string]string{
			"temporary": "use a 302 instead of 301 (HTTP mode only)", // negation of the CLI-only permanent
		}), []string{"domain", "to"}},
	}

	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			schema, ok := schemas[tt.tool]
			if !ok {
				t.Fatalf("tool %q is not advertised", tt.tool)
			}
			if schema["type"] != "object" {
				t.Errorf("schema type = %v, want object", schema["type"])
			}
			switch ap := schema["additionalProperties"].(type) {
			case bool:
				if ap {
					t.Errorf("additionalProperties = true, want closed")
				}
			case map[string]any:
				if len(ap) == 0 {
					t.Errorf("additionalProperties = %v, want closed ({\"not\":{}})", ap)
				}
			default:
				t.Errorf("additionalProperties = %v, want closed", schema["additionalProperties"])
			}

			if got, want := schemaProps(t, schema), tt.want; !maps.Equal(got, want) {
				t.Errorf("properties drifted from the shared specs\n got: %v\nwant: %v", got, want)
			}
			for name, desc := range tt.want {
				if strings.TrimSpace(desc) == "" {
					t.Errorf("property %q has an empty description", name)
				}
			}

			var reqRaw []any
			if req, ok := schema["required"].([]any); ok {
				reqRaw = req
			}
			gotRequired := make([]string, 0, len(reqRaw))
			for _, r := range reqRaw {
				s, _ := r.(string)
				gotRequired = append(gotRequired, s)
			}
			if want := slices.Sorted(slices.Values(tt.required)); !slices.Equal(slices.Sorted(slices.Values(gotRequired)), want) {
				t.Errorf("required = %v, want %v", gotRequired, want)
			}
		})
	}
}

// Call the real tool chain with arguments that are rejected by schema
// validation, before any handler code runs. This is the wire-compat guard:
// required-ness, closedness and property types must behave exactly as they
// did when the SDK inferred these schemas by hand-written struct reflection.
func TestAddToolInputSchemasRejectInvalidArguments(t *testing.T) {
	tests := []struct {
		tool string
		args string
	}{
		{"add_site", `{}`},                                                             // path and domain required
		{"add_site", `{"path":"/tmp/demo"}`},                                           // domain required
		{"add_site", `{"path":"/tmp/demo","domain":"d.test","nope":1}`},                // closed
		{"add_route", `{"path":"/api"}`},                                               // target required
		{"add_route", `{"target":"demo","path":"/api","port":"3000"}`},                 // port is an integer
		{"add_proxy", `{}`},                                                            // domain required
		{"add_proxy", `{"domain":"d.test","fallback_url":"https://z.example.com"}`},    // CLI-only fallback stays out
		{"add_proxy", `{"domain":"d.test","port":8080}`},                               // port is a string
		{"add_redirect", `{"domain":"d.test"}`},                                        // to required
		{"add_redirect", `{"domain":"d.test","to":"https://x.test","permanent":true}`}, // CLI-only permanent stays out
	}
	for _, tt := range tests {
		t.Run(tt.tool+"/"+tt.args, func(t *testing.T) {
			err := callToolForValidation(t, tt.tool, tt.args)
			if err == nil {
				t.Fatalf("arguments were accepted, want a schema-validation error")
			}
			if !strings.Contains(err.Error(), `validating "arguments"`) {
				t.Fatalf("error %v is not a schema-validation error", err)
			}
		})
	}
}

// callToolForValidation drives a real tool call over the in-memory transport
// and fails the test unless the arguments are rejected by schema validation
// before any handler code runs. It returns the SDK's validation error text.
func callToolForValidation(t *testing.T, name, args string) error {
	t.Helper()
	srv := newServer()
	registerReadTools(srv)
	registerDiagTools(srv)
	registerWriteTools(srv)

	ctx := t.Context()
	serverTransport, clientTransport := mcpsdk.NewInMemoryTransports()
	serverSession, err := srv.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer func() { _ = serverSession.Close() }()
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "schema-test", Version: "validation"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer func() { _ = clientSession.Close() }()

	res, err := clientSession.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      name,
		Arguments: json.RawMessage(args),
	})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	if !res.IsError {
		return nil
	}
	var msg string
	for _, c := range res.Content {
		if tc, ok := c.(*mcpsdk.TextContent); ok {
			msg = tc.Text
		}
	}
	return errors.New(msg)
}

// The MCP-only start tri-state must stay nullable: a client that sends an
// explicit null (meaning "use the default", as with the SDK-inferred schema
// before the shared-spec switch) is still schema-valid.
func TestAddSiteSchemaKeepsStartNullable(t *testing.T) {
	schemas := advertisedSchemas(t)
	raw, ok := schemas["add_site"]["properties"].(map[string]any)["start"].(map[string]any)
	if !ok {
		t.Fatalf("add_site schema has no start property: %v", schemas["add_site"])
	}
	oneOf, ok := raw["oneOf"].([]any)
	if !ok {
		t.Fatalf("start = %v, want a nullable oneOf", raw)
	}
	nullable := false
	for _, branch := range oneOf {
		if b, ok := branch.(map[string]any); ok && b["type"] == "null" {
			nullable = true
		}
	}
	if !nullable {
		t.Errorf("start does not accept null: %v", raw)
	}
}
