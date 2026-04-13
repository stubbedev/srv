// Package site handles site management operations.
package site

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/stubbedev/srv/internal/config"
	"github.com/stubbedev/srv/internal/constants"
)

// =============================================================================
// Node.js / Bun / Deno Site Detection
// =============================================================================

// NodeSiteInfo holds detected configuration for a Node.js, Bun, or Deno project.
type NodeSiteInfo struct {
	Runtime        string // "node", "bun", "deno"
	PackageManager string // "npm", "yarn", "pnpm", "bun", "deno"
	NodeVersion    string // "lts", "20", etc. (node runtime only)
	Framework      string // "next", "nuxt", "vite", "express", "nestjs", "generic"
	StartCmd       string // e.g. "npm run dev"
	Port           int    // Container port to proxy to
}

// packageJSON represents the fields of a package.json file that srv cares about.
type packageJSON struct {
	Scripts         map[string]string `json:"scripts"`
	Engines         map[string]string `json:"engines"`
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
	PackageManager  string            `json:"packageManager"` // e.g. "pnpm@9.0.0" (corepack standard)
}

// denoConfig represents the fields of a deno.json / deno.jsonc file that srv cares about.
type denoConfig struct {
	Tasks map[string]string `json:"tasks"`
}

// DetectNodeSite checks whether dir contains a Node.js / Bun / Deno project.
// Returns nil if no matching project is found.
//
// Detection order:
//  1. deno.json / deno.jsonc present → Deno project
//  2. package.json present           → Node.js or Bun project
func DetectNodeSite(dir string) (*NodeSiteInfo, error) {
	// Deno takes priority — its config file is unambiguous.
	if info := detectDenoSite(dir); info != nil {
		return info, nil
	}

	pkgPath := filepath.Join(dir, "package.json")
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading package.json: %w", err)
	}

	var pkg packageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		// Malformed package.json — use safe npm defaults rather than failing hard.
		return NodeDefaults(), nil
	}

	pm := detectPM(dir, &pkg)
	runtime := runtimeForPM(pm)
	framework := detectNodeFramework(&pkg)
	version := detectNodeVersion(dir, &pkg)
	port := detectNodePort(framework)
	startCmd := buildStartCmd(pm, framework, pkg.Scripts)

	return &NodeSiteInfo{
		Runtime:        runtime,
		PackageManager: pm,
		NodeVersion:    version,
		Framework:      framework,
		StartCmd:       startCmd,
		Port:           port,
	}, nil
}

// detectDenoSite returns a NodeSiteInfo if dir contains a Deno project, nil otherwise.
func detectDenoSite(dir string) *NodeSiteInfo {
	var cfgPath string
	for _, name := range []string{"deno.json", "deno.jsonc"} {
		if fileExists(filepath.Join(dir, name)) {
			cfgPath = filepath.Join(dir, name)
			break
		}
	}
	if cfgPath == "" {
		return nil
	}

	startCmd := "deno task start"
	if data, err := os.ReadFile(cfgPath); err == nil {
		var cfg denoConfig
		if json.Unmarshal(data, &cfg) == nil {
			if _, ok := cfg.Tasks["dev"]; ok {
				startCmd = "deno task dev"
			} else if _, ok := cfg.Tasks["start"]; ok {
				startCmd = "deno task start"
			}
		}
	}

	return &NodeSiteInfo{
		Runtime:        constants.NodeRuntimeDeno,
		PackageManager: constants.NodePMDeno,
		Framework:      constants.NodeFrameworkGeneric,
		StartCmd:       startCmd,
		Port:           constants.NodeDefaultPort,
	}
}

