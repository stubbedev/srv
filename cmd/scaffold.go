// Package cmd — scaffold.go implements `srv scaffold` which writes a
// language-specific Dockerfile + docker-compose.yml + .dockerignore into a
// project directory. srv itself doesn't read or rewrite these files
// afterwards — the user owns them. Once scaffolded, `srv add` treats the
// project as a SiteTypeDockerfile / SiteTypeCompose site.
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/stubbedev/srv/internal/ui"
)

var scaffoldFlags struct {
	lang       string
	framework  string
	version    string
	extensions string
	port       int
	dir        string
	force      bool
}

var scaffoldCmd = &cobra.Command{
	Use:   "scaffold",
	Short: "Generate a Dockerfile + docker-compose.yml for a project",
	Long: `Write a starter Dockerfile, docker-compose.yml, and .dockerignore
into the project root so srv can serve it via the normal dockerfile /
compose site path.

srv does NOT manage the resulting files — once scaffolded they live in
your repo and are yours to edit, commit, and tune. Re-running scaffold
on a project with existing files refuses unless --force is passed.

Supported languages and frameworks:

  --lang php      --framework laravel|symfony|wordpress|generic
                  --version 8.4 (default) / 8.3 / 8.2 / ...
                  --extensions redis,imagick,...
  --lang node     --framework nextjs|nuxt|vite|express|nestjs|generic
                  --version lts (default) / 22 / 20 / ...
  --lang ruby     --framework rails|sinatra|generic
                  --version 3.3 (default) / 3.2 / ...
  --lang python   --framework django|fastapi|flask|generic
                  --version 3.12 (default) / 3.11 / ...

Examples:
  srv scaffold --lang php --framework laravel
  srv scaffold --lang node --framework nextjs --version 22
  srv scaffold --lang python --framework fastapi
  srv scaffold --lang ruby --framework rails --dir ./api`,
	RunE: runScaffold,
}

func init() {
	scaffoldCmd.Flags().StringVar(&scaffoldFlags.lang, "lang", "", "Language: php / node / ruby / python (required)")
	scaffoldCmd.Flags().StringVar(&scaffoldFlags.framework, "framework", "generic", "Framework variant (see --help)")
	scaffoldCmd.Flags().StringVar(&scaffoldFlags.version, "version", "", "Language runtime version (default: language-specific)")
	scaffoldCmd.Flags().StringVar(&scaffoldFlags.extensions, "extensions", "", "PHP only: comma-separated extra extensions (e.g. 'redis,imagick')")
	scaffoldCmd.Flags().IntVar(&scaffoldFlags.port, "port", 0, "Override the framework default container port")
	scaffoldCmd.Flags().StringVar(&scaffoldFlags.dir, "dir", ".", "Project directory to scaffold into")
	scaffoldCmd.Flags().BoolVarP(&scaffoldFlags.force, "force", "f", false, "Overwrite existing Dockerfile / docker-compose.yml / .dockerignore")
	scaffoldCmd.GroupID = GroupSystem
	RootCmd.AddCommand(scaffoldCmd)
}

func runScaffold(cmd *cobra.Command, args []string) error {
	if scaffoldFlags.lang == "" {
		return ui.UsageError("srv scaffold --lang LANG --framework FW", "--lang is required")
	}

	dir, err := filepath.Abs(scaffoldFlags.dir)
	if err != nil {
		return fmt.Errorf("resolve --dir: %w", err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return fmt.Errorf("project dir %q does not exist (or is not a directory)", dir)
	}

	tpl, err := selectScaffoldTemplate(scaffoldFlags.lang, scaffoldFlags.framework)
	if err != nil {
		return err
	}
	tpl.applyOverrides(scaffoldFlags.version, scaffoldFlags.port, scaffoldFlags.extensions)

	files := tpl.render()

	if !scaffoldFlags.force {
		for name := range files {
			path := filepath.Join(dir, name)
			if _, err := os.Stat(path); err == nil {
				return fmt.Errorf("%s already exists in %s — re-run with --force to overwrite", name, dir)
			}
		}
	}

	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
		ui.Dim("  wrote %s", path)
	}

	ui.Success("Scaffolded %s/%s into %s", tpl.lang, tpl.framework, dir)
	ui.Dim("Next: `srv add %s --domain <name>.test --local`", dir)
	return nil
}

