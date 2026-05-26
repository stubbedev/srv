// Package cmd — site_add_detect.go contains the project-type detection
// flow for `srv add`: probing the filesystem for compose / Dockerfile /
// static, honouring the --type override, and producing a one-liner
// detection summary.
package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/site"
)

// validateSiteSetup validates the path and discovers compose / dockerfile /
// static site. Detection order (when --type is not specified):
//  1. docker-compose.yml present → compose site
//  2. Dockerfile present         → Dockerfile site
//  3. otherwise                  → static site
//
// srv does not own language runtimes — if a project needs PHP, Node, Ruby,
// Python, etc., the user provides the Dockerfile or docker-compose.yml.
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

	// 2. Try bare Dockerfile detection.
	dockerfileInfo, err := site.DetectDockerfileSite(sitePath)
	if err != nil {
		return nil, fmt.Errorf("could not check for Dockerfile: %w", err)
	}
	if dockerfileInfo != nil {
		setup.isDockerfile = true
		setup.dockerfileInfo = dockerfileInfo
		return setup, nil
	}

	// 3. Fall back to static site.
	setup.isStatic = true
	return setup, nil
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
		return nil, fmt.Errorf("unknown site type %q — valid types: dockerfile, static, compose", typeStr)
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
