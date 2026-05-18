package site

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/srv/internal/constants"
)

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRuntimeForPM(t *testing.T) {
	tests := map[string]string{
		constants.NodePMBun:  constants.NodeRuntimeBun,
		constants.NodePMNPM:  constants.NodeRuntimeNode,
		constants.NodePMYarn: constants.NodeRuntimeNode,
		constants.NodePMPNPM: constants.NodeRuntimeNode,
		"unknown":            constants.NodeRuntimeNode,
	}
	for pm, want := range tests {
		if got := runtimeForPM(pm); got != want {
			t.Errorf("runtimeForPM(%q) = %q, want %q", pm, got, want)
		}
	}
}

func TestDetectPMFromPackageManagerField(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{
		"yarn@4.0.0":  constants.NodePMYarn,
		"pnpm@9.0.0":  constants.NodePMPNPM,
		"bun@1.0.0":   constants.NodePMBun,
		"weird@1.0.0": constants.NodePMNPM, // falls through to default
	}
	for input, want := range cases {
		pkg := &packageJSON{PackageManager: input}
		if got := detectPM(dir, pkg); got != want {
			t.Errorf("packageManager=%q -> %q, want %q", input, got, want)
		}
	}
}

func TestDetectPMFromLockFiles(t *testing.T) {
	cases := []struct {
		lock string
		want string
	}{
		{"bun.lockb", constants.NodePMBun},
		{"bun.lock", constants.NodePMBun},
		{"pnpm-lock.yaml", constants.NodePMPNPM},
		{"yarn.lock", constants.NodePMYarn},
	}
	for _, c := range cases {
		dir := t.TempDir()
		writeFiles(t, dir, map[string]string{c.lock: ""})
		if got := detectPM(dir, &packageJSON{}); got != c.want {
			t.Errorf("%s -> %q, want %q", c.lock, got, c.want)
		}
	}
}

func TestDetectPMDefaultNPM(t *testing.T) {
	dir := t.TempDir()
	if got := detectPM(dir, &packageJSON{}); got != constants.NodePMNPM {
		t.Errorf("default -> %q, want npm", got)
	}
}

func TestNodeDefaults(t *testing.T) {
	d := NodeDefaults()
	if d.Runtime != constants.NodeRuntimeNode || d.PackageManager != constants.NodePMNPM {
		t.Errorf("defaults wrong: %+v", d)
	}
}

func TestDetectNodeFramework(t *testing.T) {
	cases := []struct {
		name string
		pkg  packageJSON
		want string
	}{
		{"next-in-deps", packageJSON{Dependencies: map[string]string{"next": "x"}}, constants.NodeFrameworkNext},
		{"nuxt", packageJSON{Dependencies: map[string]string{"nuxt": "x"}}, constants.NodeFrameworkNuxt},
		{"nuxt3", packageJSON{Dependencies: map[string]string{"nuxt3": "x"}}, constants.NodeFrameworkNuxt},
		{"nestjs", packageJSON{Dependencies: map[string]string{"@nestjs/core": "x"}}, constants.NodeFrameworkNestJS},
		{"express", packageJSON{Dependencies: map[string]string{"express": "x"}}, constants.NodeFrameworkExpress},
		{"vite", packageJSON{DevDependencies: map[string]string{"vite": "x"}}, constants.NodeFrameworkVite},
		{"generic", packageJSON{Dependencies: map[string]string{"lodash": "x"}}, constants.NodeFrameworkGeneric},
		{"empty", packageJSON{}, constants.NodeFrameworkGeneric},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := detectNodeFramework(&c.pkg); got != c.want {
				t.Errorf("%s -> %q, want %q", c.name, got, c.want)
			}
		})
	}
}

func TestReadVersionFile(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		content string
		want    string
	}{
		{"20.10.0\n", "20"},
		{"v20.10.0", "20"},
		{"20", "20"},
		{"lts/iron\n", constants.NodeVersionLTS},
		{"LTS/*\n", constants.NodeVersionLTS},
		{"", ""},
	}
	for i, c := range cases {
		path := filepath.Join(dir, ".nvmrc")
		if err := os.WriteFile(path, []byte(c.content), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := readVersionFile(path); got != c.want {
			t.Errorf("case %d (%q) -> %q, want %q", i, c.content, got, c.want)
		}
	}
}

func TestReadVersionFileMissing(t *testing.T) {
	if got := readVersionFile("/no/such/path/.nvmrc"); got != "" {
		t.Errorf("missing file -> %q, want empty", got)
	}
}