// detectPM infers the package manager from the packageManager field and lock files.
// The packageManager field (corepack standard) takes priority over lock files.
func detectPM(dir string, pkg *packageJSON) string {
	if pkg.PackageManager != "" {
		name := strings.SplitN(pkg.PackageManager, "@", 2)[0]
		switch name {
		case constants.NodePMYarn, constants.NodePMPNPM, constants.NodePMBun:
			return name
		}
	}
	// Lock files are the most reliable signal in the wild.
	switch {
	case fileExists(filepath.Join(dir, "bun.lockb")), fileExists(filepath.Join(dir, "bun.lock")):
		return constants.NodePMBun
	case fileExists(filepath.Join(dir, "pnpm-lock.yaml")):
		return constants.NodePMPNPM
	case fileExists(filepath.Join(dir, "yarn.lock")):
		return constants.NodePMYarn
	}
	return constants.NodePMNPM
}

// runtimeForPM returns the runtime that pairs with the given package manager.
// Bun is both a runtime and a package manager; everything else uses Node.js.
func runtimeForPM(pm string) string {
	if pm == constants.NodePMBun {
		return constants.NodeRuntimeBun
	}
	return constants.NodeRuntimeNode
}

// NodeDefaults returns NodeSiteInfo with safe npm/node defaults.
func NodeDefaults() *NodeSiteInfo {
	return &NodeSiteInfo{
		Runtime:        constants.NodeRuntimeNode,
		PackageManager: constants.NodePMNPM,
		NodeVersion:    constants.NodeVersionLTS,
		Framework:      constants.NodeFrameworkGeneric,
		StartCmd:       "npm start",
		Port:           constants.NodeDefaultPort,
	}
}

// detectNodeFramework infers the framework from package.json dependencies.
func detectNodeFramework(pkg *packageJSON) string {
	deps := make(map[string]bool, len(pkg.Dependencies)+len(pkg.DevDependencies))
	for k := range pkg.Dependencies {
		deps[k] = true
	}
	for k := range pkg.DevDependencies {
		deps[k] = true
	}

	switch {
	case deps["next"]:
		return constants.NodeFrameworkNext
	case deps["nuxt"] || deps["nuxt3"]:
		return constants.NodeFrameworkNuxt
	case deps["@nestjs/core"]:
		return constants.NodeFrameworkNestJS
	case deps["express"]:
		return constants.NodeFrameworkExpress
	case deps["vite"]:
		return constants.NodeFrameworkVite
	default:
		return constants.NodeFrameworkGeneric
	}
}

// detectNodeVersion resolves the Node version from .nvmrc, .node-version, or
// the engines.node field in package.json. Falls back to LTS.
func detectNodeVersion(dir string, pkg *packageJSON) string {
	if v := readVersionFile(filepath.Join(dir, ".nvmrc")); v != "" {
		return v
	}
	if v := readVersionFile(filepath.Join(dir, ".node-version")); v != "" {
		return v
	}
	if constraint, ok := pkg.Engines["node"]; ok {
		if v := parseNodeVersionConstraint(constraint); v != "" {
			return v
		}
	}
	return constants.NodeVersionLTS
}

// readVersionFile reads .nvmrc or .node-version and returns a canonical major version.
func readVersionFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	v := strings.TrimSpace(string(data))
	v = strings.TrimPrefix(v, "v")

	// "lts/iron", "lts/*" → "lts"
	if strings.HasPrefix(strings.ToLower(v), "lts") {
		return constants.NodeVersionLTS
	}
	// "20.10.0" → "20", "20" → "20"
	if idx := strings.Index(v, "."); idx > 0 {
		return v[:idx]
	}
	return v
}

// parseNodeVersionConstraint converts a semver constraint to a major version.
func parseNodeVersionConstraint(constraint string) string {
	constraint = strings.TrimSpace(constraint)
	for _, prefix := range []string{">=", "<=", ">", "<", "^", "~"} {
		constraint = strings.TrimPrefix(constraint, prefix)
		constraint = strings.TrimSpace(constraint)
	}
	constraint = strings.TrimPrefix(constraint, "v")
	if idx := strings.IndexAny(constraint, ".x "); idx > 0 {
		return constraint[:idx]
	}
	for _, c := range constraint {
		if c < '0' || c > '9' {
			return ""
		}
	}
	return constraint
}

