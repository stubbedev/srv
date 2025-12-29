# srv

A CLI tool for managing local development and production sites with Traefik reverse proxy. Supports trusted local SSL via mkcert and automatic Let's Encrypt certificates for production.

## Installation

### Via Install Script

```bash
curl -fsSL https://raw.githubusercontent.com/stubbedev/srv/master/install.sh | sh
```

### Via Releases

Download the binary for your platform from [releases](https://github.com/stubbedev/srv/releases/latest).

**Supported platforms:** Linux (amd64, arm64, armv7, 386), macOS (amd64, arm64), FreeBSD

**Requirements:** Docker

## Quick Start

### Local Development

```bash
# One-time setup
srv init

# Add a site (auto-detects static or docker-compose)
srv add ~/my-project --domain mysite.test --local

# Visit https://mysite.test
```

### Production

```bash
# Initialize (prompts for Let's Encrypt email)
srv init

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
| `srv add PATH` | Add a new site (static or docker-compose) |
| `srv remove SITE` | Remove a site and stop its containers |
| `srv start SITE` | Start a site's containers |
| `srv stop SITE` | Stop a site's containers |
| `srv restart SITE` | Restart a site's containers |
| `srv list` | List all registered sites |
| `srv info SITE` | Show detailed site information |
| `srv logs SITE` | View site container logs |
| `srv open SITE` | Open site in browser |
| `srv share SITE` | Share site via tunnel (cloudflared/ngrok) |

### Proxy Management

| Command | Description |
|---------|-------------|
| `srv proxy add` | Create a proxy to localhost port or container |
| `srv proxy remove NAME` | Remove a proxy |
| `srv proxy list` | List all proxies |
| `srv proxy share NAME` | Share a proxy via tunnel |

### System Commands

| Command | Description |
|---------|-------------|
| `srv init` | Initialize srv environment |
| `srv doctor` | Run diagnostic checks |
| `srv update` | Update Traefik and DNS images |
| `srv paths` | Show configuration paths |
| `srv version` | Show version information |

## Command Reference

### `srv add`

Register a new site with Traefik. Automatically detects whether the path contains a `docker-compose.yml` (compose site) or static files.

```bash
srv add PATH [flags]
```

#### Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--domain` | `-d` | | Domain name (required) |
| `--local` | `-l` | `false` | Use local SSL via mkcert (otherwise Let's Encrypt) |
| `--name` | `-n` | directory name | Custom site name |
| `--port` | `-p` | `80` | Container port to route traffic to |
| `--service` | | | Service name for multi-service docker-compose |
| `--force` | `-f` | `false` | Overwrite existing configuration |
| `--spa` | | `true` | Enable SPA mode (fallback to index.html) |
| `--cache` | | `true` | Enable caching headers for static assets |
| `--cors` | | `false` | Enable CORS headers (allow all origins) |
| `--skip-validation` | | `false` | Skip compose file validation |

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

### `srv share`

Share a site publicly via tunnel service.

```bash
srv share SITE [flags]
```

| Flag | Description |
|------|-------------|
| `--tool` | Tunnel tool to use (`cloudflared`, `ngrok`) |

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

### `srv init`

Initialize the srv environment: creates Docker network, generates Traefik configuration, and starts containers.

```bash
srv init [flags]
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

## Site Types

### Static Sites

For directories without a `docker-compose.yml`, srv generates an nginx container that:

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

# List all proxies
srv proxy list

# Remove a proxy
srv proxy remove api.test
```

All proxies use local SSL (mkcert) and automatically register with the local DNS server.

## Sharing Sites

Share local sites publicly using tunnel services:

```bash
# Auto-detect tunnel tool (prefers cloudflared)
srv share mysite

# Use specific tool
srv share mysite --tool ngrok

# Share a proxy
srv proxy share api.test
```

**Supported tools:**
- [cloudflared](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/) (recommended)
- [ngrok](https://ngrok.com/)

## How It Works

- **Local SSL (`--local`)**: Uses [mkcert](https://github.com/FiloSottile/mkcert) for trusted local certificates. Domains are automatically registered with the built-in DNS server (dnsmasq).

- **Production SSL**: Uses Let's Encrypt for automatic certificate provisioning and renewal.

- **Traefik**: Routes requests to containers based on domain rules. Configuration is generated automatically.

- **DNS**: Local domains (added with `--local` or via `srv proxy add`) are registered with a dnsmasq container and resolve to `127.0.0.1`. Works with any TLD (`.test`, `.local`, `.dev`, etc.).

## Configuration

All configuration is stored in `~/.config/srv/` - srv never writes files to your project directories.

| Path | Description |
|------|-------------|
| `~/.config/srv/config.yml` | Global configuration (email, network name) |
| `~/.config/srv/traefik/` | Traefik docker-compose and static config |
| `~/.config/srv/traefik/conf/` | Dynamic Traefik routing configs |
| `~/.config/srv/traefik/certs/` | Let's Encrypt certificates (acme.json) |
| `~/.config/srv/sites/` | Site configurations |
| `~/.config/srv/sites/{name}/metadata.yml` | Site metadata |
| `~/.config/srv/sites/{name}/certs/` | Local SSL certificates (mkcert) |
| `~/.config/srv/sites/{name}/docker-compose.yml` | Generated compose for static sites |

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
srv init
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
srv init --fresh
```

## License

MIT
