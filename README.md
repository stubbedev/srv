# srv

A CLI that puts Traefik + TLS in front of your sites. srv handles routing,
local certificates (mkcert is vendored in), production certificates (Let's
Encrypt), and local DNS. It does **not** manage language runtimes — for
anything beyond static files, you bring your own `Dockerfile` or
`docker-compose.yml` and srv attaches Traefik routing on top.

What you get:
- Static sites served by nginx with sensible defaults (SPA, caching, CORS, hidden-file blocks) — or by the srv daemon itself, with no per-site container (`srv add --daemon`)
- Proxies to arbitrary localhost ports or Docker containers
- HTTP and DNS-layer redirects with TLS-clean source hostnames
- Trusted local HTTPS (`*.test`, `*.local`, …) without browser warnings
- Auto-provisioned Let's Encrypt certificates for production domains
- Multi-host aliases, internal plain-HTTP listener, per-site path/regex routes
- An [MCP server](#mcp-server) so AI agents drive srv the same way the CLI does

## When srv is (and isn't) worth it

srv is an **edge layer**: Traefik, mkcert/ACME, a DNS server, and a daemon
wired together with a CLI that manages them per-site. It is not a PaaS — no
runtime, no buildpack, no app manager.

Worth it when you have a dev box or a server fronting multiple sites, apps,
`localhost:PORT` dev servers, and redirects under one TLS edge — especially a
multi-tenant app served under many hostnames (one SAN cert, one router, many
`Host` rules).

Overkill for a single project (run Caddy or FrankenPHP with a self-signed
cert directly), or if you already have a reverse-proxy setup you're happy
with (nginx-proxy, Caddy, bare Traefik, Kubernetes Ingress). srv overlaps
with those; it doesn't layer on top.

## Installation

### Via Homebrew (macOS / Linux)

```bash
brew tap stubbedev/tap
brew install stubbedev/tap/srv
brew services start srv   # optional: run the watch daemon in the background
```

Don't enable both `brew services start srv` and `srv daemon install` — they
register two supervisor units that race over the same job.

### Via install script

```bash
curl -fsSL https://raw.githubusercontent.com/stubbedev/srv/master/install.sh | sh
```

### Via releases

Download the tarball for your platform from
[releases](https://github.com/stubbedev/srv/releases/latest), extract, and
place `srv` on your `PATH`.

**Supported platforms:** Linux (amd64, arm64, armv7, 386), macOS (amd64,
arm64). The brew formula covers darwin/linux amd64+arm64; armv7 and 386 are
install-script or manual-download only.

**Runtime requirements:**
- A container runtime with a Docker-compatible API and Compose v2 — Docker,
  Podman, Colima, OrbStack or Rancher Desktop (see [Container runtimes](#container-runtimes)).
  Traefik runs as a container even for `--daemon` static sites.
- Ports 80 and 443 for Traefik (see the Podman note below for rootless engines)
- Nothing else: the [mkcert](https://github.com/FiloSottile/mkcert) engine is
  vendored into srv (same CAROOT layout as the mkcert tool), and DNS is served
  by an embedded resolver inside the srv daemon on `127.0.0.1:15353` — no
  dnsmasq container, no privileged DNS port. `srv install` points your system
  resolver (systemd-resolved, NetworkManager, or macOS) at it once, with a
  single sudo prompt. On systemd-resolved it also checks that
  `/etc/resolv.conf` actually sends lookups through resolved's stub; if it
  links to resolved's upstream list instead, `srv install --yes` re-points it
  (`srv doctor` shows the fix either way).

## Quick start

### Local development

```bash
srv install                                             # one-time setup
srv add ~/my-project --domain mysite.test --local       # static site (nginx container)
srv add ~/my-project --domain mysite.test --local --daemon   # no site container
# visit https://mysite.test
```

### Production

```bash
srv install                                             # prompts once for the Let's Encrypt email
srv add /var/www/myapp --domain example.com
# visit https://example.com — cert auto-provisioned
```

Production requirements: domain DNS pointing at the server, ports 80 and 443
open.

## Container runtimes

srv needs one thing from a container runtime: a **Docker-compatible API
endpoint** (to create the shared network, inspect containers, pull images —
Traefik watches labels through it) plus **Compose v2**, which writes the
`com.docker.compose.*` labels srv finds containers by (`podman compose`
delegates to Compose v2 and is fine; the Python `podman-compose` is not).
`srv doctor` checks both.

With no `container_engine` key set, srv probes for a runtime that is actually
usable here — CLI on `$PATH` and API socket present — in this order:

| Runtime | CLI | Socket probed |
|---|---|---|
| `docker` | `docker` | `/var/run/docker.sock`, `~/.docker/run/docker.sock` |
| `orbstack` | `docker` | `~/.orbstack/run/docker.sock` |
| `colima` | `docker` | `~/.colima/default/docker.sock`, `~/.colima/docker.sock` |
| `rancher-desktop` | `docker` | `~/.rd/docker.sock` |
| `podman` | `podman` | `$XDG_RUNTIME_DIR/podman/podman.sock`, `/run/podman/podman.sock` |

If nothing is detectable, srv falls back to Docker. To pin one instead:

```yaml
# ~/.config/srv/config.yml
container_engine: podman   # auto (default), docker, colima, orbstack, rancher-desktop
```

**`DOCKER_HOST` beats everything.** If it is exported, srv uses that endpoint
verbatim — which is also how you reach anything not in the table (a remote
daemon, a rootless socket in an odd place, a Docker-API shim). Otherwise srv
exports `DOCKER_HOST` itself from whatever it resolved, so the Docker SDK and
Compose v2 follow the same choice, and bind-mounts the socket into Traefik.

**nerdctl and Finch are not supported** — containerd's socket is a different
protocol, not a Docker-compatible API. Put a Docker-API shim in front and
point `DOCKER_HOST` at it if you have one.

**Rootful Podman is the supported Podman target.** Traefik binds :80 and
:443, below the kernel's unprivileged port floor, so rootless Podman refuses
them until the host lowers it:

```bash
sudo sysctl -w net.ipv4.ip_unprivileged_port_start=53
```

`srv doctor` reports which runtime it resolved and how, its endpoint, its
compose implementation, and this port floor when it applies.

## Commands

> Full reference, auto-generated from the binary:
> **[docs/cli.md](docs/cli.md)**. The table below is a summary; run
> `srv <command> --help` for flags.

<!-- BEGIN:cli -->
### Site Commands

| Command | Description |
|---------|-------------|
| `srv add PATH` | Add a site |
| `srv alias <add\|list\|remove>` | Manage extra hostnames for a site |
| `srv info SITE` | Show site info |
| `srv internal <disable\|enable\|list>` | Manage the plain-HTTP internal listener (port 88) for a site |
| `srv list` | List all sites |
| `srv logs [SITE]` | Show site logs |
| `srv network <attach\|detach\|list>` | Manage extra Docker networks attached to a site |
| `srv open SITE` | Open a site in the default browser |
| `srv reload [SITE]` | Re-apply a site's metadata.yml without restarting (unless --restart) |
| `srv remove SITE` | Remove a site |
| `srv restart SITE` | Restart a site |
| `srv route <add\|list\|remove>` | Manage extra Traefik routers attached to a site |
| `srv shell SITE` | Open an interactive shell in a site's container |
| `srv start SITE` | Start a site |
| `srv stop SITE` | Stop a site |
| `srv validate [SITE]` | Validate a site's metadata.yml without applying changes |
| `srv volume <add\|list\|remove>` | Manage extra host bind-mounts attached to a site |

### Proxy Commands

| Command | Description |
|---------|-------------|
| `srv proxy <add\|list\|remove>` | Manage proxy routes |
| `srv redirect <add\|list\|reload\|remove>` | Manage HTTP redirects |

### System Commands

| Command | Description |
|---------|-------------|
| `srv daemon <install\|logs\|restart\|start\|status\|stop\|uninstall>` | Manage the srv daemon |
| `srv doctor` | Run diagnostic checks |
| `srv import <valet>` | Import site configurations from other tools |
| `srv install` | Install srv environment |
| `srv mcp` | Start the srv MCP server (stdio, or --http for a shared daemon) |
| `srv metrics <disable\|enable\|status>` | Manage the optional metrics stack (prometheus + grafana) |
| `srv paths` | Show config paths |
| `srv uninstall` | Completely remove srv from the system |
| `srv update` | Update the Traefik image |
<!-- END:cli -->

> Generated from the command tree by `go run ./cmd/gen-readme`; run
> `just sync-readme` after touching a subcommand.

## `srv add`

Register a new site and generate its routing. The site type is
auto-detected from the project directory:

1. **Compose** — the path contains a `docker-compose.yml`
2. **Dockerfile** — the path contains a `Dockerfile`
3. **Static** — otherwise the directory is served as static files (nginx, or
   the srv daemon itself with `--daemon`)

If you point srv at a project that needs a runtime (PHP, Node, Ruby,
Python, …) but carries neither file, it is served as a static site. Drop in
a Dockerfile or docker-compose.yml to run the app code.

```bash
srv add PATH [flags]
```

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--domain` | `-d` | | Canonical hostname (required) |
| `--alias` | | | Extra hostname mapped to the same site (repeatable) |
| `--wildcard` | | `false` | Also match one-level subdomains (`*.foo.test`); local sites only |
| `--internal-http` | | `false` | Also expose on the plain-HTTP `:88` entrypoint |
| `--local` | `-l` | `false` | Local SSL via mkcert (otherwise Let's Encrypt) |
| `--daemon` | | `false` | Static only: serve from the srv daemon itself — no nginx container, no Docker |
| `--name` | `-n` | directory name | Custom site name |
| `--port` | `-p` | `80` | Container port to route traffic to |
| `--service` | | | Compose service to route to (multi-service files) |
| `--profile` | | | docker-compose profile (required if the service declares multiple) |
| `--force` | `-f` | `false` | Overwrite existing configuration |
| `--spa` | | `true` | Static only: fall back to `/index.html` for unknown routes |
| `--cache` | | `true` | Static only: emit caching headers for static assets |
| `--cors` | | `false` | Static only: emit permissive CORS headers |
| `--volume` | | | Extra bind-mount `HOST:CONTAINER[:ro]` (repeatable) |
| `--type` | | auto | Force site type: `static`, `dockerfile`, or `compose` |

```bash
# Static site (auto-detected)
srv add ./dist --domain docs.test --local

# Static site served by the daemon itself — no site container
srv add ./slides --domain slides.local --local --daemon

# Compose site, specific service + port
srv add ./app --domain api.test --local --service backend --port 3000

# Force static even if a Dockerfile is present
srv add ./mixed-project --domain x.test --local --type static
```

## Static sites

For directories without a `docker-compose.yml` or `Dockerfile`, srv by
default generates an nginx (`nginx:alpine`) container that serves the
directory with:

- SPA routing (fallback to `/index.html`) and asset caching, both configurable
- Optional permissive CORS headers and a custom `404.html`
- Hidden-file and sensitive-extension blocks (`.env`, `.git`, `.htaccess`, …)
- gzip compression and standard security headers

With `--daemon`, srv skips the container entirely: the daemon's embedded
HTTP server hosts the site on `127.0.0.1:15380` and multiplexes every
daemon-served site by `Host` header. It mirrors the nginx semantics above,
`docker compose` is never invoked (start/stop just add or remove the
Traefik route file), and `srv logs SITE` shows per-request access logs
straight from the daemon log. `--port` and `--volume` don't apply — there
is no container.

## Dockerfile and compose sites (bring your own runtime)

Any project root with a `Dockerfile` is a dockerfile site; any root with a
`docker-compose.yml` is a compose site. srv attaches Traefik routing and
leaves your files alone — it never generates or edits them.

### Worked example: Laravel with local HTTPS

```yaml
# docker-compose.yml
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

```env
# .env
APP_URL=https://mylaravel.test
ASSET_URL=https://mylaravel.test
TRUSTED_PROXIES=*
```

`TRUSTED_PROXIES=*` is required so Laravel respects
`X-Forwarded-Proto: https` from Traefik — otherwise it generates `http://`
URLs and you hit mixed-content errors.

```bash
srv add . --domain mylaravel.test --local
```

srv detects the compose file, mints a mkcert cert, registers the hostname
with its DNS server, attaches routing labels to the `app` service, and runs
`docker compose up -d`. For host-side MySQL/Redis on the host's loopback,
point `.env` at `host.docker.internal` — see
[Talking to host services from inside a container](#talking-to-host-services-from-inside-a-container).

## Proxies (non-Docker upstreams)

```bash
srv proxy add --domain api.test --port 3000            # local dev server
srv proxy add --domain db.test --container postgres:5432

# 5xx fallback to a remote URL: Traefik's native failover re-proxies when
# the primary upstream returns 5xx
srv proxy add --domain myapp.com --port 3001 \
  --fallback https://myapp.com --fallback-timeout 2s

srv proxy list
srv proxy remove api.test
```

All proxies use local SSL (mkcert) and register with srv's DNS server.

## Host-to-URL redirects

301 (permanent, default) or 302 (`--temporary`). The request path and query
string are appended to the target, so
`https://jira.example.com/browse/X?y=1` lands on
`https://jira.myapp.com/browse/X?y=1`. A mkcert-signed certificate is
provisioned for the source domain so browsers follow the redirect without a
TLS warning.

```bash
srv redirect add --domain jira.example.com --to https://jira.myapp.com
srv redirect add -d old.test --to https://new.test --temporary
srv redirect add -d legacy.test --to https://new.test --wildcard

srv redirect list
srv redirect remove jira-example-com
```

### `--dns-only` (DNS-layer redirect)

Skip Traefik and TLS entirely: the source hostname is pinned to the target's
resolved IP with an `address=` record in srv's DNS server.

```bash
srv redirect add --domain jira.example.com.test --to jira.myapp.com --dns-only
```

The client never sees an HTTP 301 — it connects straight to the target's IP
with `Host: jira.example.com.test`. Whether the visible URL changes depends
on what the backend does with that `Host:` header. When the target's IP
changes, run `srv redirect reload` to re-resolve.

| | `--dns-only` | default (HTTP 301/302) |
|---|---|---|
| Browser URL bar | depends on backend behavior | always switches to target |
| Path / query preserved | yes | yes |
| Works if target unreachable | no — TCP fails | yes — the redirect is the response |

## Multi-domain aliases

Run one container under many hostnames — handy for multi-tenant apps where
every tenant maps to the same project:

```bash
srv add ~/git/work/myapp --domain myapp.test \
  --alias cms-myapp.test --alias jira.example.com.test --local --wildcard

srv alias add myapp jira-staging.test
srv alias remove myapp jira-staging.test
srv alias list myapp
```

A single mkcert certificate covers every alias; all hostnames register with
the DNS server; the Traefik router OR-joins every `Host` rule.

## Internal plain-HTTP listener

Container-to-host calls often want `https://myapp.test` from another
container whose client doesn't trust the mkcert CA. srv exposes a second
Traefik entrypoint on `:88` that serves the same routers without TLS:

```bash
srv add ./my-app --domain app.test --local --internal-http   # at add time
srv internal enable app.test                                  # or post-hoc
srv internal disable app.test
srv internal list
```

Result: `https://app.test` (mkcert TLS) and `http://app.test:88` (plain)
both reach the same backend.

## Per-site routes

Attach extra Traefik routers so different paths hit different upstreams:

```bash
# Path-prefix split (e.g. WebSocket on /app)
srv route add myapp.test --path /app --port 6001

# Regex rewrite
srv route add myapp.test \
  --path-regex '^/videos/([^/]+)/(.+)$' \
  --rewrite '/abs/videos/$1/$2' \
  --port 9080 --preserve-host

# Upstream targets: localhost port, container[:port], or http(s):// URL
srv route add api.test --path /v2 --container backend-v2:3000
srv route add docs.test --path /sdk --url https://sdk.example.com

srv route list myapp.test
srv route remove myapp.test app
```

Routes are persisted in the site's `metadata.yml` under `routes:` and
emitted as a per-site Traefik file-provider config at
`~/.config/srv/traefik/conf/routes-<name>.yml`.

## Talking to host services from inside a container

App code in a container has its own loopback namespace, so
`DB_HOST=127.0.0.1` points at the app container itself, not at MySQL on the
host. Three escape hatches:

**(a) Host services on the loopback → `host.docker.internal`.** Add
`extra_hosts: ["host.docker.internal:host-gateway"]` to your
`docker-compose.yml` and rewrite each affected `.env` entry
(`DB_HOST=host.docker.internal`). `srv doctor` warns when it finds
`*_HOST=127.0.0.1`-style entries in container-backed sites.

**(b) Services in another compose stack → `srv network attach`.** Join your
site to that stack's network and address the containers by hostname:

```bash
srv network attach my-app mysql01_default
srv network detach my-app mysql01_default
srv network list my-app
```

Networks must already exist; run `srv restart <site>` after attaching.

**(c) Host files and binaries → `srv volume add`.** Mount what the app
needs (`ffmpeg`, a shared temp dir, nix profiles):

```bash
srv volume add my-app ~/.nix-profile:/home/$USER/.nix-profile:ro
srv volume add my-app /nix:/nix:ro
```

Mounts must be absolute (`~` is expanded); `/app` is reserved for the
project bind. `srv volume list <site>` and `srv volume remove <site>
<target>` manage them.

## Daemon

The daemon is a user service (systemd user unit or launchd agent) that runs
the shared infrastructure and keeps sites in sync:

- **Hot reload** — watches every `~/.config/srv/sites/<name>/metadata.yml`
  and re-applies changes within ~300ms (debounced across editor saves):
  certs refresh, DNS updates, routing regenerates. Hand-edit the YAML, no
  restart command needed.
- **Docker events** — connects containers started outside srv (e.g. a bare
  `docker compose up`) to the srv network.
- **Embedded DNS** — serves all registered local domains on
  `127.0.0.1:15353`.
- **Embedded static server** — hosts `--daemon` static sites on
  `127.0.0.1:15380`.
- **Single instance** — the daemon takes a lock on `<root>/daemon.lock`:
  however many sites and containers you run, one daemon serves them all. A
  second `daemon start --foreground` refuses, and a standalone `srv dnsd` /
  `srv httpd` names the daemon as the port holder instead of failing with a
  bare "address already in use".

```bash
srv daemon start      # --foreground to run in the foreground, --no-watch to disable hot reload
srv daemon stop | restart | status | logs
srv daemon install    # start on boot (systemd user service / launchd agent)
srv daemon uninstall
```

Manual triggers, for when the watcher is off or you want to force it:

```bash
srv reload SITE             # re-apply one site's metadata
srv reload --all            # all sites
srv reload SITE --restart   # also force container restart (label-baked changes)
srv validate SITE           # check metadata.yml without applying
```

## Doctor

```bash
srv doctor [--fix-perms]
```

Checks the user config, container engine + Compose v2, the firewall, port
availability (80, 443, 8080), the srv Docker network, the Traefik container,
srv's DNS server and the system resolver routing to it, local certificate
validity and expiry, per-site metadata validity, container-site `.env`
host-loopback references, and the ownership of `~/.config/srv`.
`--fix-perms` runs `sudo chown -R` to repair root-owned files.

## Importing from Laravel Valet

```bash
srv import valet             # print the equivalent srv commands (dry run)
srv import valet --apply     # execute them
srv import valet --list-sites
```

Reads every Valet nginx config (`~/.valet` or `~/.config/valet`, whichever
has content), resolves each host to its project directory via parked paths
and `Sites/` symlinks, folds hosts sharing a project root into one
`srv add --alias …` call, and maps proxy passes, `:88` listeners, path
splits, regex rewrites, and fallback blocks onto the matching srv commands.
`--skip` records decisions in `~/.config/srv/import-decisions.yml`.

**PHP sites** are emitted as commented-out `srv add` lines: srv does not
manage runtimes, so each project needs a user-provided Dockerfile or
docker-compose.yml before the line can run.

## Metrics (Prometheus + Grafana)

Opt-in observability stack scraping Traefik's existing `/metrics` endpoint:

```bash
srv metrics enable
# https://grafana.local      (admin / admin)
# https://prometheus.local
srv metrics status
srv metrics disable
```

Both UIs route through Traefik with mkcert-signed TLS; loopback ports are
not exposed. Grafana ships with a pre-wired Prometheus datasource — import
dashboard ID 17347 for a per-router Traefik overview.

## MCP server

`srv mcp` runs a [Model Context Protocol](https://modelcontextprotocol.io)
server so AI agents can drive srv the same way a human does from the CLI —
inspecting and mutating sites, proxies, redirects, routes, networks, and
volumes.

**The tool surface is lazy-loaded.** At startup the server advertises only
`version` and `srv_activate`, so srv costs no context in sessions that never
touch it. When the agent needs srv, `srv_activate(group="read")` unlocks
inspection and diagnostics, `srv_activate(group="write")` (the default)
also unlocks every mutating tool. Activation lasts for the session;
destructive tools still gate on confirmation regardless of tier. The full
tool table is generated below.

**Transports:**

- **stdio (default)** — the client launches one `srv mcp` process per
  client; nothing to host or keep running.
- **Streamable HTTP (`--http`)** — one long-running daemon shared by every
  MCP client on the host:

  ```sh
  srv mcp --http                 # listens on 127.0.0.1:8765/mcp
  srv mcp --http=0.0.0.0:9000    # bind elsewhere; --http-path=/foo to remap
  ```

  Each HTTP session keeps its own activation state. Per-request workspace
  context (anchoring relative paths in `add_site`/`add_volume`) comes from
  the client's MCP roots or an `X-Repo-Root` header. Mutating calls are
  serialized across clients; each call is bounded by `--tool-timeout`
  (default 10m).

The HTTP endpoint binds **loopback with no auth** — it mutates a privileged
Traefik edge, so it trusts local processes only. Put it behind a reverse
proxy with TLS and authentication before binding off-host; browser clients
on another origin need `--trusted-origin`.

**Wiring it into a client** — most share the same `mcpServers` schema:

```json
{ "mcpServers": { "srv": { "command": "srv", "args": ["mcp"] } } }
```

Claude Code, no file editing:

```sh
claude mcp add srv -- srv mcp          # current project
claude mcp add -s user srv -- srv mcp  # all projects
```

Paste the block into Claude Desktop's `claude_desktop_config.json`,
Cursor's `~/.cursor/mcp.json`, Windsurf's `mcp_config.json`, or Cline/Roo
Code's MCP panel. VS Code (Copilot agent mode) uses a `servers` key:
`code --add-mcp '{"name":"srv","command":"srv","args":["mcp"]}'`. If your
client doesn't inherit your shell `PATH`, use the absolute path from
`which srv`.

<!-- BEGIN:mcp -->
Available tools, by tier:

| Tier | Tool | Description |
|---|---|---|
| core | `srv_activate` | Unlock a tier of srv tools. |
| core | `version` | Return the srv version, commit, and build date. |
| read | `daemon_log` | Return the tail of the daemon log (default 50 lines, override with `lines`). |
| read | `daemon_status` | Report whether the srv watch daemon is installed and running, plus its raw service-manager status (systemd/launchd). |
| read | `get_proxy` | Return full metadata for one proxy: domains, aliases, wildcard flag, is_local flag, attached routes. |
| read | `get_site` | Return full metadata for one site: domains, aliases, routes, mounts, internal-http flag, network attachments, container status, type, project dir. |
| read | `list_proxies` | List every srv-managed proxy by name. |
| read | `list_redirects` | List every srv-managed redirect by name. |
| read | `list_sites` | List every registered site with name, canonical domain, type (static/dockerfile/compose), is_local flag, and container status. |
| read | `metrics_status` | Report whether the metrics stack (Prometheus + Grafana) containers are running, with their dashboard domains. |
| read | `paths` | Return the on-disk paths srv writes to (config root, sites dir, traefik conf dir, proxies dir). |
| read | `validate_site` | Parse a site's metadata.yml and report whether it's valid. |
| write | `add_alias` | Add an extra hostname (alias) to a site. |
| write | `add_proxy` | Create a proxy routing a domain to a localhost port or a Docker container (container="name:port"). |
| write | `add_redirect` | Create a redirect. |
| write | `add_route` | Attach an extra Traefik route (path-prefix or regex) to a site or proxy `target`. |
| write | `add_site` | Register a new site from a project directory and start it. |
| write | `add_volume` | Attach an extra host bind-mount to a site's container. |
| write | `attach_network` | Attach an existing Docker network to a site so its container can reach services on that network. |
| write | `detach_network` | Detach an extra Docker network from a site. |
| write | `reload_site` | Re-apply a site's metadata.yml without restarting the container. |
| write | `remove_alias` | Remove an alias hostname from a site (the canonical first domain cannot be removed this way). |
| write | `remove_proxy` | Remove a proxy: deletes its Traefik config, local cert, DNS registration, and metadata. |
| write | `remove_redirect` | Remove a redirect (HTTP or DNS-only): deletes its yaml and any derived cert/DNS state. |
| write | `remove_route` | Remove an extra route by id from a site or proxy `target`. |
| write | `remove_site` | Remove a site: stop its containers and delete its Traefik config, local cert, DNS registrations, and metadata directory. |
| write | `remove_volume` | Detach a bind-mount from a site by its container target path. |
| write | `restart_site` | Restart a site's containers, regenerating artifacts first. |
| write | `set_internal_listener` | Enable or disable the plain-HTTP `internal` entrypoint for a site (enable=true/false). |
| write | `start_site` | Start a site's containers (docker compose up). |
| write | `stop_site` | Stop a site's containers (docker compose stop). |
<!-- END:mcp -->

> Generated from the live MCP server by `go run ./cmd/gen-readme`.

## Declarative config files

Every site, proxy, and redirect lives in a single yaml file under
`~/.config/srv/`. The daemon watches them and re-applies changes within
~300ms. The field reference below is generated from the Go structs (the
same source as the published [JSON Schemas](schemas/)), so it always
matches the binary.

<!-- BEGIN:config -->
#### Site — `metadata.yml`

_Path: `~/.config/srv/sites/<name>/metadata.yml`_

| Field | Type | Required | Description |
|---|---|---|---|
| `schema_version` | integer | no | metadata.yml schema version (1 = current). |
| `type` | string | no | Site runtime type. |
| `domains` | array<string> | no | All hostnames; the first entry is canonical. |
| `project_path` | string | no | Absolute path to the project on disk. |
| `service_name` | string | no | Container name used for Traefik routing. |
| `compose_service_name` | string | no | docker-compose service name (for compose commands). |
| `profile` | string | no | docker-compose profile (if the service uses profiles). |
| `port` | integer | no | Port the service listens on inside the container. |
| `is_local` | boolean | no | Whether to use a locally-issued (mkcert) SSL certificate. |
| `wildcard` | boolean | no | Match apex + one-level subdomains (*.example.com). |
| `network_name` | string | no | Docker network the site joins. |
| `extra_networks` | array<string> | no | Extra external Docker networks the site joins (for reaching user-managed containers like mysql01). |
| `volumes` | array<object> | no | Extra host bind-mounts attached to the site's container (e.g. ~/.nix-profile |
| `listeners` | array<string> | no | Extra Traefik entrypoints (e.g. 'internal' for plain HTTP on :88). |
| `routes` | array<object> | no | Extra Traefik routers (path-prefix / regex-rewrite splits). |
| `spa` | boolean | no | Single-page-app mode (fall back to /index.html). |
| `cache` | boolean | no | Emit aggressive caching headers for static assets. |
| `cors` | boolean | no | Emit permissive CORS headers. |
| `daemon_served` | boolean | no | Serve the static files from the srv daemon itself — no nginx container and no Docker; one embedded server hosts every daemon-served site. |
| `dockerfile_port` | integer | no | Port discovered from the Dockerfile EXPOSE directive. |

#### Proxy — `proxy-<name>.yml`

_Path: `~/.config/srv/proxies/proxy-<name>.yml`_

| Field | Type | Required | Description |
|---|---|---|---|
| `schema_version` | integer | no | metadata.yml schema version (1 = current). |
| `name` | string | no | Proxy name; also the basename of the generated proxy-<name>.yml. |
| `domains` | array<string> | no | All hostnames routed to this proxy; the first entry is canonical. |
| `wildcard` | boolean | no | Match apex + one-level subdomains (*.example.com); local proxies only. |
| `is_local` | boolean | no | Use a locally-issued (mkcert) SSL certificate instead of Let's Encrypt. |
| `port` | integer | no | Port of the primary upstream (a localhost service the daemon's embedded components dial); 0 for container-primary proxies. |
| `routes` | array<object> | no | Extra Traefik routers (path-prefix / regex-rewrite splits) attached via `srv route`. |
| `fallback_url` | string | no | Fallback (srv proxy add --fallback): the remote URL 5xx responses re-proxy to. Empty when the proxy has no fallback. The failover itself is Traefik's native `failover` service, rendered into proxy-<name>.yml. |
| `fallback_timeout` | string | no | FallbackTimeout is the connect timeout to the primary upstream before Traefik fails over (--fallback-timeout). |
| `fallback_port` | integer | no | FallbackPort is retired: it recorded the loopback port of the daemon- hosted failover listener, which the native Traefik failover replaced. Old metadata files still carry it; it is read only to detect proxies that need migration, and cleared when that migration re-renders them. |

#### DNS-only redirect

_Path: `~/.config/srv/traefik/conf/redirect-<name>.yml`_

| Field | Type | Required | Description |
|---|---|---|---|
| `dns` | object | no | source → target hostname pair for the DNS-layer redirect. |

#### User config — `config.yml`

_Path: `~/.config/srv/config.yml`_

| Field | Type | Required | Description |
|---|---|---|---|
| `container_engine` | string | no | Container runtime srv drives. Omit it (or set auto) to detect one; name it to pin. Requires a Docker-compatible API endpoint and Compose v2. |
| `upstream_dns` | array<string> | no | Upstream resolvers written into dnsmasq.conf as server= lines. Each entry must be an IP address with an optional #port suffix. Defaults to Google DNS (8.8.8.8 and 8.8.4.4) when empty. |
<!-- END:config -->

> Generated by `go run ./cmd/gen-readme`.

Example — a compose site on a local domain (`sites/app/metadata.yml`):

```yaml
type: compose
domains: [app.example.test]
is_local: true
```

## How it works

- **Traefik** (pinned `v3.7`) terminates TLS on :80/:443 and routes by
  `Host` rule from generated file-provider configs in
  `~/.config/srv/traefik/conf/`. The dashboard is on
  `http://127.0.0.1:8080/dashboard/`.
- **Local SSL (`--local`)**: certificates from srv's vendored
  [mkcert](https://github.com/FiloSottile/mkcert) CA — trusted in your
  browser without warnings.
- **Production SSL**: Let's Encrypt via Traefik's ACME resolver; renewal is
  automatic.
- **DNS**: the daemon's embedded server answers every registered local
  domain with `127.0.0.1` (any TLD works) on `127.0.0.1:15353` and forwards
  everything else upstream (`upstream_dns`, Google DNS by default).
  `srv install` points the system resolver at it; the underlying zone files
  stay in dnsmasq format and remain hand-editable (watched, and re-read on
  SIGHUP).

## Configuration paths

All configuration lives in `~/.config/srv/` — srv never writes files to
your project directories (`srv paths` prints the main ones).

| Path | Description |
|------|-------------|
| `~/.config/srv/config.yml` | User config (container engine, upstream DNS) |
| `~/.config/srv/daemon.log` | Daemon + access log |
| `~/.config/srv/traefik/` | Traefik docker-compose and static config |
| `~/.config/srv/traefik/conf/` | Dynamic Traefik routing configs |
| `~/.config/srv/traefik/conf/site-<name>.yml` | Site routing config (compose + daemon-served sites) |
| `~/.config/srv/traefik/conf/routes-<name>.yml` | Per-site extra routes (`srv route`) |
| `~/.config/srv/traefik/conf/proxy-<name>.yml` | Proxy routing config (`srv proxy`) |
| `~/.config/srv/traefik/conf/redirect-<name>.yml` | Redirect config (`srv redirect`) |
| `~/.config/srv/traefik/conf/proxy-metrics.yml` | grafana.local / prometheus.local routers |
| `~/.config/srv/traefik/dnsmasq.conf`, `dnsmasq.hosts/` | DNS zone files (dnsmasq format; watched by the embedded server) |
| `~/.config/srv/traefik/certs/` | Let's Encrypt certificates (acme.json) |
| `~/.config/srv/sites/<name>/metadata.yml` | Site metadata (canonical source of truth) |
| `~/.config/srv/sites/<name>/certs/` | Local SSL certificates (mkcert) |
| `~/.config/srv/sites/<name>/docker-compose.yml`, `nginx.conf` | Generated for static + dockerfile sites |
| `~/.config/srv/sites/<name>/.reload-state` | Hash of last-applied metadata (hot-reload short-circuit) |
| `~/.config/srv/proxies/` | Proxy metadata (`proxy-<name>.yml`) |
| `~/.config/srv/metrics/` | Prometheus + Grafana compose stack |

## Global flags

| Flag | Short | Description |
|------|-------|-------------|
| `--format` | | Output format for list/inspect commands: `table` (default) or `json` |
| `--quiet` | `-q` | Suppress informational output (errors still printed) |
| `--verbose` | `-v` | Enable verbose output |

## Troubleshooting

**SSL not trusted in browser?** Restart your browser after adding your
first local site — the mkcert CA is installed system-wide but browsers only
pick it up on restart.

**Site not accessible?**

```bash
srv doctor
srv logs mysite
```

**DNS not resolving?** `srv doctor` reports whether srv's DNS server is up
and whether your system resolver actually routes through it. `srv install`
re-runs the resolver setup (installs upgraded from older srv releases may
still point at the retired container-era port).

**Port already in use?**

```bash
srv doctor
sudo lsof -i :80
sudo lsof -i :443
```

**Reset everything?**

```bash
srv install --fresh --yes
```

**Remove srv entirely?**

```bash
srv uninstall
```

## License

MIT
