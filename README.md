# srv

A CLI tool for managing sites with Traefik reverse proxy. Works for both local development (mkcert) and production (automatic Let's Encrypt SSL).

## Install

### Via install script

```bash
curl -fsSL https://raw.githubusercontent.com/stubbedev/srv/master/install.sh | sh
```

### Via releases

Download from [releases](https://github.com/stubbedev/srv/releases/latest).

**Requirements:** Docker

## Quick Start

### Local Development

Serve any directory with trusted local SSL.

```bash
# One-time setup
srv init

# Add a site (static files or docker-compose)
srv add ~/my-project --domain mysite.test --local
srv start mysite

# Visit https://mysite.test
```

### Production

Serve sites with automatic Let's Encrypt SSL certificates.

```bash
# Initialize (prompts for Let's Encrypt email)
srv init

# Add a site with a real domain
srv add /var/www/myapp --domain example.com --start

# Visit https://example.com (cert auto-provisioned)
```

**Requirements:**
- Domain DNS pointing to your server
- Ports 80 and 443 open

## Adding Sites

The `srv add` command handles both static sites and docker-compose projects:

```bash
# Static site (no docker-compose.yml) - serves files with nginx
srv add /var/www/html --domain example.com

# Docker-compose site - routes traffic to your service
srv add /var/www/app --domain example.com

# Local development with mkcert SSL
srv add ./mysite --domain mysite.test --local

# Start immediately after adding
srv add ./mysite --domain mysite.test --local --start
```

### Static Sites

For directories without a `docker-compose.yml`, srv generates an nginx container that:

- Serves your HTML, CSS, JS, and other static files
- Blocks sensitive files (`.env`, `.git`, `.htaccess`, etc.)
- Adds security headers and gzip compression
- Supports custom `404.html` pages
- Caches static assets (configurable)
- Supports SPA routing (configurable)
- Optional CORS headers

```bash
# Basic static site
srv add ./dist --domain example.com

# Disable SPA mode (return 404 instead of falling back to index.html)
srv add ./docs --domain docs.example.com --spa=false

# Disable caching (for development)
srv add ./site --domain dev.test --local --cache=false

# Enable CORS (for API/assets accessed from other domains)
srv add ./assets --domain cdn.example.com --cors
```

All configuration is stored in `~/.config/srv/sites/{name}/` - no files are created in your project directory.

### Docker-Compose Sites

For directories with a `docker-compose.yml`, srv:

- Generates Traefik routing configuration in `~/.config/srv/`
- Connects your service to the Traefik network
- If multiple services exist, prompts you to select which one to route traffic to

No files are created in your project directory.

## Commands

```
srv init              Initialize srv
srv add PATH          Add a site (static or docker-compose)
srv remove SITE       Remove a site
srv start SITE        Start a site
srv stop SITE         Stop a site
srv restart SITE      Restart a site
srv list              List sites
srv info [SITE]       Show site details
srv logs SITE         View site logs
srv open [SITE]       Open site in browser
srv status            Show status
srv doctor            Check for issues
```

### Flags for `srv add`

| Flag | Description |
|------|-------------|
| `--domain`, `-d` | Domain name (required) |
| `--local`, `-l` | Use local SSL via mkcert (default: Let's Encrypt) |
| `--start`, `-s` | Start the site after adding |
| `--name`, `-n` | Site name (default: directory name) |
| `--port`, `-p` | Container port (default: 80) |
| `--service` | Service name for multi-service docker-compose |
| `--force`, `-f` | Overwrite existing configuration |

### Proxying

Proxy non-Docker services running on localhost:

```bash
srv proxy add --domain api.test --port 3000
srv proxy add -d myapp.test -p 8080
srv proxy list
srv proxy remove api
```

Proxies use local SSL (mkcert) and automatically register with local DNS, resolving to 127.0.0.1.

### Sharing

Share sites publicly via tunnel:

```bash
srv share mysite              # Uses cloudflared
srv share mysite --tool ngrok
```

## How It Works

- **Local (`--local`)** - Uses mkcert for trusted local SSL certificates. Domains are automatically registered with the local DNS server.
- **Production** - Uses Let's Encrypt for automatic SSL certificates
- **Traefik** - Routes requests to your containers based on domain
- **DNS** - Any domain added with `--local` or via `srv proxy add` is registered with the local dnsmasq container and resolves to 127.0.0.1. This works for any TLD (e.g., `myapp.test`, `api.local`, `dev.example.com`).

## Configuration

All configuration is stored in `~/.config/srv/` - srv never writes files to your project directories.

| Path | Description |
|------|-------------|
| `~/.config/srv/config.yml` | Global configuration |
| `~/.config/srv/traefik/` | Traefik configuration and docker-compose |
| `~/.config/srv/traefik/conf/` | Dynamic Traefik routing configs |
| `~/.config/srv/traefik/certs/` | Let's Encrypt certificates |
| `~/.config/srv/sites/` | Site symlinks and per-site config |
| `~/.config/srv/sites/{name}/metadata.yml` | Site metadata |
| `~/.config/srv/sites/{name}/certs/` | Local SSL certificates (mkcert) |
| `~/.config/srv/sites/{name}/docker-compose.yml` | Static site nginx config |

## Troubleshooting

**SSL not trusted?**

Restart your browser after adding your first local site (the mkcert CA is auto-installed).

**Something broken?**
```bash
srv doctor      # Run diagnostics
```

## License

MIT