// scaffoldTemplate is one language+framework combination ready to render
// into Dockerfile / docker-compose.yml / .dockerignore content.
type scaffoldTemplate struct {
	lang        string
	framework   string
	baseImage   string // Dockerfile FROM target
	version     string // language runtime version (display only after override)
	port        int    // container port the app listens on
	docRoot     string // project-relative document root (PHP only)
	startCmd    string // CMD line for node/ruby/python; ignored for PHP
	extensions  []string // PHP only — extra extensions to install
	envLines    []string // additional ENV ... lines for the Dockerfile
}

// applyOverrides patches the template with user-supplied flag values.
func (t *scaffoldTemplate) applyOverrides(version string, port int, extensions string) {
	if version != "" {
		t.version = version
		t.baseImage = renderBaseImage(t.lang, version)
	}
	if port > 0 {
		t.port = port
	}
	if extensions != "" {
		for _, e := range strings.Split(extensions, ",") {
			if e = strings.TrimSpace(e); e != "" {
				t.extensions = append(t.extensions, e)
			}
		}
	}
}

// render returns the file-name → contents map this template emits.
func (t *scaffoldTemplate) render() map[string]string {
	return map[string]string{
		"Dockerfile":         t.renderDockerfile(),
		"docker-compose.yml": t.renderCompose(),
		".dockerignore":      t.renderDockerignore(),
	}
}

func (t *scaffoldTemplate) renderDockerfile() string {
	switch t.lang {
	case "php":
		return t.renderPHPDockerfile()
	case "node":
		return t.renderNodeDockerfile()
	case "ruby":
		return t.renderRubyDockerfile()
	case "python":
		return t.renderPythonDockerfile()
	}
	return ""
}

func (t *scaffoldTemplate) renderPHPDockerfile() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# syntax=docker/dockerfile:1.6\n# Generated by srv scaffold — yours to edit and commit.\nFROM %s\n\n", t.baseImage)
	if len(t.extensions) > 0 {
		b.WriteString("# Install PHP extensions via install-php-extensions.\n")
		b.WriteString("ADD --chmod=0755 https://github.com/mlocati/docker-php-extension-installer/releases/latest/download/install-php-extensions /usr/local/bin/\n")
		b.WriteString("RUN --mount=type=cache,target=/var/cache/apk,sharing=locked \\\n")
		b.WriteString("    --mount=type=cache,target=/tmp/ipe-build,sharing=locked \\\n")
		b.WriteString("    install-php-extensions @composer")
		for _, ext := range t.extensions {
			b.WriteString(" " + ext)
		}
		b.WriteString("\n\n")
	} else {
		b.WriteString("# Composer ships in the FrankenPHP base image; uncomment to add extensions:\n")
		b.WriteString("# ADD --chmod=0755 https://github.com/mlocati/docker-php-extension-installer/releases/latest/download/install-php-extensions /usr/local/bin/\n")
		b.WriteString("# RUN install-php-extensions redis imagick gd\n\n")
	}
	docRoot := t.docRoot
	if docRoot == "" {
		docRoot = "."
	}
	fmt.Fprintf(&b, "ENV SERVER_NAME=:%d\nENV SERVER_ROOT=%s\nENV CADDY_GLOBAL_OPTIONS=\"auto_https off\"\n\n", t.port, docRoot)
	fmt.Fprintf(&b, "WORKDIR /app\nCOPY . /app\n")
	return b.String()
}

