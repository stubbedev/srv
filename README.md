# srv

A CLI tool for managing local development and production sites with Traefik reverse proxy. Supports trusted local SSL via mkcert and automatic Let's Encrypt certificates for production.

## Installation

### Via Install Script

```bash
curl -fsSL https://raw.githubusercontent.com/stubbedev/srv/master/install.sh | sh
```

### Via Releases

Download the binary for your platform from [releases](https://github.com/stubbedev/srv/releases/latest).

**Supported platforms:** Linux (amd64, arm64, armv7, 386), macOS (amd64, arm64)

**Requirements:** Docker

## Quick Start

### Local Development

```bash
# One-time setup
srv install

# Add a site (auto-detects static, PHP, or docker-compose)
srv add ~/my-project --domain mysite.test --local

# Visit https://mysite.test
```

### Production

```bash
# Install (prompts for Let's Encrypt email)
srv install

# Add a site with a real domain
srv add /var/www/myapp --domain example.com

# Visit https://example.com (cert auto-provisioned)
```

**Production requirements:**
- Domain DNS pointing to your server
- Ports 80 and 443 open

## Commands

### Site Management

| Command | Description |
|---------|-------------|
| `srv add PATH` | Add a new site (static, PHP, or docker-compose) |
| `srv remove SITE` | Remove a site and stop its containers |
| `srv start SITE` | Start a site's containers |
| `srv stop SITE` | Stop a site's containers |
| `srv restart SITE` | Restart a site's containers |
| `srv reload SITE` | Re-apply metadata.yml without restarting (`--restart` to also restart) |
| `srv validate SITE` | Validate a site's metadata.yml without applying |
| `srv list` | List all registered sites |
| `srv info SITE` | Show detailed site information |
| `srv logs SITE` | View site container logs (`--all` to multiplex every site) |
| `srv shell SITE` | Open an interactive shell in the site's container |
| `srv alias add\|remove\|list SITE` | Manage extra hostnames mapped to the same site |
| `srv internal enable\|disable\|list SITE` | Toggle the plain-HTTP `:88` listener for a site |
| `srv route add\|remove\|list SITE` | Attach path-prefix / regex-rewrite routers to a site |

### Proxy Management

| Command | Description |
|---------|-------------|
| `srv proxy add` | Create a proxy to localhost port or container (`--fallback` for 5xx remote failover) |
| `srv proxy remove NAME` | Remove a proxy |
| `srv proxy list` | List all proxies |

### Import

| Command | Description |
|---------|-------------|
| `srv import valet` | Translate `~/.config/valet/Nginx/*` (or `~/.valet/Nginx/*`) into srv commands (`--apply` to execute) |

### Daemon Management

| Command | Description |
|---------|-------------|
| `srv daemon start` | Start the srv daemon |
| `srv daemon stop` | Stop the srv daemon |
| `srv daemon restart` | Restart the srv daemon |
| `srv daemon status` | Show daemon status |
| `srv daemon logs` | Show daemon logs |
| `srv daemon install` | Install daemon as a system service |
| `srv daemon uninstall` | Uninstall daemon system service |

### System Commands

| Command | Description |
|---------|-------------|
| `srv install` | Install srv environment |
| `srv doctor` | Run diagnostic checks |
| `srv update` | Update Traefik and DNS images |
| `srv paths` | Show configuration paths |
| `srv version` | Show version information |
| `srv uninstall` | Completely remove srv from the system |
| `srv completion` | Generate shell autocompletion script |
| `srv metrics enable|disable|status` | Opt-in Prometheus + Grafana stack scraping Traefik |

## Command Reference

### `srv add`

Register a new site with Traefik. Automatically detects the site type:

1. **Docker-Compose** - if the path contains a `docker-compose.yml`
2. **PHP** - if the path contains a `composer.json` or `.php`/`.phtml` files
3. **Static** - otherwise, serves the directory as static files via nginx

The site is automatically started after being added.

```bash
srv add PATH [flags]
```

#### Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--domain` | `-d` | | Canonical hostname (required) |
| `--alias` | | | Extra hostname mapped to the same site (repeatable) |
| `--wildcard` | | `false` | Also match one-level subdomains (`*.foo.test`); local sites only |
| `--internal-http` | | `false` | Also expose on the plain-HTTP `:88` listener (for in-cluster calls that skip TLS) |
| `--local` | `-l` | `false` | Use local SSL via mkcert (otherwise Let's Encrypt) |
| `--name` | `-n` | domain name | Custom site name |
| `--port` | `-p` | `80` | Container port to route traffic to |
| `--service` | | | Service name for multi-service docker-compose |
| `--force` | `-f` | `false` | Overwrite existing configuration |
| `--spa` | | `true` | Enable SPA mode (fallback to index.html) |
| `--cache` | | `true` | Enable caching headers for static assets |
| `--cors` | | `false` | Enable CORS headers (allow all origins) |
| `--skip-validation` | | `false` | Skip compose file validation |
| `--max-body` | | `2G` | Max request body size (e.g. `128M`, `2G`) |
| `--read-timeout` | | | Upstream read timeout (e.g. `300s`) |
| `--send-timeout` | | | Upstream send timeout |
| `--connect-timeout` | | | Upstream connect timeout |
| `--php-version` | | auto-detected | PHP version (e.g., `8.3`, or `latest`) |
| `--document-root` | | auto-detected | Document root relative to project |
| `--php-extensions` | | auto-detected | PHP extensions: full list, or `+ext,-ext` to add/remove from defaults |

#### Examples

```bash
# Production site with Let's Encrypt SSL
srv add /var/www/myapp --domain example.com

# Local development with mkcert SSL
srv add ./mysite --domain mysite.test --local

# Static site with custom options
srv add ./dist --domain docs.test --local --spa=false

# Docker-compose site with specific service and port
srv add ./app --domain api.test --local --service backend --port 3000

# Overwrite existing site configuration
srv add ./site --domain site.test --local --force

# PHP site (auto-detected from composer.json or .php files)
srv add ./laravel-app --domain app.test --local

# PHP site with explicit version and extra extensions
srv add ./myapp --domain myapp.test --local --php-version 8.3 --php-extensions "+redis,-calendar"
```

### `srv start`

Start a site's containers.

```bash
srv start SITE [flags]
```

| Flag | Short | Description |
|------|-------|-------------|
| `--all` | `-a` | Start all registered sites |

### `srv stop`

Stop a site's containers.

```bash
srv stop SITE [flags]
```

| Flag | Short | Description |
|------|-------|-------------|
| `--all` | `-a` | Stop all registered sites |

### `srv restart`

Restart a site's containers.

```bash
srv restart SITE [flags]
```

| Flag | Short | Description |
|------|-------|-------------|
| `--all` | `-a` | Restart all registered sites |

### `srv logs`

View container logs for a site.

```bash
srv logs SITE [flags]
```

| Flag | Short | Description |
|------|-------|-------------|
| `--follow` | `-f` | Follow log output |
| `--tail` | | Number of lines to show from the end |
| `--since` | | Show logs since timestamp (e.g., `10m`, `1h`) |

### `srv proxy add`

Create a proxy from a local domain to a localhost port or Docker container.

```bash
srv proxy add [flags]
```

| Flag | Short | Description |
|------|-------|-------------|
| `--domain` | `-d` | Domain name (required) |
| `--port` | `-p` | Localhost port to proxy to |
| `--container` | `-c` | Docker container to proxy to (`container:port`) |
| `--name` | `-n` | Proxy name (default: derived from domain) |
| `--force` | `-f` | Overwrite existing proxy configuration |

#### Examples

```bash
# Proxy to a local development server
srv proxy add --domain api.test --port 3000

# Proxy to a Docker container
srv proxy add --domain db.test --container postgres:5432

# Short form
srv proxy add -d myapp.test -p 8080
```

### `srv install`

Install the srv environment: creates Docker network, generates Traefik configuration, and starts containers.

```bash
srv install [flags]
```

| Flag | Description |
|------|-------------|
| `--fresh` | Remove existing configuration and start fresh |

### `srv doctor`

Run diagnostic checks to identify common issues.

**Checks performed:**
- Docker availability and status
- Firewall configuration (ports 80, 443)
- Port availability (80, 443, 8080, 53)
- Docker network existence
- Traefik container status
- DNS server status
- Local SSL certificates (mkcert)

### `srv daemon`

The srv daemon watches for Docker container start events and automatically connects registered site containers to the srv network. This ensures containers are properly connected even when started outside of srv (e.g., via `docker compose up` directly).

```bash
srv daemon start      # Start the daemon
srv daemon stop       # Stop the daemon
srv daemon restart    # Restart the daemon
srv daemon status     # Check daemon status
srv daemon logs       # View daemon logs
srv daemon install    # Install as system service (starts on boot)
srv daemon uninstall  # Remove system service
```

#### `srv daemon start`

| Flag | Short | Description |
|------|-------|-------------|
| `--foreground` | `-f` | Run in foreground (useful for debugging) |

#### `srv daemon logs`

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--follow` | `-f` | `false` | Follow log output |
| `--tail` | `-n` | `50` | Number of lines to show |

### `srv uninstall`

Completely remove srv and all its components from the system.

```bash
srv uninstall [flags]
```

| Flag | Short | Description |
|------|-------|-------------|
| `--force` | `-f` | Skip confirmation prompt |

**This will:**
1. Stop and remove the Traefik container
2. Stop and remove the DNS container
3. Remove system DNS configuration
4. Remove the daemon service
5. Remove the Docker network
6. Remove the config directory (`~/.config/srv`)
7. Remove the srv binary

**Note:** Site directories and their contents are NOT removed.

### `srv completion`

Generate shell autocompletion scripts.

```bash
# Bash
source <(srv completion bash)

# Zsh
source <(srv completion zsh)

# Fish
srv completion fish | source

# PowerShell
srv completion powershell | Out-String | Invoke-Expression
```

## Site Types

### Static Sites

For directories without a `docker-compose.yml` or PHP files, srv generates an nginx container that:

- Serves HTML, CSS, JS, and other static files
- Blocks sensitive files (`.env`, `.git`, `.htaccess`, etc.)
- Adds security headers and gzip compression
- Supports custom `404.html` pages
- Caches static assets (configurable)
- Supports SPA routing (configurable)
- Optional CORS headers

```bash
# Basic static site
srv add ./dist --domain example.com

# Disable SPA mode (return 404 for unknown routes)
srv add ./docs --domain docs.example.com --spa=false

# Disable caching (useful for development)
srv add ./site --domain dev.test --local --cache=false

# Enable CORS (for assets accessed from other domains)
srv add ./assets --domain cdn.example.com --cors
```

### PHP Sites

For directories containing a `composer.json` or `.php`/`.phtml` files (without a `docker-compose.yml`), srv generates an nginx web container per site plus a **shared PHP-FPM pool** that serves every site with a matching `(php_version, extension_set)` fingerprint.

What you get:

- Auto-detected PHP version from `composer.json` requirements
- Auto-detected framework (Laravel, Symfony, WordPress, or generic)
- Auto-detected document root (`public/`, `web/`, etc.)
- Required extensions extracted from `composer.json`
- Pool Dockerfile uses BuildKit cache mounts so adding/removing one extension reuses the apk cache and IPE tarball cache across sites
- `pm=ondemand` on local-mode sites so idle pools cost zero workers
- nginx config with FastCGI routing pointing at the shared pool container
- Sensitive paths blocked (`.env`, `.git`, `vendor/`, `node_modules/`)
- Security headers + gzip
- Healthcheck on the web container (busybox `nc -z`)
- macOS bind mounts get `consistency: cached` automatically

**Container layout for N sites with the same fingerprint:**
- N nginx web containers (`srv-<sitename>-web`, label `dev.srv.type=php`)
- 1 shared FPM container (`srv-fpm-<hash>`, label `dev.srv.type=php-fpm-pool`)

Each site is mounted at `/var/www/<sitename>` inside the pool; `srv shell <site>` exec's into the pool container with the working directory set to your site's mount.

```bash
# Laravel project (auto-detects framework, PHP version, and extensions)
srv add ./laravel-app --domain app.test --local

# WordPress site
srv add ./wordpress --domain blog.test --local

# Override PHP version
srv add ./myapp --domain myapp.test --local --php-version 8.3

# Override document root
srv add ./myapp --domain myapp.test --local --document-root public

# Add/remove extensions from auto-detected defaults
srv add ./myapp --domain myapp.test --local --php-extensions "+redis,-calendar"

# Replace all extensions with an explicit list
srv add ./myapp --domain myapp.test --local --php-extensions "pdo,pdo_mysql,mbstring,gd"
```

**Detection order:**
1. `docker-compose.yml` present - treated as a compose site (not PHP)
2. `composer.json` present - PHP site with full metadata extraction
3. `.php` or `.phtml` files present - PHP site with default extensions
4. Otherwise - static site

### Docker-Compose Sites

For directories with a `docker-compose.yml`, srv:

- Generates Traefik routing configuration
- Connects your service to the Traefik network
- Prompts for service selection if multiple services exist
- Auto-detects exposed ports from compose file

```bash
# Single service (auto-detected)
srv add ./app --domain myapp.test --local

# Multi-service with specific service
srv add ./app --domain api.test --local --service backend

# Custom port
srv add ./app --domain myapp.test --local --port 3000
```

## Proxying Non-Docker Services

Proxy domains to services running outside Docker (e.g., local dev servers):

```bash
# Proxy to localhost:3000
srv proxy add --domain api.test --port 3000

# Proxy to a running Docker container
srv proxy add --domain db.test --container postgres:5432

# Proxy with a 5xx fallback to a remote URL (spins up an nginx sidecar that
# re-proxies to the fallback when the primary upstream returns 5xx)
srv proxy add --domain kontainer.com --port 3001 \
  --fallback https://kontainer.com --fallback-timeout 2s

# List all proxies
srv proxy list

# Remove a proxy
srv proxy remove api.test
```

All proxies use local SSL (mkcert) and automatically register with the local DNS server.

## Multi-Domain Aliases

Run one container under many hostnames — handy for multi-tenant Laravel apps where every tenant maps to the same project:

```bash
srv add ~/git/work/kontainer \
  --domain kontainer.test \
  --alias  cms-kontainer.test \
  --alias  jira.konform.com.test \
  --local --wildcard

# Add an alias after the fact
srv alias add kontainer jira-staging.test

# Drop one
srv alias remove kontainer jira-staging.test

# Inspect
srv alias list kontainer
```

A single mkcert certificate covers every alias; all hostnames register with dnsmasq; the Traefik router OR-joins every Host rule.

## Internal Plain-HTTP Listener

Container-to-host calls often want to reach `https://kontainer.test` from another container, but the in-container client doesn't trust the mkcert CA. srv exposes a second Traefik entrypoint on `:88` that serves the same routers without TLS. Sites opt in:

```bash
# At add time
srv add ./laravel-app --domain app.test --local --internal-http

# Post-hoc
srv internal enable app.test
srv internal disable app.test
srv internal list
```

Result: `https://app.test` (port 443, mkcert TLS) and `http://app.test:88` (plain) both reach the same backend.

## Per-Site Routes

Attach additional Traefik routers to a site so different paths hit different upstreams (e.g. WebSocket on `/app`, S3 gateway on `/videos/...`):

```bash
# Path-prefix split (e.g. Laravel Reverb on port 6001)
srv route add kontainer.test --path /app --port 6001

# Regex rewrite (e.g. rewrite /videos/{token}/{rest} → /abs/videos/{token}/{rest})
srv route add kontainer.test \
  --path-regex '^/videos/([^/]+)/(.+)$' \
  --rewrite     '/abs/videos/$1/$2' \
  --port 9080 --preserve-host

# Upstream targets: localhost port, container[:port], or http(s):// URL
srv route add api.test --path /v2 --container backend-v2:3000
srv route add docs.test --path /sdk --url https://sdk.example.com

# Inspect / remove
srv route list kontainer.test
srv route remove kontainer.test app
```

Routes are persisted in the site's `metadata.yml` under `routes:` and emitted as a per-site Traefik file-provider config at `~/.config/srv/traefik/conf/routes-<name>.yml`.

## Hot-Reload on Metadata Edits

The srv daemon watches every `~/.config/srv/sites/<name>/metadata.yml` and re-applies changes within ~300ms (debounced across editor saves). Hand-edits the YAML file → certs refresh, DNS updates, routing config regenerates, `docker compose up -d` runs to pick up label changes. No restart command needed.

Manual triggers:

```bash
srv reload SITE             # re-apply one site's metadata
srv reload --all            # all sites
srv reload SITE --restart   # also force container restart
srv validate SITE           # check metadata.yml without applying
srv validate --all
```

Opt out of automatic file watching:

```bash
srv daemon start --no-watch
```

## Importing from Laravel Valet

Migrate an existing Valet rig (works against `~/.config/valet` or legacy `~/.valet`):

```bash
# Print equivalent srv commands without running them
srv import valet

# Execute them
srv import valet --apply
```

The importer:

- Reads `config.json` for parked paths
- Resolves each host's project directory by peeling hyphenated subdomain prefixes against `Sites/` symlinks and parked paths
- Folds hosts that share a project root into one `srv add --alias …` call
- Maps `proxy_pass http://localhost:N` blocks to `srv proxy add`
- Captures `--wildcard`, `--internal-http` (when a `listen 88` block is present), and `--fallback URL` (when an `error_page 5xx = @name` block re-proxies)
- Surfaces per-path location splits as `srv route add` hints
- Captures `client_max_body_size` and `fastcgi_*_timeout` as `--max-body` / `--*-timeout`

## Metrics (Prometheus + Grafana)

Opt-in observability stack scraping Traefik's existing `/metrics` endpoint:

```bash
srv metrics enable
# https://grafana.local      (admin / admin)
# https://prometheus.local
srv metrics status
srv metrics disable
```

Both UIs are routed through Traefik with mkcert-signed TLS; loopback ports are not exposed. Grafana ships with a pre-wired Prometheus datasource. Import dashboard ID 17347 for a per-router Traefik overview.

## Daemon Hot-Reload Details

`srv daemon` already watches Docker container start events to keep new containers connected to the srv network. The same daemon now also:

- Watches `~/.config/srv/sites/` recursively via fsnotify
- Debounces per-site edits over a 300ms quiet period to coalesce editor save patterns (rename + chmod + write)
- Short-circuits Reload when a SHA-256 of the metadata.yml matches the last-applied hash (kept in `<site>/.reload-state`)
- Auto-runs `docker compose up -d` after a regen so label / compose changes take effect without `srv restart`
- Surfaces validation errors per site in `srv doctor` and the daemon log

Per-site state file:

```
~/.config/srv/sites/<name>/.reload-state    # hex SHA-256 of last-applied metadata.yml
```

## How It Works

- **Local SSL (`--local`)**: Uses [mkcert](https://github.com/FiloSottile/mkcert) for trusted local certificates. Domains are automatically registered with the built-in DNS server (dnsmasq).

- **Production SSL**: Uses Let's Encrypt for automatic certificate provisioning and renewal.

- **Traefik**: Routes requests to containers based on domain rules. Configuration is generated automatically.

- **DNS**: Local domains (added with `--local` or via `srv proxy add`) are registered with a dnsmasq container and resolve to `127.0.0.1`. Works with any TLD (`.test`, `.local`, `.dev`, etc.).

## Configuration

All configuration is stored in `~/.config/srv/` - srv never writes files to your project directories.

| Path | Description |
|------|-------------|
| `~/.config/srv/config.yml` | Global configuration (parked paths) |
| `~/.config/srv/traefik/` | Traefik docker-compose and static config |
| `~/.config/srv/traefik/conf/` | Dynamic Traefik routing configs |
| `~/.config/srv/traefik/conf/site-<name>.yml` | Compose-site Traefik file-provider config |
| `~/.config/srv/traefik/conf/routes-<name>.yml` | Per-site extra routes (`srv route`) |
| `~/.config/srv/traefik/conf/proxy-<name>.yml` | Proxy file-provider config (`srv proxy`) |
| `~/.config/srv/traefik/conf/proxy-metrics.yml` | grafana.local / prometheus.local routers |
| `~/.config/srv/traefik/certs/` | Let's Encrypt certificates (acme.json) |
| `~/.config/srv/sites/` | Site configurations |
| `~/.config/srv/sites/{name}/metadata.yml` | Site metadata (canonical source of truth) |
| `~/.config/srv/sites/{name}/.reload-state` | Hash of last-applied metadata (daemon short-circuit) |
| `~/.config/srv/sites/{name}/certs/` | Local SSL certificates (mkcert) |
| `~/.config/srv/sites/{name}/docker-compose.yml` | Generated compose (nginx web for PHP, full stack for others) |
| `~/.config/srv/sites/{name}/nginx.conf` | Generated nginx config |
| `~/.config/srv/fpm/<hash>/` | Shared PHP-FPM pool (Dockerfile + compose + php.ini + php-fpm.conf) |
| `~/.config/srv/metrics/` | Prometheus + Grafana compose stack |

## Global Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--verbose` | `-v` | Enable verbose output |

## Troubleshooting

### SSL not trusted in browser?

Restart your browser after adding your first local site. The mkcert CA is auto-installed but browsers need to be restarted to recognize it.

### Site not accessible?

```bash
# Run diagnostics
srv doctor

# Check if Traefik is running
srv doctor | grep -A5 "Traefik"

# View site logs
srv logs mysite
```

### DNS not resolving?

```bash
# Check DNS server status
srv doctor | grep -A10 "DNS"

# Restart srv
srv install
```

### Port already in use?

```bash
# Check which process is using the port
srv doctor | grep -A10 "Ports"

# Or manually check
sudo lsof -i :80
sudo lsof -i :443
```

### Reset everything?

```bash
# Remove all configuration and start fresh
srv install --fresh
```

## License

MIT
