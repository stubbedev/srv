// Package cmd — site_add_detect.go contains the project-type detection
// flow for `srv add`: probing the filesystem for compose/PHP/Node/Ruby/etc.,
// honouring the --type override, and producing a one-liner detection summary.
package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/site"
)

// validateSiteSetup validates the path and discovers compose file or PHP/Node project.
// Detection order (when --type is not specified):
//  1. docker-compose.yml present → compose site
//  2. composer.json present      → PHP site (with full metadata)
//  3. *.php / *.phtml present    → PHP site (raw, with defaults)
//  4. package.json / deno.json   → Node.js / Bun / Deno site
//  5. Gemfile present            → Ruby site
//  6. requirements.txt / pyproject.toml / Pipfile → Python site
//  7. Dockerfile present         → Dockerfile site
//  8. otherwise                  → static site
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

	// 2. Try composer.json-based PHP detection.
	phpInfo, err := site.DetectPHPSite(sitePath)
	if err != nil {
		return nil, fmt.Errorf("could not check for PHP project: %w", err)
	}
	if phpInfo != nil {
		setup.isPHP = true
		setup.phpInfo = phpInfo
		return setup, nil
	}

	// 3. Try raw PHP file detection.
	isRawPHP, err := site.DetectRawPHPSite(sitePath)
	if err != nil {
		return nil, fmt.Errorf("could not check for PHP files: %w", err)
	}
	if isRawPHP {
		setup.isPHP = true
		setup.phpInfo = site.RawPHPDefaults()
		return setup, nil
	}

	// 4. Try Node.js / Bun / Deno detection.
	nodeInfo, err := site.DetectNodeSite(sitePath)
	if err != nil {
		return nil, fmt.Errorf("could not check for Node.js project: %w", err)
	}
	if nodeInfo != nil {
		setup.isNode = true
		setup.nodeInfo = nodeInfo
		return setup, nil
	}

	// 5. Try Ruby detection (Gemfile).
	rubyInfo, err := site.DetectRubySite(sitePath)
	if err != nil {
		return nil, fmt.Errorf("could not check for Ruby project: %w", err)
	}
	if rubyInfo != nil {
		setup.isRuby = true
		setup.rubyInfo = rubyInfo
		return setup, nil
	}

	// 6. Try Python detection.
	pythonInfo, err := site.DetectPythonSite(sitePath)
	if err != nil {
		return nil, fmt.Errorf("could not check for Python project: %w", err)
	}
	if pythonInfo != nil {
		setup.isPython = true
		setup.pythonInfo = pythonInfo
		return setup, nil
	}

	// 7. Try bare Dockerfile detection.
	dockerfileInfo, err := site.DetectDockerfileSite(sitePath)
	if err != nil {
		return nil, fmt.Errorf("could not check for Dockerfile: %w", err)
	}
	if dockerfileInfo != nil {
		setup.isDockerfile = true
		setup.dockerfileInfo = dockerfileInfo
		return setup, nil
	}

	// 8. Fall back to static site.
	setup.isStatic = true
	return setup, nil
}

// applyTypeOverride forces a specific site type, running detection only for that type.
func applyTypeOverride(setup *siteSetup, sitePath, typeStr string) (*siteSetup, error) {
	switch strings.ToLower(typeStr) {
	case "php":
		phpInfo, err := site.DetectPHPSite(sitePath)
		if err != nil || phpInfo == nil {
			phpInfo = site.RawPHPDefaults()
		}
		setup.isPHP = true
		setup.phpInfo = phpInfo
	case "node":
		nodeInfo, err := site.DetectNodeSite(sitePath)
		if err != nil || nodeInfo == nil {
			nodeInfo = site.NodeDefaults()
		}
		setup.isNode = true
		setup.nodeInfo = nodeInfo
	case "ruby":
		rubyInfo, err := site.DetectRubySite(sitePath)
		if err != nil || rubyInfo == nil {
			rubyInfo = &site.RubySiteInfo{
				RubyVersion: constants.RubyVersionLatest,
				Framework:   constants.RubyFrameworkGeneric,
				Port:        constants.RubyDefaultPort,
				StartCmd:    "sh -c 'bundle install && bundle exec ruby app.rb'",
			}
		}
		setup.isRuby = true
		setup.rubyInfo = rubyInfo
	case "python":
		pythonInfo, err := site.DetectPythonSite(sitePath)
		if err != nil || pythonInfo == nil {
			pythonInfo = &site.PythonSiteInfo{
				PythonVersion: constants.PythonVersionLatest,
				Framework:     constants.PythonFrameworkGeneric,
				Port:          constants.PythonDefaultPort,
				StartCmd:      "sh -c 'pip install -r requirements.txt && python app.py'",
			}
		}
		setup.isPython = true
		setup.pythonInfo = pythonInfo
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
		return nil, fmt.Errorf("unknown site type %q — valid types: php, node, ruby, python, dockerfile, static, compose", typeStr)
	}
	return setup, nil
}

// detectionSummary returns a human-readable summary of what was detected for a site.
func detectionSummary(setup *siteSetup) string {
	switch {
	case setup.composePath != "":
		return "docker-compose project"
	case setup.isPHP && setup.phpInfo != nil:
		fw := setup.phpInfo.Framework
		ver := setup.phpInfo.PHPVersion
		ext := len(setup.phpInfo.Extensions)
		if fw != "generic" {
			return fmt.Sprintf("%s (PHP %s, %d extensions)", fw, ver, ext)
		}
		return fmt.Sprintf("php (PHP %s, %d extensions)", ver, ext)
	case setup.isNode && setup.nodeInfo != nil:
		info := setup.nodeInfo
		runtime := info.Runtime
		if info.PackageManager != info.Runtime && info.PackageManager != constants.NodePMDeno {
			runtime += " / " + info.PackageManager
		}
		fw := info.Framework
		ver := info.NodeVersion
		if fw != "generic" {
			return fmt.Sprintf("%s (%s %s)", fw, runtime, ver)
		}
		return fmt.Sprintf("%s %s", runtime, ver)
	case setup.isRuby && setup.rubyInfo != nil:
		info := setup.rubyInfo
		if info.Framework != constants.RubyFrameworkGeneric {
			return fmt.Sprintf("%s (ruby %s)", info.Framework, info.RubyVersion)
		}
		return fmt.Sprintf("ruby %s", info.RubyVersion)
	case setup.isPython && setup.pythonInfo != nil:
		info := setup.pythonInfo
		if info.Framework != constants.PythonFrameworkGeneric {
			return fmt.Sprintf("%s (python %s)", info.Framework, info.PythonVersion)
		}
		return fmt.Sprintf("python %s", info.PythonVersion)
	case setup.isDockerfile && setup.dockerfileInfo != nil:
		return fmt.Sprintf("Dockerfile (port %d)", setup.dockerfileInfo.Port)
	case setup.isStatic:
		return "static site"
	default:
		return "unknown"
	}
}