func (t *scaffoldTemplate) renderNodeDockerfile() string {
	return fmt.Sprintf(`# syntax=docker/dockerfile:1.6
# Generated by srv scaffold — yours to edit and commit.
FROM %s

WORKDIR /app
COPY package*.json ./
RUN npm install --omit=dev || npm install
COPY . /app
ENV NODE_ENV=production
EXPOSE %d
CMD ["sh", "-c", %q]
`, t.baseImage, t.port, t.startCmd)
}

func (t *scaffoldTemplate) renderRubyDockerfile() string {
	return fmt.Sprintf(`# syntax=docker/dockerfile:1.6
# Generated by srv scaffold — yours to edit and commit.
FROM %s

RUN apk add --no-cache build-base git
WORKDIR /app
COPY Gemfile Gemfile.lock* ./
RUN bundle install
COPY . /app
EXPOSE %d
CMD ["sh", "-c", %q]
`, t.baseImage, t.port, t.startCmd)
}

func (t *scaffoldTemplate) renderPythonDockerfile() string {
	return fmt.Sprintf(`# syntax=docker/dockerfile:1.6
# Generated by srv scaffold — yours to edit and commit.
FROM %s

WORKDIR /app
COPY requirements*.txt ./
RUN pip install --no-cache-dir -r requirements.txt
COPY . /app
EXPOSE %d
CMD ["sh", "-c", %q]
`, t.baseImage, t.port, t.startCmd)
}

func (t *scaffoldTemplate) renderCompose() string {
	return fmt.Sprintf(`# Generated by srv scaffold — yours to edit and commit.
# After editing this file, run `+"`srv reload <site>`"+` to re-apply Traefik routing.
name: %s
services:
  app:
    build: .
    image: %s-local:latest
    user: "${UID:-1000}:${GID:-1000}"
    working_dir: /app
    volumes:
      - .:/app
    # Lets the container reach services on the host's loopback (e.g. a
    # MySQL bound to 127.0.0.1) by name. Use DB_HOST=host.docker.internal
    # in your .env to point app code at host services.
    extra_hosts:
      - "host.docker.internal:host-gateway"
    expose:
      - "%d"
    restart: unless-stopped
`, projectName(scaffoldFlags.dir), t.lang, t.port)
}

func (t *scaffoldTemplate) renderDockerignore() string {
	common := []string{
		".git",
		".gitignore",
		"node_modules",
		"vendor",
		"tmp",
		".env.local",
		".env.test",
		"*.log",
	}
	switch t.lang {
	case "php":
		common = append(common, "storage/logs", "storage/framework/cache/data", "bootstrap/cache")
	case "node":
		common = append(common, ".next", ".nuxt", "dist", "build", ".cache")
	case "python":
		common = append(common, "__pycache__", "*.pyc", ".venv", "venv")
	case "ruby":
		common = append(common, "log", "tmp")
	}
	return "# Generated by srv scaffold.\n" + strings.Join(common, "\n") + "\n"
}

// projectName returns a docker-compose-safe project name derived from the
// scaffold target directory's basename. Lowercased, non-alnum collapsed to '-'.
func projectName(dir string) string {
	base := filepath.Base(dir)
	if base == "." || base == "" {
		cwd, _ := os.Getwd()
		base = filepath.Base(cwd)
	}
	var b strings.Builder
	for _, r := range strings.ToLower(base) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "srv-app"
	}
	return out
}

// renderBaseImage picks the Docker base image tag for a language + version.
func renderBaseImage(lang, version string) string {
	switch lang {
	case "php":
		if version == "" || version == "latest" {
			return "dunglas/frankenphp:alpine"
		}
		return fmt.Sprintf("dunglas/frankenphp:php%s-alpine", version)
	case "node":
		if version == "" || version == "lts" {
			return "node:lts-alpine"
		}
		return fmt.Sprintf("node:%s-alpine", version)
	case "ruby":
		if version == "" || version == "latest" {
			return "ruby:alpine"
		}
		return fmt.Sprintf("ruby:%s-alpine", version)
	case "python":
		if version == "" || version == "latest" {
			return "python:alpine"
		}
		return fmt.Sprintf("python:%s-alpine", version)
	}
	return ""
}

