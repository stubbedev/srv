package traefik

import (
	"strings"
	"testing"

	"github.com/stubbedev/srv/internal/engine"
	"github.com/stubbedev/srv/internal/ops"
	"gopkg.in/yaml.v3"
)

// TestRenderTraefikTemplatePositive: ordinary network/email values land at the
// right paths and the document parses cleanly.
func TestRenderTraefikTemplatePositive(t *testing.T) {
	out, err := renderTraefikTemplate("srv-network", "ops@example.com")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := yaml.Unmarshal(out, &m); err != nil {
		t.Fatalf("rendered template is not valid YAML: %v\n%s", err, out)
	}
	network := m["providers"].(map[string]any)["docker"].(map[string]any)["network"]
	if network != "srv-network" {
		t.Errorf("network = %v, want srv-network", network)
	}
	email := m["certificatesResolvers"].(map[string]any)["letsencrypt"].(map[string]any)["acme"].(map[string]any)["email"]
	if email != "ops@example.com" {
		t.Errorf("email = %v, want ops@example.com", email)
	}
}

// TestRenderTraefikTemplateInjection: a malicious email (the value srv takes
// from the user via `srv start --email`) cannot break the YAML or inject keys.
// Before the yamlpatch rewrite this value was string-substituted into the
// template text and would have produced a broken or attacker-shaped document.
func TestRenderTraefikTemplateInjection(t *testing.T) {
	// Sentinel key that the template does not contain; if it appears at the top
	// level, the email payload escaped its scalar and injected structure.
	malicious := "x@x.com\"\nevilInjectedKey: pwned\nlog:\n  level: DEBUG"
	out, err := renderTraefikTemplate("srv-network", malicious)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := yaml.Unmarshal(out, &m); err != nil {
		t.Fatalf("malicious email broke the document: %v\n%s", err, out)
	}
	if _, leaked := m["evilInjectedKey"]; leaked {
		t.Error("injection leaked a top-level 'evilInjectedKey' via the email field")
	}
	// The email must be stored verbatim as a single scalar.
	email := m["certificatesResolvers"].(map[string]any)["letsencrypt"].(map[string]any)["acme"].(map[string]any)["email"]
	if email != malicious {
		t.Errorf("email mangled:\ngot:  %q\nwant: %q", email, malicious)
	}
}

// A unix-socket runtime is bind-mounted to a fixed container path, so the
// provider endpoint is that fixed path whichever runtime srv resolved — the
// Colima/OrbStack/Podman socket differences stay entirely on the host side.
func TestRenderTraefikTemplatePinsSocketEndpoint(t *testing.T) {
	t.Cleanup(ops.SwapEngine(engine.Engine{
		Name: "colima", Binary: "docker",
		Endpoint: "unix:///home/u/.colima/default/docker.sock",
	}))
	out, err := renderTraefikTemplate("srv-network", "ops@example.com")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := yaml.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	endpoint := m["providers"].(map[string]any)["docker"].(map[string]any)["endpoint"]
	if endpoint != "unix://"+engine.ContainerSocketPath {
		t.Errorf("endpoint = %v, want the fixed container socket path", endpoint)
	}
}

// A remote daemon has no socket to bind: Traefik has to be pointed at the URL,
// and the compose file must not grow an empty volume entry.
func TestRemoteEndpointIsPassedThroughAndNotMounted(t *testing.T) {
	t.Cleanup(ops.SwapEngine(engine.Engine{
		Name: "docker", Binary: "docker", Endpoint: "tcp://10.0.0.1:2375",
	}))
	out, err := renderTraefikTemplate("srv-network", "ops@example.com")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := yaml.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	if endpoint := m["providers"].(map[string]any)["docker"].(map[string]any)["endpoint"]; endpoint != "tcp://10.0.0.1:2375" {
		t.Errorf("endpoint = %v, want the tcp url", endpoint)
	}

	compose, err := DockerComposeTemplate("srv-network", "/tmp/sites", "u", "p")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(compose, ":"+engine.ContainerSocketPath+":ro") {
		t.Errorf("compose mounts a socket for a tcp endpoint:\n%s", compose)
	}
}