// detectNodePort returns the conventional default port for a given framework.
func detectNodePort(framework string) int {
	if framework == constants.NodeFrameworkVite {
		return 5173
	}
	return constants.NodeDefaultPort
}

// buildStartCmd builds the dev/start command for the given package manager and framework.
//
// Vite binds to localhost by default inside Docker, so --host is always appended.
// The flag syntax differs slightly between package managers:
//   - npm requires "-- --host" (the -- separates npm flags from script args)
//   - yarn, pnpm, bun pass extra args directly to the script
func buildStartCmd(pm, framework string, scripts map[string]string) string {
	script := detectBestScript(framework, scripts)

	hostFlag := ""
	if framework == constants.NodeFrameworkVite {
		if pm == constants.NodePMNPM {
			hostFlag = " -- --host"
		} else {
			hostFlag = " --host"
		}
	}

	switch pm {
	case constants.NodePMYarn:
		return "yarn " + script + hostFlag
	case constants.NodePMPNPM:
		return "pnpm " + script + hostFlag
	case constants.NodePMBun:
		return "bun run " + script + hostFlag
	default: // npm
		return "npm run " + script + hostFlag
	}
}

// detectBestScript picks the best script to run for local development.
// Frameworks that support HMR prefer "dev" over "start".
func detectBestScript(framework string, scripts map[string]string) string {
	if scripts == nil {
		return "start"
	}
	switch framework {
	case constants.NodeFrameworkNext, constants.NodeFrameworkNuxt, constants.NodeFrameworkVite:
		if _, ok := scripts["dev"]; ok {
			return "dev"
		}
	}
	if _, ok := scripts["start"]; ok {
		return "start"
	}
	if _, ok := scripts["dev"]; ok {
		return "dev"
	}
	return "start"
}

// nodeInstallCmd returns the dependency install command for the given package manager.
// yarn and pnpm are activated via corepack which ships with Node.js 16+.
func nodeInstallCmd(pm string) string {
	switch pm {
	case constants.NodePMYarn:
		return "corepack enable && yarn install"
	case constants.NodePMPNPM:
		return "corepack enable && pnpm install"
	case constants.NodePMBun:
		return "bun install"
	default: // npm
		return "npm install"
	}
}

// nodeDockerImage returns the Docker image for the given runtime and version.
func nodeDockerImage(info *NodeSiteInfo) string {
	switch info.Runtime {
	case constants.NodeRuntimeBun:
		return constants.BunImageAlpine
	case constants.NodeRuntimeDeno:
		return constants.DenoImageAlpine
	default:
		return NodeImageTag(info.NodeVersion)
	}
}

// nodeWrappedCommand wraps the start command with an install step so dependencies
// are always present on startup. Deno fetches its own deps at run time.
func nodeWrappedCommand(info *NodeSiteInfo) string {
	if info.Runtime == constants.NodeRuntimeDeno {
		return info.StartCmd // deno handles its own dependencies
	}
	install := nodeInstallCmd(info.PackageManager)
	return fmt.Sprintf("sh -c '%s && %s'", install, info.StartCmd)
}

// NodeImageTag returns the Docker image tag for the given Node version string.
// When version is "lts" or empty it returns "node:lts-alpine".
func NodeImageTag(version string) string {
	if version == "" || version == constants.NodeVersionLTS {
		return constants.NodeImageLTS
	}
	return fmt.Sprintf(constants.NodeImageFormat, version)
}

// =============================================================================
// Docker Compose generation (Node / Bun / Deno)
// =============================================================================

type nodeServiceConfig struct {
	ContainerName string            `yaml:"container_name"`
	Image         string            `yaml:"image"`
	Command       string            `yaml:"command"`
	WorkingDir    string            `yaml:"working_dir"`
	Volumes       []nodeVolumeConfig `yaml:"volumes"`
	Environment   map[string]string  `yaml:"environment,omitempty"`
	Labels        map[string]string  `yaml:"labels,omitempty"`
	Networks      []string           `yaml:"networks"`
	Restart       string             `yaml:"restart"`
}