// selectScaffoldTemplate picks the right starter template for a given
// language + framework combination. Each language has a small per-framework
// override block for docroot, default port, install/start commands.
func selectScaffoldTemplate(lang, framework string) (*scaffoldTemplate, error) {
	lang = strings.ToLower(lang)
	framework = strings.ToLower(framework)
	switch lang {
	case "php":
		return phpScaffoldTemplate(framework), nil
	case "node":
		return nodeScaffoldTemplate(framework), nil
	case "ruby":
		return rubyScaffoldTemplate(framework), nil
	case "python":
		return pythonScaffoldTemplate(framework), nil
	}
	return nil, fmt.Errorf("unknown --lang %q (supported: php / node / ruby / python)", lang)
}

func phpScaffoldTemplate(framework string) *scaffoldTemplate {
	t := &scaffoldTemplate{
		lang:      "php",
		framework: framework,
		baseImage: "dunglas/frankenphp:php8.4-alpine",
		version:   "8.4",
		port:      80,
		docRoot:   ".",
	}
	switch framework {
	case "laravel":
		t.docRoot = "public"
		t.extensions = []string{"bcmath", "intl", "pcntl", "pdo_mysql", "pdo_pgsql", "redis"}
	case "symfony":
		t.docRoot = "public"
		t.extensions = []string{"intl", "pdo_mysql", "pdo_pgsql"}
	case "wordpress":
		t.docRoot = "."
		t.extensions = []string{"gd", "imagick", "intl", "mysqli"}
	default:
		t.framework = "generic"
	}
	return t
}

func nodeScaffoldTemplate(framework string) *scaffoldTemplate {
	t := &scaffoldTemplate{
		lang:      "node",
		framework: framework,
		baseImage: "node:lts-alpine",
		version:   "lts",
		port:      3000,
		startCmd:  "npm start",
	}
	switch framework {
	case "nextjs", "next":
		t.framework = "nextjs"
		t.startCmd = "npm run build && npm run start"
	case "nuxt":
		t.startCmd = "npm run build && node .output/server/index.mjs"
	case "vite":
		t.startCmd = "npm run preview -- --host 0.0.0.0 --port 3000"
	case "express":
		t.startCmd = "node ./index.js"
	case "nestjs", "nest":
		t.framework = "nestjs"
		t.startCmd = "npm run start:prod"
	default:
		t.framework = "generic"
	}
	return t
}

func rubyScaffoldTemplate(framework string) *scaffoldTemplate {
	t := &scaffoldTemplate{
		lang:      "ruby",
		framework: framework,
		baseImage: "ruby:3.3-alpine",
		version:   "3.3",
		port:      3000,
		startCmd:  "bundle exec ruby app.rb",
	}
	switch framework {
	case "rails":
		t.startCmd = "bundle exec rails server -b 0.0.0.0 -p 3000"
	case "sinatra":
		t.startCmd = "bundle exec ruby app.rb -o 0.0.0.0 -p 3000"
	default:
		t.framework = "generic"
	}
	return t
}

func pythonScaffoldTemplate(framework string) *scaffoldTemplate {
	t := &scaffoldTemplate{
		lang:      "python",
		framework: framework,
		baseImage: "python:3.12-alpine",
		version:   "3.12",
		port:      8000,
		startCmd:  "python -u app.py",
	}
	switch framework {
	case "django":
		t.startCmd = "gunicorn --bind 0.0.0.0:8000 myproject.wsgi:application"
	case "fastapi":
		t.startCmd = "uvicorn main:app --host 0.0.0.0 --port 8000"
	case "flask":
		t.envLines = append(t.envLines, "ENV FLASK_RUN_HOST=0.0.0.0")
		t.startCmd = "flask run --host 0.0.0.0 --port 8000"
	default:
		t.framework = "generic"
	}
	return t
}
