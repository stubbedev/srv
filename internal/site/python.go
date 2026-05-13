// Package site handles site management operations.
package site

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
)

// =============================================================================
// Python Site Detection
// =============================================================================

// PythonSiteInfo holds detected configuration for a Python project.
type PythonSiteInfo struct {
	PythonVersion string // "latest", "3.12", etc.
	Framework     string // "django", "fastapi", "flask", "generic"
	StartCmd      string // e.g. "python manage.py runserver 0.0.0.0:8000"
	Port          int    // Container port to proxy to
}

// DetectPythonSite checks whether dir contains a Python project.
// Detection: requirements.txt, pyproject.toml, Pipfile, or setup.py.
func DetectPythonSite(dir string) (*PythonSiteInfo, error) {
	markers := []string{"requirements.txt", "pyproject.toml", "Pipfile", "setup.py"}
	found := ""
	for _, m := range markers {
		if fileExists(filepath.Join(dir, m)) {
			found = m
			break
		}
	}
	if found == "" {
		return nil, nil
	}

	framework := detectPythonFramework(dir)
	version := detectPythonVersion(dir)
	port := constants.PythonDefaultPort
	startCmd := buildPythonStartCmd(framework, port)

	return &PythonSiteInfo{
		PythonVersion: version,
		Framework:     framework,
		StartCmd:      startCmd,
		Port:          port,
	}, nil
}

// detectPythonFramework reads requirements/pyproject and looks for known packages.
func detectPythonFramework(dir string) string {
	content := readPythonDeps(dir)
	switch {
	case strings.Contains(content, "django") || strings.Contains(content, "Django"):
		return constants.PythonFrameworkDjango
	case strings.Contains(content, "fastapi") || strings.Contains(content, "FastAPI"):
		return constants.PythonFrameworkFastAPI
	case strings.Contains(content, "flask") || strings.Contains(content, "Flask"):
		return constants.PythonFrameworkFlask
	default:
		return constants.PythonFrameworkGeneric
	}
}

// readPythonDeps returns the concatenated contents of dependency files for framework detection.
func readPythonDeps(dir string) string {
	var parts []string
	for _, name := range []string{"requirements.txt", "pyproject.toml", "Pipfile"} {
		if data, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
			parts = append(parts, strings.ToLower(string(data)))
		}
	}
	return strings.Join(parts, "\n")
}

// detectPythonVersion reads .python-version or returns "latest".
func detectPythonVersion(dir string) string {
	path := filepath.Join(dir, ".python-version")
	data, err := os.ReadFile(path)
	if err != nil {
		return constants.PythonVersionLatest
	}
	v := strings.TrimSpace(string(data))
	if v == "" {
		return constants.PythonVersionLatest
	}
	// "3.12.0" → "3.12"
	parts := strings.SplitN(v, ".", 3)
	if len(parts) >= 2 {
		return parts[0] + "." + parts[1]
	}
	return v
}

// buildPythonStartCmd builds the server start command for a given framework.
func buildPythonStartCmd(framework string, port int) string {
	switch framework {
	case constants.PythonFrameworkDjango:
		return fmt.Sprintf("sh -c 'pip install -r requirements.txt && python manage.py runserver 0.0.0.0:%d'", port)
	case constants.PythonFrameworkFastAPI:
		return fmt.Sprintf("sh -c 'pip install -r requirements.txt && uvicorn main:app --host 0.0.0.0 --port %d --reload'", port)
	case constants.PythonFrameworkFlask:
		return fmt.Sprintf("sh -c 'pip install -r requirements.txt && flask run --host 0.0.0.0 --port %d'", port)
	default:
		return "sh -c 'pip install -r requirements.txt && python app.py'"
	}
}

// PythonImageTag returns the Docker image tag for the given Python version.
func PythonImageTag(version string) string {
	if version == "" || version == constants.PythonVersionLatest {
		return constants.PythonImageAlpine
	}
	return fmt.Sprintf(constants.PythonImageFormat, version)
}

// =============================================================================
// Docker Compose generation (Python)
// =============================================================================

type pythonServiceConfig struct {
	ContainerName string             `yaml:"container_name"`
	Image         string             `yaml:"image"`
	Command       string             `yaml:"command"`
	WorkingDir    string             `yaml:"working_dir"`
	Volumes       []nodeVolumeConfig `yaml:"volumes"`
	Environment   map[string]string  `yaml:"environment,omitempty"`
	Labels        map[string]string  `yaml:"labels,omitempty"`
	Networks      []string           `yaml:"networks"`
	Restart       string             `yaml:"restart"`
	HealthCheck   *staticHealthCheck `yaml:"healthcheck,omitempty"`
}

type pythonComposeConfig struct {
	Name     string                         `yaml:"name,omitempty"`
	Services map[string]pythonServiceConfig `yaml:"services"`
	Networks map[string]nodeNetworkConfig   `yaml:"networks"`
}

// WritePythonSiteConfig generates and writes docker-compose.yml for a Python site.
func WritePythonSiteConfig(name string, meta SiteMetadata, info *PythonSiteInfo, force bool) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	siteDir := SiteConfigDir(cfg, name)
	if err := os.MkdirAll(siteDir, constants.DirPermDefault); err != nil {
		return fmt.Errorf("failed to create site config directory: %w", err)
	}

	containerName := "srv-" + name + "-app"
	image := PythonImageTag(info.PythonVersion)
	labels := buildAppTraefikLabels(name, meta.Domains, meta.IsLocal, meta.Wildcard, info.Port)
	if HasListener(meta.Listeners, constants.ListenerInternal) {
		addInternalListenerLabels(labels, name, meta.Domains, meta.Wildcard)
	}
	StampSrvLabels(labels, name, string(meta.Type))

	env := map[string]string{
		"PORT":                    fmt.Sprintf("%d", info.Port),
		"PYTHONPATH":              constants.PythonDockerWorkDir,
		"PYTHONDONTWRITEBYTECODE": "1",
		"PYTHONUNBUFFERED":        "1",
	}
	if info.Framework == constants.PythonFrameworkFlask {
		env["FLASK_ENV"] = "development"
		env["FLASK_DEBUG"] = "1"
	}

	composeConfig := pythonComposeConfig{
		Name:     constants.ComposeProjectName,
		Services: map[string]pythonServiceConfig{
			"app": {
				ContainerName: containerName,
				Image:         image,
				Command:       info.StartCmd,
				WorkingDir:    constants.PythonDockerWorkDir,
				Volumes: []nodeVolumeConfig{
					{
						Type:        "bind",
						Source:      meta.ProjectPath,
						Target:      constants.PythonDockerWorkDir,
						Consistency: volumeConsistencyForHost(),
					},
				},
				Environment: env,
				Labels:      labels,
				Networks:    []string{constants.TraefikSubdir},
				Restart:     constants.RestartUnlessStopped,
				HealthCheck: makeStaticHealthCheck(info.Port),
			},
		},
		Networks: map[string]nodeNetworkConfig{
			constants.TraefikSubdir: {
				Name:     meta.NetworkName,
				External: true,
			},
		},
	}

	data, err := yaml.Marshal(&composeConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal compose config: %w", err)
	}

	header := fmt.Sprintf(`# Generated by srv - python site (%s)
# Project: %s
#
# This file is yours to edit. Changes take effect on next restart.
#
# Common customisations:
#   environment:
#     PORT: "%d"               # Override listen port
#     DEBUG: "false"           # Disable debug mode for production

`, info.Framework, meta.ProjectPath, info.Port)
	content := header + string(data)

	composePath := SiteComposePath(cfg, name)
	return writeFile(composePath, []byte(content), force)
}