func TestParseNodeVersionConstraint(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{">=18.0.0", "18"},
		{"^20.10", "20"},
		{"~16", "16"},
		{"v22", "22"},
		{"18.x", "18"},
		{"18 || 20", "18"},
		{"latest", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := parseNodeVersionConstraint(c.in); got != c.want {
			t.Errorf("parseNodeVersionConstraint(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDetectNodePort(t *testing.T) {
	if detectNodePort(constants.NodeFrameworkVite) != 5173 {
		t.Error("vite default port wrong")
	}
	if detectNodePort(constants.NodeFrameworkNext) != constants.NodeDefaultPort {
		t.Error("next default port wrong")
	}
}

func TestDetectBestScript(t *testing.T) {
	cases := []struct {
		framework string
		scripts   map[string]string
		want      string
	}{
		{constants.NodeFrameworkNext, map[string]string{"dev": "x"}, "dev"},
		{constants.NodeFrameworkVite, map[string]string{"dev": "x", "start": "y"}, "dev"},
		{constants.NodeFrameworkExpress, map[string]string{"start": "x"}, "start"},
		{constants.NodeFrameworkGeneric, map[string]string{"dev": "x"}, "dev"},
		{constants.NodeFrameworkExpress, map[string]string{}, "start"},
		{constants.NodeFrameworkExpress, nil, "start"},
	}
	for _, c := range cases {
		if got := detectBestScript(c.framework, c.scripts); got != c.want {
			t.Errorf("detectBestScript(%q, %v) = %q, want %q", c.framework, c.scripts, got, c.want)
		}
	}
}

func TestBuildStartCmdVite(t *testing.T) {
	cases := map[string]string{
		constants.NodePMNPM:  "npm run dev -- --host",
		constants.NodePMYarn: "yarn dev --host",
		constants.NodePMPNPM: "pnpm dev --host",
		constants.NodePMBun:  "bun run dev --host",
	}
	for pm, want := range cases {
		got := buildStartCmd(pm, constants.NodeFrameworkVite, map[string]string{"dev": "vite"})
		if got != want {
			t.Errorf("buildStartCmd(%q,vite) = %q, want %q", pm, got, want)
		}
	}
}

func TestBuildStartCmdNonVite(t *testing.T) {
	cases := map[string]string{
		constants.NodePMNPM:  "npm run start",
		constants.NodePMYarn: "yarn start",
		constants.NodePMPNPM: "pnpm start",
		constants.NodePMBun:  "bun run start",
	}
	for pm, want := range cases {
		got := buildStartCmd(pm, constants.NodeFrameworkExpress, map[string]string{"start": "node ."})
		if got != want {
			t.Errorf("buildStartCmd(%q,express) = %q, want %q", pm, got, want)
		}
	}
}

func TestNodeInstallCmd(t *testing.T) {
	cases := map[string]string{
		constants.NodePMYarn: "corepack enable && yarn install",
		constants.NodePMPNPM: "corepack enable && pnpm install",
		constants.NodePMBun:  "bun install",
		constants.NodePMNPM:  "npm install",
		"unknown":            "npm install",
	}
	for pm, want := range cases {
		if got := nodeInstallCmd(pm); got != want {
			t.Errorf("nodeInstallCmd(%q) = %q, want %q", pm, got, want)
		}
	}
}

func TestNodeDockerImage(t *testing.T) {
	cases := []struct {
		info *NodeSiteInfo
		want string
	}{
		{&NodeSiteInfo{Runtime: constants.NodeRuntimeBun}, constants.BunImageAlpine},
		{&NodeSiteInfo{Runtime: constants.NodeRuntimeDeno}, constants.DenoImageAlpine},
		{&NodeSiteInfo{Runtime: constants.NodeRuntimeNode, NodeVersion: "20"}, "node:20-alpine"},
		{&NodeSiteInfo{Runtime: constants.NodeRuntimeNode, NodeVersion: constants.NodeVersionLTS}, constants.NodeImageLTS},
	}
	for _, c := range cases {
		if got := nodeDockerImage(c.info); got != c.want {
			t.Errorf("nodeDockerImage(%+v) = %q, want %q", c.info, got, c.want)
		}
	}
}

func TestNodeImageTag(t *testing.T) {
	if got := NodeImageTag(""); got != constants.NodeImageLTS {
		t.Errorf("empty -> %q", got)
	}
	if got := NodeImageTag(constants.NodeVersionLTS); got != constants.NodeImageLTS {
		t.Errorf("lts -> %q", got)
	}
	if got := NodeImageTag("18"); got != "node:18-alpine" {
		t.Errorf("18 -> %q", got)
	}
}

func TestNodeWrappedCommand(t *testing.T) {
	deno := &NodeSiteInfo{Runtime: constants.NodeRuntimeDeno, StartCmd: "deno task start"}
	if got := nodeWrappedCommand(deno); got != "deno task start" {
		t.Errorf("deno wrap = %q", got)
	}
	npm := &NodeSiteInfo{Runtime: constants.NodeRuntimeNode, PackageManager: constants.NodePMNPM, StartCmd: "npm run start"}
	got := nodeWrappedCommand(npm)
	if got != "sh -c 'npm install && npm run start'" {
		t.Errorf("npm wrap = %q", got)
	}
}

func TestDetectNodeSiteMissing(t *testing.T) {
	dir := t.TempDir()
	info, err := DetectNodeSite(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info != nil {
		t.Errorf("expected nil for empty dir, got %+v", info)
	}
}

func TestDetectNodeSiteFromPackageJSON(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"package.json": `{"scripts":{"dev":"next dev"},"dependencies":{"next":"14"}}`,
	})
	info, err := DetectNodeSite(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info == nil {
		t.Fatal("expected detected info")
	}
	if info.Framework != constants.NodeFrameworkNext {
		t.Errorf("Framework = %q, want next", info.Framework)
	}
	if info.PackageManager != constants.NodePMNPM {
		t.Errorf("PM = %q, want npm", info.PackageManager)
	}
	if info.StartCmd != "npm run dev" {
		t.Errorf("StartCmd = %q", info.StartCmd)
	}
}

func TestDetectNodeSiteMalformedJSONUsesDefaults(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"package.json": `{not valid json`})
	info, err := DetectNodeSite(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info == nil {
		t.Fatal("expected defaults, not nil")
	}
	if info.Runtime != constants.NodeRuntimeNode || info.PackageManager != constants.NodePMNPM {
		t.Errorf("defaults wrong: %+v", info)
	}
}

func TestDetectDenoSite(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"deno.json": `{"tasks":{"dev":"deno run main.ts"}}`,
	})
	info, err := DetectNodeSite(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info == nil {
		t.Fatal("expected deno detection")
	}
	if info.Runtime != constants.NodeRuntimeDeno {
		t.Errorf("Runtime = %q", info.Runtime)
	}
	if info.StartCmd != "deno task dev" {
		t.Errorf("StartCmd = %q", info.StartCmd)
	}
}

func TestDetectDenoSiteJsonc(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"deno.jsonc": `{"tasks":{"start":"deno run main.ts"}}`,
	})
	info, err := DetectNodeSite(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info == nil || info.Runtime != constants.NodeRuntimeDeno {
		t.Fatalf("deno.jsonc not detected: %+v", info)
	}
	if info.StartCmd != "deno task start" {
		t.Errorf("StartCmd = %q", info.StartCmd)
	}
}

