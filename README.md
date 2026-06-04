# srv

A CLI that puts Traefik + TLS in front of your sites. srv handles routing,
local certificates (via mkcert), production certificates (via Let's
Encrypt), and local DNS. It does **not** manage language runtimes — for
anything beyond static files, you bring your own `Dockerfile` or
`docker-compose.yml` and srv attaches Traefik routing on top.

What you get:
- Static sites served by nginx with sensible defaults (SPA, caching, CORS, hidden-file blocks)
- Proxies to arbitrary localhost ports or Docker containers
- HTTP and DNS-layer redirects with TLS-clean source hostnames
- Trusted local HTTPS (`*.test`, `*.local`, …) without browser warnings
- Auto-provisioned Let's Encrypt certificates for production domains
- Multi-host aliases, internal plain-HTTP listener, per-site path/regex routes

## When srv is (and isn't) worth it

srv is an **edge layer**: Traefik, mkcert/ACME, and dnsmasq, wired together with
a CLI that knows how to manage them per-site. It is not a PaaS — there is no
runtime, no buildpack, no app manager.

Worth it when you have:
- A dev box where every project should be reachable at `<name>.test` with
  browser-trusted HTTPS, the same way, with one command per project
- A server fronting multiple sites or apps where you'd otherwise hand-craft
  Traefik configs and ACME wiring per host
- A multi-tenant app served under many hostnames (one SAN cert, one router,
  many `Host` rules)
- A mix of containerised apps, static sites, plain `localhost:PORT` dev
  servers, and 301/DNS-layer redirects under a single TLS edge

Overkill when you have:
- A single project. Run FrankenPHP or Caddy with a self-signed cert directly;
  srv won't save you enough wiring to justify the install.
- An existing reverse-proxy setup you're happy with (nginx-proxy, Caddy,
  bare Traefik, Kubernetes Ingress). srv overlaps with those, it doesn't
  layer on top.

## Installation

### Via Homebrew (macOS / Linux)

```bash
brew install stubbedev/srv/srv
```

This installs the binary, pulls in `mkcert` as a dependency, and registers a
`brew services` recipe. To run the watch daemon in the background:

```bash
brew services start srv
```

Don't enable both `brew services start srv` and `srv daemon install` — they
both register supervisor units that race over the same Docker watcher.

### Via install script

```bash
curl -fsSL https://raw.githubusercontent.com/stubbedev/srv/master/install.sh | sh
```

### Via releases

