package site

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseComposeFileMissing(t *testing.T) {
	if _, err := ParseComposeFile("/nonexistent-srv-compose"); err == nil {
		t.Error("expected err")
	}
}

func TestParseComposeFileBadYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.yml")
	if err := os.WriteFile(path, []byte("not: : valid yaml :"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseComposeFile(path); err == nil {
		t.Error("expected err")
	}
}

func TestParseComposeFileValid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "compose.yml")
	body := "services:\n  web:\n    image: nginx\n    ports:\n      - 8080:80\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cf, err := ParseComposeFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if cf == nil || len(cf.Services) != 1 {
		t.Errorf("got %+v", cf)
	}
}

func TestGetServiceInfosDerivedContainerName(t *testing.T) {
	dir := t.TempDir()
	parent := filepath.Join(dir, "myproject")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "docker-compose.yml")
	if err := os.WriteFile(path, []byte("services:\n  web:\n    image: nginx\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	infos, err := GetServiceInfos(path)
	if err != nil {
		t.Fatal(err)
	}
	if infos[0].ContainerName != "myproject-web-1" {
		t.Errorf("got %q", infos[0].ContainerName)
	}
}

func TestLoadEnvFileMissing(t *testing.T) {
	envVars := map[string]string{}
	loadEnvFile("/nonexistent-srv-env", envVars)
	if len(envVars) != 0 {
		t.Errorf("expected unchanged map, got %v", envVars)
	}
}

func TestLoadEnvFileBasic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	body := "# comment\nKEY=value\nQUOTED=\"quoted value\"\nSINGLE='single'\n\nNOEQUALS\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	envVars := map[string]string{}
	loadEnvFile(path, envVars)
	if envVars["KEY"] != "value" {
		t.Errorf("KEY = %q", envVars["KEY"])
	}
	if envVars["QUOTED"] != "quoted value" {
		t.Errorf("QUOTED = %q", envVars["QUOTED"])
	}
	if envVars["SINGLE"] != "single" {
		t.Errorf("SINGLE = %q", envVars["SINGLE"])
	}
}

func TestExtractPortFromPorts(t *testing.T) {
	cases := []struct {
		ports []string
		want  int
	}{
		{[]string{"80"}, 80},
		{[]string{"8080:80"}, 80},
		{[]string{"8080:80/tcp"}, 80},
		{[]string{"127.0.0.1:8080:3000"}, 3000},
		{[]string{"${PORT}:5000"}, 5000},
		{[]string{}, 0},
		{[]string{"too:many:colons:here"}, 0},
	}
	for _, c := range cases {
		got := extractPortFromPorts(c.ports, map[string]string{"PORT": "8080"})
		if got != c.want {
			t.Errorf("extractPortFromPorts(%v) = %d, want %d", c.ports, got, c.want)
		}
	}
}

func TestDiscoverServicePortExposeFallback(t *testing.T) {
	svc := ComposeService{Expose: []string{"3000"}}
	if got := discoverServicePort(svc, nil); got != 3000 {
		t.Errorf("got %d", got)
	}
}

func TestDiscoverServicePortNone(t *testing.T) {
	if got := discoverServicePort(ComposeService{}, nil); got != 0 {
		t.Errorf("got %d", got)
	}
}

func TestExtractPortFromExposeBadStr(t *testing.T) {
	if got := extractPortFromExpose([]string{"abc"}, nil); got != 0 {
		t.Errorf("got %d", got)
	}
}

func TestLoadEnvVarsForComposeWithEnvFile(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, "app.env")
	if err := os.WriteFile(envFile, []byte("APP_PORT=4000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	composePath := filepath.Join(dir, "docker-compose.yml")
	body := "services:\n  web:\n    image: nginx\n    env_file:\n      - app.env\n"
	if err := os.WriteFile(composePath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cf, err := ParseComposeFile(composePath)
	if err != nil {
		t.Fatal(err)
	}
	env := loadEnvVarsForCompose(composePath, cf)
	if env["APP_PORT"] != "4000" {
		t.Errorf("env_file not loaded: %v", env)
	}
}

func TestLoadEnvVarsForComposeAbsoluteEnvFile(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, "abs.env")
	if err := os.WriteFile(envFile, []byte("ABS=yes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	composePath := filepath.Join(dir, "docker-compose.yml")
	body := "services:\n  web:\n    image: nginx\n    env_file:\n      - " + envFile + "\n"
	if err := os.WriteFile(composePath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cf, _ := ParseComposeFile(composePath)
	env := loadEnvVarsForCompose(composePath, cf)
	if env["ABS"] != "yes" {
		t.Errorf("abs env_file not loaded: %v", env)
	}
}
