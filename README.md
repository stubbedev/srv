# srv

A CLI tool for managing sites with Traefik reverse proxy. Works for both local development (`*.test` with mkcert) and production (automatic Let's Encrypt SSL).

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/stubbedev/srv/master/install.sh | sh
```

Or download from [releases](https://github.com/stubbedev/srv/releases/latest).

**Requirements:** Docker

## Local Development

Serve any directory at `https://mysite.test` with trusted local SSL.

```bash
# One-time setup
srv init
srv dns setup    # Configure local DNS (requires sudo)
srv trust        # Install local CA

# Serve a directory
cd ~/my-project
srv link
srv start my-project

# Visit https://my-project.test
```

## Production

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

## Commands

```
srv init              Initialize srv
srv add PATH          Add a site with docker-compose
srv link [NAME]       Link directory as static site
srv remove SITE       Remove a site
srv start SITE        Start a site
srv stop SITE         Stop a site
srv restart SITE      Restart a site
srv list              List sites
srv logs SITE         View site logs
srv open [SITE]       Open site in browser
srv status            Show status
srv doctor            Check for issues
```

### DNS & SSL

```
srv dns               Show DNS status
srv dns setup         Configure system DNS
srv dns remove        Remove DNS configuration
srv trust             Install local CA
srv secure [SITE]     Enable local SSL
srv unsecure [SITE]   Use Let's Encrypt instead
```

### Proxying

Proxy non-Docker services:

```bash
srv proxy add api http://127.0.0.1:3000
srv proxy list
srv proxy remove api
```

### Sharing

Share sites publicly via tunnel:

```bash
srv share mysite              # Uses cloudflared
srv share mysite --tool ngrok
```

## How It Works

- **Local (`*.test`)** - Uses mkcert for trusted local SSL certificates
- **Production** - Uses Let's Encrypt for automatic SSL certificates
- **Traefik** - Routes requests to your containers based on domain

For static directories, srv generates an nginx container. For directories with `docker-compose.yml`, srv adds Traefik labels to your existing services.

## Configuration

All state stored in `~/.config/srv/`.

| Path | Description |
|------|-------------|
| `~/.config/srv/traefik/` | Traefik configuration |
| `~/.config/srv/sites/` | Site symlinks |
| `~/.config/srv/traefik/certs/` | SSL certificates |

## Troubleshooting

**Domain not resolving?**
```bash
srv dns setup   # Configure system DNS
```

**SSL not trusted?**
```bash
srv trust       # Install local CA
```

**Something broken?**
```bash
srv doctor      # Run diagnostics
```

## License

MIT
