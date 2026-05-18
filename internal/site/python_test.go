package site

import (
	"strings"
	"testing"

	"github.com/stubbedev/srv/internal/constants"
)

func TestDetectPythonFramework(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{"django", "Django==4.2", constants.PythonFrameworkDjango},
		{"django-lowercase", "django==4.2", constants.PythonFrameworkDjango},
		{"fastapi", "fastapi==0.100\n", constants.PythonFrameworkFastAPI},
		{"flask", "Flask==2.3", constants.PythonFrameworkFlask},
		{"none", "requests==2.0", constants.PythonFrameworkGeneric},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFiles(t, dir, map[string]string{"requirements.txt": c.content})
			got := detectPythonFramework(dir)
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestDetectPythonVersion(t *testing.T) {
	cases := []struct {
		content string
		want    string
	}{
		{"3.12.0\n", "3.12"},
		{"3.11\n", "3.11"},
		{"", constants.PythonVersionLatest},
	}
	for _, c := range cases {
		dir := t.TempDir()
		writeFiles(t, dir, map[string]string{".python-version": c.content})
		if got := detectPythonVersion(dir); got != c.want {
			t.Errorf("content %q -> %q, want %q", c.content, got, c.want)
		}
	}
}

func TestDetectPythonVersionMissingFile(t *testing.T) {
	dir := t.TempDir()
	if got := detectPythonVersion(dir); got != constants.PythonVersionLatest {
		t.Errorf("missing -> %q, want latest", got)
	}
}

func TestBuildPythonStartCmd(t *testing.T) {
	cases := []struct {
		fw   string
		want string
	}{
		{constants.PythonFrameworkDjango, "manage.py"},
		{constants.PythonFrameworkFastAPI, "uvicorn"},
		{constants.PythonFrameworkFlask, "flask run"},
		{constants.PythonFrameworkGeneric, "python app.py"},
	}
	for _, c := range cases {
		got := buildPythonStartCmd(c.fw, 8000)
		if !strings.Contains(got, c.want) {
			t.Errorf("%s -> %q, missing %q", c.fw, got, c.want)
		}
	}
}

func TestPythonImageTag(t *testing.T) {
	if got := PythonImageTag(""); got != constants.PythonImageAlpine {
		t.Errorf("empty -> %q", got)
	}
	if got := PythonImageTag(constants.PythonVersionLatest); got != constants.PythonImageAlpine {
		t.Errorf("latest -> %q", got)
	}
	if got := PythonImageTag("3.12"); got != "python:3.12-alpine" {
		t.Errorf("3.12 -> %q", got)
	}
}

func TestDetectPythonSiteMissing(t *testing.T) {
	dir := t.TempDir()
	info, err := DetectPythonSite(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info != nil {
		t.Error("expected nil")
	}
}

func TestDetectPythonSiteFlask(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"requirements.txt": "Flask==2.3\n",
	})
	info, err := DetectPythonSite(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info == nil {
		t.Fatal("expected detected")
	}
	if info.Framework != constants.PythonFrameworkFlask {
		t.Errorf("Framework = %q, want flask", info.Framework)
	}
	if info.Port != constants.PythonDefaultPort {
		t.Errorf("Port = %d", info.Port)
	}
}

func TestDetectPythonSitePyprojectMarker(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"pyproject.toml": "[project]\nname=\"app\"\n",
	})
	info, err := DetectPythonSite(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info == nil {
		t.Fatal("expected detected via pyproject")
	}
}