func TestDetectNodeVersionFromNvmrc(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{".nvmrc": "20"})
	got := detectNodeVersion(dir, &packageJSON{})
	if got != "20" {
		t.Errorf("got %q, want 20", got)
	}
}

func TestDetectNodeVersionFromEngines(t *testing.T) {
	dir := t.TempDir()
	pkg := &packageJSON{Engines: map[string]string{"node": ">=20.5.0"}}
	if got := detectNodeVersion(dir, pkg); got != "20" {
		t.Errorf("got %q, want 20", got)
	}
}

func TestDetectNodeVersionFallback(t *testing.T) {
	dir := t.TempDir()
	if got := detectNodeVersion(dir, &packageJSON{}); got != constants.NodeVersionLTS {
		t.Errorf("got %q, want lts", got)
	}
}

func TestDetectNodeVersionFromNodeVersionFile(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{".node-version": "18.5.0"})
	if got := detectNodeVersion(dir, &packageJSON{}); got != "18" {
		t.Errorf("got %q", got)
	}
}

func TestBuildNodeTraefikLabelsLocal(t *testing.T) {
	labels := buildNodeTraefikLabels("blog", []string{"blog.local"}, true, false, 3000)
	if labels["traefik.enable"] != "true" {
		t.Error("traefik.enable missing")
	}
	if _, ok := labels["traefik.http.routers.blog.tls.certresolver"]; ok {
		t.Error("certresolver should not be set for local site")
	}
	if labels["traefik.http.services.blog.loadbalancer.server.port"] != "3000" {
		t.Errorf("port label wrong: %q", labels["traefik.http.services.blog.loadbalancer.server.port"])
	}
}

func TestBuildNodeTraefikLabelsRemote(t *testing.T) {
	labels := buildNodeTraefikLabels("blog", []string{"blog.com"}, false, false, 3000)
	if labels["traefik.http.routers.blog.tls.certresolver"] != "letsencrypt" {
		t.Error("certresolver should be letsencrypt for non-local site")
	}
}