Download the tarball for your platform from [releases](https://github.com/stubbedev/srv/releases/latest), then extract and place `srv` on your `PATH`.

**Supported platforms:** Linux (amd64, arm64, armv7, 386), macOS (amd64, arm64).
Brew formula covers darwin/linux amd64+arm64; armv7 and 386 are install-script
or manual-download only.

**Runtime requirements:**
- Docker
- [mkcert](https://github.com/FiloSottile/mkcert) — for local TLS. Install via `brew install mkcert`, `nix profile install nixpkgs#mkcert`, or your distro package manager. srv shells out to it; no embedded copy.

## Quick start

### Local development

```bash
# One-time setup
srv install

# Static site
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

> Full reference, auto-generated from the binary: **[docs/cli.md](docs/cli.md)**.
> The summary tables below cover the most common operations; everything below
> exists for muscle memory and quick scanning.

### Sites

| Command | Description |
|---------|-------------|
| `srv add PATH` | Add a site (static, compose, or Dockerfile — auto-detected) |
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
| `srv network attach\|detach\|list SITE` | Attach the site's container to extra Docker networks |
| `srv volume add\|remove\|list SITE` | Mount extra host directories into the site's container |

### Proxies & redirects

| Command | Description |
|---------|-------------|
| `srv proxy add` | Create a proxy to a localhost port or container (`--fallback` for 5xx remote failover) |
| `srv proxy remove NAME` | Remove a proxy |
| `srv proxy list` | List all proxies |
| `srv redirect add` | 301/302 redirect a domain to another URL (path + query preserved). `--dns-only` for a DNS-layer A-record swap with no TLS or Traefik |
| `srv redirect remove NAME` | Remove a redirect |
| `srv redirect list` | List all redirects |
| `srv redirect reload` | Re-apply every `redirect-*.yml` (re-resolves DNS-only targets, refreshes certs) |

### System

| Command | Description |
|---------|-------------|
| `srv install` | Install srv environment (Docker network, Traefik, DNS) |
| `srv doctor` | Run diagnostic checks |
| `srv update` | Update Traefik and DNS images |
| `srv paths` | Show configuration paths |
| `srv version` | Show version information |
| `srv uninstall` | Completely remove srv from the system |
| `srv completion` | Generate shell autocompletion script |
| `srv import valet` | Translate `~/.config/valet/Nginx/*` (or `~/.valet/Nginx/*`) into srv commands |
| `srv metrics enable\|disable\|status` | Opt-in Prometheus + Grafana stack scraping Traefik |
| `srv daemon start\|stop\|restart\|status\|logs\|install\|uninstall` | Manage the watch daemon |
| `srv mcp` | Start the [Model Context Protocol](#mcp-server) server on stdio so AI agents can drive srv |

## `srv add`

Register a new site with srv. The site type is auto-detected from the
project directory:

1. **Compose** — if the path contains a `docker-compose.yml`
2. **Dockerfile** — if the path contains a `Dockerfile`
3. **Static** — otherwise, the directory is served as static files via nginx

If you point srv at a project that needs a runtime (PHP, Node, Ruby,
Python, …) but doesn't carry a Dockerfile or docker-compose.yml, srv will
serve the directory as a static site. To run the app code, drop in a
Dockerfile or docker-compose.yml first.

```bash
srv add PATH [flags]
```

### Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--domain` | `-d` | | Canonical hostname (required) |
| `--alias` | | | Extra hostname mapped to the same site (repeatable) |
| `--wildcard` | | `false` | Also match one-level subdomains (`*.foo.test`); local sites only |
| `--internal-http` | | `false` | Also expose on the plain-HTTP `:88` listener (for in-cluster calls that skip TLS) |
| `--local` | `-l` | `false` | Use local SSL via mkcert (otherwise Let's Encrypt) |
| `--name` | `-n` | directory name | Custom site name |
| `--port` | `-p` | `80` | Container port to route traffic to |
| `--service` | | | Container name to route to (compose multi-service) |
| `--profile` | | | docker-compose profile (required if the chosen service declares multiple) |
| `--force` | `-f` | `false` | Overwrite existing configuration |
| `--spa` | | `true` | Static only: fall back to `/index.html` for unknown routes |
| `--cache` | | `true` | Static only: emit caching headers for static assets |
| `--cors` | | `false` | Static only: emit permissive CORS headers |
| `--volume` | | | Extra bind-mount in `HOST:CONTAINER[:ro]` form (repeatable) |
| `--type` | | auto | Force site type: `static`, `dockerfile`, or `compose` |
| `--skip-validation` | | `false` | Skip compose file validation |

### Examples

```bash
# Static site (auto-detected; no Dockerfile or compose file present)
srv add ./dist --domain docs.test --local

# Static site with SPA + CORS off
srv add ./docs --domain docs.example.com --spa=false --cors

# Dockerfile site
srv add ./my-app --domain app.test --local

# Compose site with a specific service + port
srv add ./app --domain api.test --local --service backend --port 3000

# Pre-mount host binaries (nix-profile, /nix) into the container
srv add ./laravel-app --domain app.test --local \
  --volume ~/.nix-profile:/home/$USER/.nix-profile:ro \
  --volume /nix:/nix:ro

# Force static even if a Dockerfile is present
srv add ./mixed-project --domain x.test --local --type static
```

## Static sites

For directories without a `docker-compose.yml` or `Dockerfile`, srv generates
an nginx container that:

- Serves HTML, CSS, JS, and other static files
- Blocks hidden files and sensitive extensions (`.env`, `.git`, `.htaccess`, …)
- Adds gzip compression and standard security headers
- Caches static assets (configurable)
- Supports SPA routing (configurable)
- Optional CORS headers
- Optional custom `404.html`

```bash
# Basic static site
srv add ./dist --domain example.com

# Disable SPA mode (return 404 for unknown routes)
srv add ./docs --domain docs.example.com --spa=false

# Disable caching (useful for development)
srv add ./site --domain dev.test --local --cache=false
```

## Dockerfile and compose sites (bring your own runtime)

srv does **not** generate Dockerfiles or `docker-compose.yml` files for
language runtimes — the user provides them. Any project root with a
`Dockerfile` is a dockerfile site; any project root with a
`docker-compose.yml` is a compose site. srv attaches Traefik routing and
leaves your files alone.

### Worked example: Laravel with local HTTPS

Drop this `docker-compose.yml` in your Laravel project root:

```yaml
services:
  app:
    image: dunglas/frankenphp:alpine
    working_dir: /app
    volumes:
      - .:/app
    expose:
      - "80"
    environment:
      SERVER_NAME: ":80"   # Traefik terminates TLS; container speaks plain HTTP
    extra_hosts:
      - "host.docker.internal:host-gateway"
```

Point Laravel at the public hostname in `.env`:

```env
APP_URL=https://mylaravel.test
ASSET_URL=https://mylaravel.test
TRUSTED_PROXIES=*
```

`TRUSTED_PROXIES=*` (or the equivalent in `App\Http\Middleware\TrustProxies`)
is required so Laravel respects `X-Forwarded-Proto: https` from Traefik —
otherwise it generates `http://` URLs and you'll hit mixed-content errors.

Register the site:

```bash
cd ~/projects/mylaravel
srv add . --domain mylaravel.test --local
```

srv detects the compose file, mints a mkcert cert, registers the hostname
with dnsmasq, attaches Traefik routing labels to the `app` service, and
runs `docker compose up -d`. Visit `https://mylaravel.test` — browser-trusted
TLS, no warnings.

For host-side MySQL/Redis/Mailpit listening on the host's loopback, set
`DB_HOST=host.docker.internal` (etc.) in `.env`; the `extra_hosts` entry
above wires that up. For services in another `docker compose` stack of
yours, use `srv network attach mylaravel <network_name>` and address them
by container hostname. See "[Talking to host services from inside a container](#talking-to-host-services-from-inside-a-container)"
below for the full set of options.

## Proxies (non-Docker upstreams)

```bash
# Proxy to a local dev server
srv proxy add --domain api.test --port 3000

# Proxy to a Docker container
srv proxy add --domain db.test --container postgres:5432

# Proxy with a 5xx fallback to a remote URL (spins up an nginx sidecar that
# re-proxies to the fallback when the primary upstream returns 5xx)
srv proxy add --domain myapp.com --port 3001 \
  --fallback https://myapp.com --fallback-timeout 2s

srv proxy list
srv proxy remove api.test
```

All proxies use local SSL (mkcert) and automatically register with the
local DNS server.

## Host-to-URL redirects

301 (permanent, default) or 302 (temporary) redirects. The request path
and query string are appended to the target, so
`https://jira.konform.com/browse/X?y=1` lands on
`https://jira.kontainer.com/browse/X?y=1`.

```bash
srv redirect add --domain jira.konform.com --to https://jira.kontainer.com
srv redirect add -d old.test --to https://new.test --temporary
srv redirect add -d legacy.test --to https://new.test --wildcard

srv redirect list
srv redirect remove jira-konform-com
```

A mkcert-signed certificate is provisioned for the source domain so
browsers follow the redirect without a TLS warning.

### `--dns-only` (DNS-layer redirect)

Skip mkcert and Traefik entirely. The source hostname is pinned to the
target's resolved IP via a dnsmasq `address=` record:

```bash
srv redirect add --domain jira.konform.com.test --to jira.kontainer.com --dns-only
```

The client never sees an HTTP 301 — it sends a request directly to the
target's IP with `Host: jira.konform.com.test`. Whether the user-visible
URL changes depends on what the backend does with that `Host:` header.

| | `--dns-only` | default (HTTP 301/302) |
|---|---|---|
| Emits | `address=/source/IP` in dnsmasq.conf | Traefik router + redirectRegex middleware + mkcert cert |
| Browser URL bar | depends on backend behavior | always switches to target |
| Path / query preserved | yes (browser hits target IP directly) | yes (regex replacement) |
| Works if target unreachable | no — DNS resolves but TCP fails | yes — redirect is the response |
| Re-resolve target IP | `srv redirect reload` | not needed (HTTP-layer) |
| Restrictions | `--to` must be a bare hostname; `--wildcard` and `--temporary` rejected | none |

When the target's IP changes, run `srv redirect reload` to re-resolve.

## Multi-domain aliases

Run one container under many hostnames — handy for multi-tenant apps where
every tenant maps to the same project:

```bash
srv add ~/git/work/kontainer \
  --domain kontainer.test \
  --alias  cms-kontainer.test \
  --alias  jira.konform.com.test \
  --local --wildcard

srv alias add kontainer jira-staging.test
srv alias remove kontainer jira-staging.test
srv alias list kontainer
```

A single mkcert certificate covers every alias; all hostnames register
with dnsmasq; the Traefik router OR-joins every Host rule.

## Internal plain-HTTP listener

Container-to-host calls often want to reach `https://kontainer.test` from
another container, but the in-container client doesn't trust the mkcert
CA. srv exposes a second Traefik entrypoint on `:88` that serves the same
routers without TLS:

```bash
# At add time
srv add ./my-app --domain app.test --local --internal-http

# Post-hoc
srv internal enable app.test
srv internal disable app.test
srv internal list
```

Result: `https://app.test` (port 443, mkcert TLS) and `http://app.test:88`
(plain) both reach the same backend.

## Per-site routes

Attach additional Traefik routers so different paths hit different
upstreams:

```bash
# Path-prefix split (e.g. WebSocket on /app)
srv route add kontainer.test --path /app --port 6001

# Regex rewrite
srv route add kontainer.test \
  --path-regex '^/videos/([^/]+)/(.+)$' \
  --rewrite     '/abs/videos/$1/$2' \
  --port 9080 --preserve-host

# Upstream targets: localhost port, container[:port], or http(s):// URL
srv route add api.test --path /v2 --container backend-v2:3000
srv route add docs.test --path /sdk --url https://sdk.example.com

srv route list kontainer.test
srv route remove kontainer.test app
```

Routes are persisted in the site's `metadata.yml` under `routes:` and
emitted as a per-site Traefik file-provider config at
`~/.config/srv/traefik/conf/routes-<name>.yml`.

## Talking to host services from inside a container

App code in a container has its own loopback namespace, so the usual
`DB_HOST=127.0.0.1` in your `.env` no longer points at MySQL on the
host — it points at the app container itself. srv gives you three escape
hatches.

### (a) Host services on the loopback → `host.docker.internal`

If MySQL/Redis/etc. listen on the host's `127.0.0.1`, add
`extra_hosts: ["host.docker.internal:host-gateway"]` to your
`docker-compose.yml` and rewrite each affected `.env` entry:

```env
DB_HOST=host.docker.internal
REDIS_HOST=host.docker.internal
ELASTICSEARCH_HOSTS=http://host.docker.internal:9200
```

`srv doctor` scans every container-backed site's `.env` for
`*_HOST=127.0.0.1`-style entries and warns when it finds them.

### (b) Services in your own docker-compose → `srv network attach`

If you run MySQL/Redis in another `docker compose` stack of your own, the
cleanest fix is to join that stack's network so your site container can
reach those containers by their hostname:

```bash
srv network attach my-app mysql01_default
srv network attach my-app redis01_default
srv network list   my-app
srv network detach my-app redis01_default
```

Then in `.env`:

```env
DB_HOST=mysql01
REDIS_HOST=redis01
```

Networks must already exist as external Docker networks; srv won't create
them. Run `srv restart <site>` after attaching/detaching.

### (c) Host filesystem paths / extra binaries → `srv volume add`

When your app shells out to host binaries (`ffmpeg`, `imagemagick`, …) or
writes through a host TEMP/asset path, mount whatever you need into the
container:

```bash
# Make nix-profile binaries available
srv volume add my-app ~/.nix-profile:/home/$USER/.nix-profile:ro
srv volume add my-app /nix:/nix:ro

# A shared temp directory
srv volume add my-app /tmp/uploads:/tmp/uploads

# Or pass --volume at add time
srv add ./my-app --domain app.test --local \
  --volume ~/.nix-profile:/home/$USER/.nix-profile:ro \
  --volume /nix:/nix:ro
```

`srv volume list <site>` shows current mounts; `srv volume remove <site>
<target>` detaches by container path. Mounts must use absolute paths
(`~` is expanded); relative or non-existent host paths are refused. The
`/app` target is reserved for the project bind.

## Hot reload on metadata edits

The srv daemon watches every `~/.config/srv/sites/<name>/metadata.yml`
and re-applies changes within ~300ms (debounced across editor saves).
Hand-edit the YAML file → certs refresh, DNS updates, routing config
regenerates, `docker compose up -d` runs to pick up label changes. No
restart command needed.

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

## Daemon

The srv daemon also watches Docker container start events to keep new
containers connected to the srv network — handy when containers start
outside of srv (e.g. via `docker compose up` directly).

```bash
srv daemon start      # Start the daemon
srv daemon stop       # Stop the daemon
srv daemon restart    # Restart the daemon
srv daemon status     # Check daemon status
srv daemon logs       # View daemon logs
srv daemon install    # Install as system service (starts on boot)
srv daemon uninstall  # Remove system service
```

## Doctor

```bash
srv doctor [--fix-perms]
```

Checks Docker, firewall rules, port availability (80, 443, 8080, 53),
the srv Docker network, Traefik + DNS containers, local certificate
expiry, mkcert installation, per-site metadata validity, container-site
`.env` host-loopback references, and the ownership of `~/.config/srv`.
`--fix-perms` runs `sudo chown -R` to repair root-owned files.

## Importing from Laravel Valet

Migrate an existing Valet rig (works against `~/.config/valet` or legacy
`~/.valet`):

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

**PHP sites** are emitted as commented-out `srv add` lines. srv does not
manage runtimes, so each PHP project needs a user-provided Dockerfile or
docker-compose.yml before the line can be uncommented and run. The dry-run
output flags this with a note next to every PHP entry.

## Metrics (Prometheus + Grafana)

Opt-in observability stack scraping Traefik's existing `/metrics`
endpoint:

```bash
srv metrics enable
# https://grafana.local      (admin / admin)
# https://prometheus.local
srv metrics status
srv metrics disable
```

Both UIs are routed through Traefik with mkcert-signed TLS; loopback
ports are not exposed. Grafana ships with a pre-wired Prometheus
datasource. Import dashboard ID 17347 for a per-router Traefik overview.

## MCP server

`srv mcp` runs a [Model Context Protocol](https://modelcontextprotocol.io)
server on stdio so AI agents can drive srv the same way a human does
from the CLI. The read-only surface is complete today (sites, proxies,
redirects, validation, paths, version); mutating tools beyond
`reload_site` are still being extracted from CLI handlers and will land
in follow-ups.

### Wiring it into a client

The server is stdio-based: a client launches `srv mcp` and talks to it over
stdin/stdout. There is nothing to host or keep running — the client starts and
stops the process. The only requirement is that the `srv` binary is reachable.
If your client doesn't inherit your shell `PATH`, replace `"srv"` below with the
absolute path from `which srv` (e.g. `/usr/local/bin/srv`).

Most clients share the same `mcpServers` schema:

```json
{
  "mcpServers": {
    "srv": {
      "command": "srv",
      "args": ["mcp"]
    }
  }
}
```

**Claude Code** — one command, no file editing:

```sh
claude mcp add srv -- srv mcp          # current project
claude mcp add -s user srv -- srv mcp  # all projects (user scope)
```

**Claude Desktop** — paste the `mcpServers` block into the config file, then
restart the app:

- macOS: `~/Library/Application Support/Claude/claude_desktop_config.json`
- Windows: `%APPDATA%\Claude\claude_desktop_config.json`

**Cursor** — paste the `mcpServers` block into `~/.cursor/mcp.json` (global) or
`.cursor/mcp.json` (per-project).

**Windsurf** — paste the `mcpServers` block into
`~/.codeium/windsurf/mcp_config.json` (or via Cascade → Plugins → View raw
config).

**Cline / Roo Code** (VS Code extensions) — open the MCP Servers panel →
"Configure MCP Servers" and add the `srv` entry under `mcpServers`.

**VS Code** (GitHub Copilot agent mode) — uses a `servers` key, not
`mcpServers`. Put this in `.vscode/mcp.json` (workspace) or your user
`mcp.json`:

```json
{
  "servers": {
    "srv": {
      "command": "srv",
      "args": ["mcp"]
    }
  }
}
```

Or from the CLI: `code --add-mcp '{"name":"srv","command":"srv","args":["mcp"]}'`.

**Zed** — uses `context_servers` in `settings.json`:

```json
{
  "context_servers": {
    "srv": {
      "source": "custom",
      "command": "srv",
      "args": ["mcp"]
    }
  }
}
```

Available tools:

| Tool | Description |
|---|---|
| `version` | srv binary version + commit + build date |
| `paths` | Config paths (`~/.config/srv`, traefik conf dir, etc.) |
| `list_sites` | All registered sites with name, domain, type, status |
| `get_site` | Full metadata for one site |
| `validate_site` | Parse + validate a site's metadata.yml |
| `list_proxies` / `get_proxy` | Proxy inventory + per-proxy metadata |
| `list_redirects` | Redirect inventory |
| `reload_site` | Re-apply a site's metadata.yml without restart |

## Declarative config files

Every site, proxy, and redirect lives in a single yaml file under
`~/.config/srv/`. The daemon watches them and re-applies changes within
~300ms.

```yaml
# DNS-only redirect — two-key schema
dns:
  source: jira.konform.com.test
  target: jira.kontainer.com
```

```yaml
# HTTP redirect — full Traefik dynamic config (file provider hot-reloads on save)
http:
  routers:
    redirect-jira-konform-com-test:
      rule: Host(`jira.konform.com.test`)
      entryPoints: [websecure]
      service: redirect-jira-konform-com-test-noop
      middlewares: [redirect-jira-konform-com-test-mw]
      tls: {}
  middlewares:
    redirect-jira-konform-com-test-mw:
      redirectRegex:
        regex: ^https?://[^/]+/?(.*)$
        replacement: https://jira.kontainer.com/$1
        permanent: true
```

## How it works

- **Local SSL (`--local`)**: Uses [mkcert](https://github.com/FiloSottile/mkcert) for trusted local certificates. Domains are automatically registered with the built-in DNS server (dnsmasq).
- **Production SSL**: Uses Let's Encrypt via Traefik's ACME resolver. Certificates renew automatically.
- **Traefik**: Routes requests to containers based on domain rules. Configuration is generated automatically.
- **DNS**: Local domains (added with `--local` or via `srv proxy add`) are registered with a dnsmasq container and resolve to `127.0.0.1`. Works with any TLD (`.test`, `.local`, `.dev`, …).

## Configuration paths

All configuration lives in `~/.config/srv/` — srv never writes files to
your project directories.

| Path | Description |
|------|-------------|
| `~/.config/srv/config.yml` | Global configuration (parked paths) |
| `~/.config/srv/traefik/` | Traefik docker-compose and static config |
| `~/.config/srv/traefik/conf/` | Dynamic Traefik routing configs |
| `~/.config/srv/traefik/conf/site-<name>.yml` | Compose-site Traefik file-provider config |
| `~/.config/srv/traefik/conf/routes-<name>.yml` | Per-site extra routes (`srv route`) |
| `~/.config/srv/traefik/conf/proxy-<name>.yml` | Proxy file-provider config (`srv proxy`) |
| `~/.config/srv/traefik/conf/redirect-<name>.yml` | Redirect file-provider config (`srv redirect`) |
| `~/.config/srv/traefik/conf/proxy-metrics.yml` | grafana.local / prometheus.local routers |
| `~/.config/srv/traefik/certs/` | Let's Encrypt certificates (acme.json) |
| `~/.config/srv/sites/` | Site configurations |
| `~/.config/srv/sites/{name}/metadata.yml` | Site metadata (canonical source of truth) |
| `~/.config/srv/sites/{name}/.reload-state` | Hash of last-applied metadata (daemon short-circuit) |
| `~/.config/srv/sites/{name}/certs/` | Local SSL certificates (mkcert) |
| `~/.config/srv/sites/{name}/docker-compose.yml` | Generated compose (static + dockerfile sites only) |
| `~/.config/srv/sites/{name}/nginx.conf` | Generated nginx config (static sites only) |
| `~/.config/srv/metrics/` | Prometheus + Grafana compose stack |

## Global flags

| Flag | Short | Description |
|------|-------|-------------|
| `--verbose` | `-v` | Enable verbose output |

## Troubleshooting

### SSL not trusted in browser?

Restart your browser after adding your first local site. The mkcert CA is
auto-installed but browsers need to be restarted to recognise it.

### Site not accessible?

```bash
srv doctor
srv logs mysite
```

### DNS not resolving?

```bash
srv doctor | grep -A10 "DNS"
srv install   # re-runs the DNS setup steps
```

### Port already in use?

```bash
srv doctor | grep -A10 "Ports"
sudo lsof -i :80
sudo lsof -i :443
```

### Reset everything?

```bash
srv install --fresh
```

## License

MIT
