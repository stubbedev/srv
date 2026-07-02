package traefik

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestSetMetricsKey(t *testing.T) {
	doc := map[string]any{"log": map[string]any{"level": "INFO"}}

	if !setMetricsKey(doc, true) {
		t.Error("enable on doc without metrics should report change")
	}
	if _, ok := doc["metrics"]; !ok {
		t.Error("metrics block not added")
	}
	if setMetricsKey(doc, true) {
		t.Error("enable should be idempotent")
	}

	if !setMetricsKey(doc, false) {
		t.Error("disable on doc with metrics should report change")
	}
	if _, ok := doc["metrics"]; ok {
		t.Error("metrics block not removed")
	}
	if setMetricsKey(doc, false) {
		t.Error("disable should be idempotent")
	}
}

// The base template must not ship the exporter — it is opt-in via
// `srv metrics enable`.
func TestTemplateHasNoMetricsBlock(t *testing.T) {
	out, err := renderTraefikTemplate("srv-network", "x@y.com")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := yaml.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["metrics"]; ok {
		t.Error("template still contains metrics block")
	}
}
