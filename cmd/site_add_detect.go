// Package cmd — site_add_detect.go contains the project-type detection
// flow for `srv add`: probing the filesystem for compose/PHP/Node/Ruby/etc.,
// honouring the --type override, and producing a one-liner detection summary.
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/site"
)

// validateSiteSetup validates the path and discovers compose / dockerfile /
// static site. Detection order (when --type is not specified):
//  1. docker-compose.yml present → compose site
//  2. language project detected (composer.json/.php, package.json, Gemfile,
//     requirements.txt/pyproject.toml/Pipfile) without a Dockerfile or
//     docker-compose.yml → ERROR pointing at `srv scaffold <lang>` or --type
//  3. Dockerfile present         → Dockerfile site
//  4. otherwise                  → static site
func validateSiteSetup(pathArg string) (*siteSetup, error) {
	sitePath, err := site.ResolvePath(pathArg)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}

	if _, err := os.Stat(sitePath); err != nil {
		return nil, fmt.Errorf("path does not exist: %s", sitePath)
	}

	setup := &siteSetup{
		sitePath: sitePath,
		port:     addFlags.port,
	}

	// --type override: skip auto-detection and set type directly.
	if addFlags.typeOverride != "" {
		return applyTypeOverride(setup, sitePath, addFlags.typeOverride)
	}

	// 1. Try to find a compose file.
	composePath, err := site.FindComposeFile(sitePath)
	if err != nil && !site.IsNotFoundError(err) {
		return nil, fmt.Errorf("could not check for docker-compose file: %w", err)
	}
	if err == nil {
		setup.composePath = composePath
		return setup, nil
	}

	// 2. Language project without a Dockerfile / docker-compose.yml: srv no
	//    longer owns runtime versions. Refuse and point at scaffold + --type.
	if lang := detectLanguageProject(sitePath); lang != "" {
		return nil, fmt.Errorf(
			"this looks like a %s project but no Dockerfile or docker-compose.yml is present.\n"+
				"  srv no longer manages language runtimes directly.\n"+
				"  options:\n"+
				"    1. `srv scaffold --lang %s` to generate a Dockerfile + docker-compose.yml in the project\n"+
				"    2. write your own Dockerfile / docker-compose.yml and re-run `srv add`\n"+
				"    3. pass `--type static` to serve only the static files in the project",
			lang, lang)
	}

	// 3. Try bare Dockerfile detection.
	dockerfileInfo, err := site.DetectDockerfileSite(sitePath)
	if err != nil {
		return nil, fmt.Errorf("could not check for Dockerfile: %w", err)
	}
	if dockerfileInfo != nil {
		setup.isDockerfile = true
		setup.dockerfileInfo = dockerfileInfo
		return setup, nil
	}

	// 4. Fall back to static site.
	setup.isStatic = true
	return setup, nil
}

// languageMarkers maps a language label to the project-root files that would
// have made srv claim it before the runtime strip. Probed in declaration
// order; first hit wins. PHP also falls back to a `.php` file scan via
// site.DetectRawPHPSite (covers projects without composer.json).
var languageMarkers = []struct {
	lang  string
	files []string
}{
	{"php", []string{"composer.json"}},
	{"node", []string{"package.json", "deno.json"}},
	{"ruby", []string{"Gemfile"}},
	{"python", []string{"requirements.txt", "pyproject.toml", "Pipfile"}},
}

// detectLanguageProject returns the language label (php/node/ruby/python) of
// a project at dir that srv would have previously managed a runtime for.
// Returns "" when none of the language markers are present. Used to surface
// a hard error in `srv add` directing the user at `srv scaffold` or --type.
func detectLanguageProject(dir string) string {
	for _, m := range languageMarkers {
		for _, f := range m.files {
			if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
				return m.lang
			}
		}
	}
	// PHP fallback: raw .php files at the root without composer.json.
	if ok, _ := site.DetectRawPHPSite(dir); ok {
		return "php"
	}
	return ""
}

// applyTypeOverride forces a specific site type, running detection only for that type.
func applyTypeOverride(setup *siteSetup, sitePath, typeStr string) (*siteSetup, error) {
	switch strings.ToLower(typeStr) {
	case "dockerfile":
		dockerfileInfo, err := site.DetectDockerfileSite(sitePath)
		if err != nil || dockerfileInfo == nil {
			dockerfileInfo = &site.DockerfileSiteInfo{Port: constants.DockerfileDefaultPort}
		}
		setup.isDockerfile = true
		setup.dockerfileInfo = dockerfileInfo
	case "static":
		setup.isStatic = true
	case "compose":
		composePath, err := site.FindComposeFile(sitePath)
		if err != nil {
			return nil, fmt.Errorf("no docker-compose.yml found (required for --type compose)")
		}
		setup.composePath = composePath
	default:
		return nil, fmt.Errorf("unknown site type %q — valid types: dockerfile, static, compose (language runtimes are user-owned now; use `srv scaffold` to generate a Dockerfile)", typeStr)
	}
	return setup, nil
}

// detectionSummary returns a human-readable summary of what was detected for a site.
func detectionSummary(setup *siteSetup) string {
	switch {
	case setup.composePath != "":
		return "docker-compose project"
	case setup.isDockerfile && setup.dockerfileInfo != nil:
		return fmt.Sprintf("Dockerfile (port %d)", setup.dockerfileInfo.Port)
	case setup.isStatic:
		return "static site"
	default:
		return "unknown"
	}
}