type nodeVolumeConfig struct {
	Type   string `yaml:"type"`
	Source string `yaml:"source"`
	Target string `yaml:"target"`
}

type nodeNetworkConfig struct {
	Name     string `yaml:"name"`
	External bool   `yaml:"external"`
}

type nodeComposeConfig struct {
	Services map[string]nodeServiceConfig `yaml:"services"`
	Networks map[string]nodeNetworkConfig `yaml:"networks"`
}

// WriteNodeSiteConfig generates and writes docker-compose.yml for a Node/Bun/Deno site.
// If force is false, existing files are left untouched so user edits are preserved.
func WriteNodeSiteConfig(name string, meta SiteMetadata, info *NodeSiteInfo, force bool) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	siteDir := SiteConfigDir(cfg, name)
	if err := os.MkdirAll(siteDir, constants.DirPermDefault); err != nil {
		return fmt.Errorf("failed to create site config directory: %w", err)
	}

	containerName := "srv-" + name + "-node"
	image := nodeDockerImage(info)
	labels := buildNodeTraefikLabels(name, meta.Domain, meta.IsLocal, info.Port)
	cmd := nodeWrappedCommand(info)

	env := map[string]string{
		"PORT":     fmt.Sprintf("%d", info.Port),
		"NODE_ENV": "development",
	}
	// Deno manages its own binding; Node/Bun dev servers need this to listen
	// on all interfaces so Traefik can reach them inside Docker.
	if info.Runtime != constants.NodeRuntimeDeno {
		env["HOST"] = "0.0.0.0"
	}

	composeConfig := nodeComposeConfig{
		Services: map[string]nodeServiceConfig{
			"node": {
				ContainerName: containerName,
				Image:         image,
				Command:       cmd,
				WorkingDir:    constants.NodeDockerWorkDir,
				Volumes: []nodeVolumeConfig{
					{
						Type:   "bind",
						Source: meta.ProjectPath,
						Target: constants.NodeDockerWorkDir,
					},
				},
				Environment: env,
				Labels:      labels,
				Networks:    []string{constants.TraefikSubdir},
				Restart:     constants.RestartUnlessStopped,
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

	runtimeLabel := info.Runtime
	if info.PackageManager != info.Runtime && info.PackageManager != constants.NodePMDeno {
		runtimeLabel = fmt.Sprintf("%s/%s", info.Runtime, info.PackageManager)
	}

	header := fmt.Sprintf(`# Generated by srv - %s site (%s)
# Project: %s
#
# This file is yours to edit. Changes take effect on next restart.
# Run "srv site runtime %s --node-version X" to change the Node version.
#
# Common customisations:
#   environment:
#     NODE_OPTIONS: "--max-old-space-size=4096"   # Increase heap size
#     PORT: "%d"                                  # Override listen port
#     CHOKIDAR_USEPOLLING: "true"                 # Enable polling for file watching

`, runtimeLabel, info.Framework, meta.ProjectPath, name, info.Port)
	content := header + string(data)

	composePath := SiteComposePath(cfg, name)
	return writeFile(composePath, []byte(content), force)
}

// buildNodeTraefikLabels builds Traefik Docker labels for a Node.js site.
// Traefik routes directly to the app container (no nginx intermediary).
func buildNodeTraefikLabels(name, domain string, isLocal bool, port int) map[string]string {
	labels := map[string]string{
		"traefik.enable": "true",
		fmt.Sprintf("traefik.http.routers.%s.rule", name):                      fmt.Sprintf("Host(`%s`)", domain),
		fmt.Sprintf("traefik.http.routers.%s.entrypoints", name):               "websecure",
		fmt.Sprintf("traefik.http.routers.%s.tls", name):                       "true",
		fmt.Sprintf("traefik.http.services.%s.loadbalancer.server.port", name): fmt.Sprintf("%d", port),
	}
	if !isLocal {
		labels[fmt.Sprintf("traefik.http.routers.%s.tls.certresolver", name)] = "letsencrypt"
	}
	return labels
}
