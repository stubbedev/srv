# srv

A single binary CLI tool for managing containerized sites with Traefik as a reverse proxy. Supports both production domains (automatic Let's Encrypt SSL) and local development (trusted `*.test` domains via mkcert).

## Requirements

- **Docker** - Must be installed and running
- **mkcert** - Optional, for local development SSL (installed via `srv trust`)

## Installation

### Download Binary (Recommended)

Download the latest release for your platform from the [releases page](https://github.com/stubbedev/srv/releases/latest).

```bash
# Linux (amd64)
curl -Lo srv https://github.com/stubbedev/srv/releases/latest/download/srv-linux-amd64
chmod +x srv
sudo mv srv /usr/local/bin/

# Linux (arm64)
curl -Lo srv https://github.com/stubbedev/srv/releases/latest/download/srv-linux-arm64
chmod +x srv
sudo mv srv /usr/local/bin/

# macOS (Apple Silicon)
curl -Lo srv https://github.com/stubbedev/srv/releases/latest/download/srv-darwin-arm64
chmod +x srv
sudo mv srv /usr/local/bin/

# macOS (Intel)
curl -Lo srv https://github.com/stubbedev/srv/releases/latest/download/srv-darwin-amd64
chmod +x srv
sudo mv srv /usr/local/bin/
```

### Using Go Install

Requires Go 1.21+:

```bash
go install github.com/stubbedev/srv@latest
```

### Verify Installation

```bash
srv version
```

## Quick Start

```bash
# Initialize (creates network, starts Traefik and DNS)
srv init

# Set up local DNS (one-time, requires sudo)
srv dns setup

# Install local SSL certificates (one-time)
srv trust

# Add a local site
srv add /path/to/my-app --domain myapp.test --start

# Visit https://myapp.test - it just works!
```

## Commands

| Command | Description |
|---------|-------------|
| `srv init` | Create Docker network, start Traefik and DNS server |
| `srv add PATH` | Register a new site |
| `srv remove SITE` | Stop and unregister a site |
| `srv list` | List all registered sites with status |
| `srv status` | Show comprehensive status overview |
| `srv logs SITE` | View logs for a site (`-f` to follow) |
| `srv start SITE` | Start a specific site |
| `srv stop SITE` | Stop a specific site |
| `srv restart SITE` | Restart a specific site |
| `srv trust` | Install mkcert CA and generate local SSL certificates |
| `srv trust --force` | Regenerate local SSL certificates |
| `srv dns` | Show local DNS status |
| `srv dns setup` | Configure system to use local DNS |
| `srv dns remove` | Remove local DNS configuration |
| `srv update` | Pull latest Traefik image and restart |
| `srv doctor` | Diagnose common issues |

### `srv add` Options

| Flag | Description |
|------|-------------|
| `-d, --domain` | Domain/hostname (e.g., `example.com` or `myapp.test`) |
| `-l, --local` | Use local SSL for `*.test` domains |
| `-p, --port` | Container port (default: `80`) |
| `-n, --name` | Router name (default: directory name) |
| `--service` | Service name in docker-compose (auto-detected) |
| `-s, --start` | Start the site immediately after adding |
| `-f, --force` | Overwrite existing site configuration |
| `--skip-validation` | Skip Traefik config validation |

## Local DNS

srv includes a built-in DNS server that automatically resolves `*.test`, `*.local`, and `*.localhost` domains to `127.0.0.1`. No more editing `/etc/hosts`!

```bash
# Check DNS status
srv dns

# Configure your system (one-time setup, requires sudo)
srv dns setup

# Remove configuration if needed
srv dns remove
```

The `srv dns setup` command automatically detects your system's DNS resolver and configures it appropriately:

- **Linux (systemd-resolved)**: Creates `/etc/systemd/resolved.conf.d/srv-local.conf`
- **Linux (NetworkManager)**: Creates `/etc/NetworkManager/dnsmasq.d/srv-local.conf`
- **macOS**: Creates `/etc/resolver/test`, `/etc/resolver/local`, `/etc/resolver/localhost`

## Status & Diagnostics

### `srv status`

Shows a comprehensive overview of your srv setup:

```bash
srv status
# Output:
# Traefik is running
#   Dashboard: http://localhost:8080/dashboard/
#
# Sites: 3 total
#   2 running
#   1 stopped
#
# DNS is configured
#   *.test, *.local, *.localhost -> 127.0.0.1
#
# Local SSL certificates valid
#   Expires: 2025-12-15 (365 days)
```

### `srv doctor`

Runs diagnostic checks to identify common issues:

```bash
srv doctor
# Checks:
#   - Docker availability
#   - Required ports (80, 443, 8080, 53)
#   - Docker network existence
#   - Traefik container status
#   - DNS server and configuration
#   - SSL certificate validity
#   - mkcert installation
```

### `srv update`

Pulls the latest Traefik image and restarts the container:

```bash
srv update
# Pulling latest Traefik image...
# Recreating Traefik container...
# Traefik updated and restarted
```

## Configuration

All state is stored in `~/.config/srv/` (or `$XDG_CONFIG_HOME/srv/`).

You can override this with the `SRV_ROOT` environment variable:

```bash
SRV_ROOT=/opt/srv srv init
```

### Directory Structure

```
~/.config/srv/
├── env.traefik              # Let's Encrypt email
├── traefik/
│   ├── docker-compose.yml   # Traefik + DNS container config
│   ├── dnsmasq.conf         # Local DNS configuration
│   ├── conf/
│   │   ├── traefik.yml          # Static config
│   │   └── traefik-dynamic.yml  # Dynamic config (certs)
│   └── certs/
│       ├── acme.json            # Let's Encrypt certs
│       └── local/               # mkcert local certs
└── sites/
    ├── my-app -> /path/to/my-app   # Symlinks to site directories
    └── other-site -> /var/www/other
```

## How It Works

1. **Network Isolation**: Creates a unique Docker network per machine (`{hash}_traefik`) to avoid conflicts
2. **Traefik Proxy**: Runs Traefik as a reverse proxy handling SSL termination and routing
3. **Local DNS**: Runs dnsmasq to resolve local domains (*.test, *.local, *.localhost)
4. **Site Registration**: Sites are symlinked into `sites/` directory with generated Traefik labels
5. **Auto-Configuration**: All config files are generated automatically on first run

### What Gets Generated

When you run `srv add` on a site:
- `<site>/docker-compose.site.yml` - Traefik labels and network config (include in your compose)
- `<site>/env.site` - Site-specific environment variables
- `~/.config/srv/sites/<name>` - Symlink to site directory

## Site Requirements

Your site needs a `docker-compose.yml` (or `compose.yml`) with at least one service:

```yaml
services:
  app:
    image: nginx:alpine
    # ... your config
```

The `srv add` command will create a `docker-compose.site.yml` that gets included automatically, adding the necessary Traefik labels and network configuration.

## Local Development

For trusted HTTPS on `*.test` domains:

```bash
# 1. Initialize srv (starts Traefik + DNS)
srv init

# 2. Configure system DNS (one-time)
srv dns setup

# 3. Install local CA (one-time)
srv trust

# 4. Add and start your site
srv add /path/to/app --domain myapp.test --start

# 5. Visit https://myapp.test
```

Supported local TLDs: `*.test`, `*.local`, `*.localhost`

## Production

For real domains with automatic Let's Encrypt certificates:

```bash
# 1. Point DNS A record to your server's IP

# 2. Add site
srv add /path/to/app --domain example.com --start

# 3. Visit https://example.com (cert auto-provisioned)
```

On first `init`, you'll be prompted for an email address for Let's Encrypt notifications.

## Shell Completion

```bash
# Bash
source <(srv completion bash)

# Zsh
source <(srv completion zsh)

# Fish
srv completion fish | source
```

Add to your shell profile for persistence.

## Traefik Dashboard

Access at http://localhost:8080/dashboard/

Shows all routers, services, and middleware configured by your sites.

## Example Workflow

```bash
# Start fresh on a new server
srv init

# Add a production site
srv add /var/www/myapp --domain myapp.com -s

# Add a staging site  
srv add /var/www/myapp-staging --domain staging.myapp.com -s

# Check status
srv list
# NAME              DOMAIN                TYPE        STATUS
# myapp             myapp.com             production  running
# myapp-staging     staging.myapp.com     production  running

# View logs
srv logs myapp --tail 100

# Update a site (after pulling new code)
srv restart myapp

# Remove a site
srv remove myapp-staging
```

## Troubleshooting

**Site shows "broken" status in list**
- The symlink target directory no longer exists
- Remove with `srv remove <site>` and re-add

**SSL certificate not working (production)**
- Ensure DNS is pointing to the server
- Check Traefik logs: `docker logs traefik`
- Verify port 80 is accessible (required for Let's Encrypt HTTP challenge)

**Local SSL not trusted**
- Run `srv trust` to install the local CA
- Restart your browser after installing
- If still not working, try `srv trust --force` to regenerate certificates
- For Firefox: ensure mkcert CA is in Firefox's certificate store (mkcert handles this automatically)

**Local domain not resolving**
- Run `srv dns` to check DNS status
- Run `srv dns setup` to configure your system
- Ensure the srv-dns container is running: `docker ps | grep srv-dns`

**Container not accessible via Traefik**
- Ensure the site's docker-compose includes the generated `docker-compose.site.yml`
- Check that containers are on the correct network: `docker network inspect <hash>_traefik`
