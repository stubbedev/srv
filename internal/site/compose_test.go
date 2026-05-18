package site

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestExpandEnvVars(t *testing.T) {
	env := map[string]string{
		"PORT":  "8080",
		"EMPTY": "",
	}
	cases := []struct {
		in, want string
	}{
		{"${PORT}", "8080"},
		{"$PORT", "8080"},
		{"${MISSING}", ""},
		{"${MISSING:-3000}", "3000"},
		{"${MISSING-3000}", "3000"},
		{"${EMPTY:-9000}", "9000"},
		{"prefix-${PORT}-suffix", "prefix-8080-suffix"},
		{"no-vars", "no-vars"},
		{"$UNKNOWN", "$UNKNOWN"},
	}
	for _, c := range cases {
		got := expandEnvVars(c.in, env)
		if got != c.want {
			t.Errorf("expandEnvVars(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestComposeLabelsUnmarshalSequence(t *testing.T) {
	data := []byte("labels:\n  - a=1\n  - b=2\n")
	var s struct {
		Labels ComposeLabels `yaml:"labels"`
	}
	if err := yaml.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	if len(s.Labels) != 2 || s.Labels[0] != "a=1" {
		t.Errorf("labels = %v", s.Labels)
	}
}

func TestComposeLabelsUnmarshalMap(t *testing.T) {
	data := []byte("labels:\n  a: \"1\"\n  b: \"2\"\n")
	var s struct {
		Labels ComposeLabels `yaml:"labels"`
	}
	if err := yaml.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	if len(s.Labels) != 2 {
		t.Errorf("labels len = %d", len(s.Labels))
	}
	gotMap := map[string]bool{s.Labels[0]: true, s.Labels[1]: true}
	if !gotMap["a=1"] || !gotMap["b=2"] {
		t.Errorf("labels = %v", s.Labels)
	}
}

func TestComposeLabelsUnmarshalNull(t *testing.T) {
	data := []byte("labels: null\n")
	var s struct {
		Labels ComposeLabels `yaml:"labels"`
	}
	if err := yaml.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	if s.Labels != nil {
		t.Errorf("expected nil labels, got %v", s.Labels)
	}
}

func TestComposeEnvVarsArray(t *testing.T) {
	data := []byte("environment:\n  - KEY=value\n  - PORT=8080\n")
	var s struct {
		E ComposeEnvVars `yaml:"environment"`
	}
	if err := yaml.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	if s.E["KEY"] != "value" || s.E["PORT"] != "8080" {
		t.Errorf("envvars = %v", s.E)
	}
}

func TestComposeEnvVarsMap(t *testing.T) {
	data := []byte("environment:\n  KEY: value\n  PORT: \"8080\"\n")
	var s struct {
		E ComposeEnvVars `yaml:"environment"`
	}
	if err := yaml.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	if s.E["KEY"] != "value" || s.E["PORT"] != "8080" {
		t.Errorf("envvars = %v", s.E)
	}
}

func TestComposeStringListSingleString(t *testing.T) {
	data := []byte("env_file: .env\n")
	var s struct {
		F ComposeStringList `yaml:"env_file"`
	}
	if err := yaml.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	if len(s.F) != 1 || s.F[0] != ".env" {
		t.Errorf("env_file = %v", s.F)
	}
}

func TestComposeStringListArray(t *testing.T) {
	data := []byte("env_file:\n  - .env\n  - .env.local\n")
	var s struct {
		F ComposeStringList `yaml:"env_file"`
	}
	if err := yaml.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	if len(s.F) != 2 {
		t.Errorf("env_file = %v", s.F)
	}
}

func TestComposeLabelsUnmarshalBadSequence(t *testing.T) {
	data := []byte("labels:\n  - [a, b]\n")
	var s struct {
		Labels ComposeLabels `yaml:"labels"`
	}
	if err := yaml.Unmarshal(data, &s); err == nil {
		t.Error("expected decode err on non-string sequence")
	}
}

func TestComposeLabelsUnmarshalBadMap(t *testing.T) {
	data := []byte("labels:\n  a: [list]\n")
	var s struct {
		Labels ComposeLabels `yaml:"labels"`
	}
	if err := yaml.Unmarshal(data, &s); err == nil {
		t.Error("expected decode err on nested seq value")
	}
}

func TestComposeStringListBadSequence(t *testing.T) {
	data := []byte("env_file:\n  - {bad: kv}\n")
	var s struct {
		F ComposeStringList `yaml:"env_file"`
	}
	if err := yaml.Unmarshal(data, &s); err == nil {
		t.Error("expected decode err")
	}
}

func TestComposeEnvVarsBadSequence(t *testing.T) {
	data := []byte("environment:\n  - {invalid: shape}\n")
	var s struct {
		E ComposeEnvVars `yaml:"environment"`
	}
	if err := yaml.Unmarshal(data, &s); err == nil {
		t.Error("expected decode err")
	}
}

func TestComposeStringListNull(t *testing.T) {
	data := []byte("env_file: null\n")
	var s struct {
		F ComposeStringList `yaml:"env_file"`
	}
	if err := yaml.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	if s.F != nil {
		t.Errorf("expected nil, got %v", s.F)
	}
}

func TestFindComposeFilePriority(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"compose.yml":        "x: y\n",
		"docker-compose.yml": "x: y\n",
	})
	path, err := FindComposeFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	// docker-compose.yml comes first in priority order.
	if !endsWith(path, "docker-compose.yml") {
		t.Errorf("expected docker-compose.yml, got %s", path)
	}
}

func endsWith(s, suffix string) bool {
	if len(s) < len(suffix) {
		return false
	}
	return s[len(s)-len(suffix):] == suffix
}
