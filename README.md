# srv

A CLI tool for managing local development sites with Traefik. Serve any directory at `https://mysite.test` with trusted SSL.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/stubbedev/srv/master/install.sh | sh
```

Or download from [releases](https://github.com/stubbedev/srv/releases/latest).

**Requirements:** Docker

## Quick Start

```bash
# Initialize srv
srv init

# Set up local DNS (one-time, requires sudo)
srv dns setup

# Install local SSL CA (one-time)
srv trust

# Serve any directory
cd ~/my-project
srv link
srv start my-project

# Visit https://my-project.test
```

## Commands

```
srv init              Initialize srv
srv link [NAME]       Link directory as site
srv unlink [NAME]     Unlink a site
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
srv unsecure [SITE]   Disable local SSL
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

1. **Link** - Creates a docker-compose config for nginx to serve your directory
2. **Start** - Spins up the container with Traefik labels
3. **Access** - Traefik routes `https://mysite.test` to your container

For directories with an existing `docker-compose.yml`, srv adds Traefik configuration without creating nginx.

## Configuration

All state stored in `~/.config/srv/`.

| Path | Description |
|------|-------------|
| `~/.config/srv/traefik/` | Traefik configuration |
| `~/.config/srv/sites/` | Site symlinks |
| `~/.config/srv/traefik/certs/local/` | Local SSL certificates |

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
