# srv CLI reference

[← back to README](../README.md)

> Auto-generated from the `srv` command tree by `go run ./cmd/gen-docs`.
> Run `just sync-docs` after touching any subcommand to refresh.

## Global flags

Available on every command:

| Flag | Default | Description |
|---|---|---|
| `--format` | `table` | Output format for list/inspect commands: 'table' (default, human-readable) or 'json' (scriptable) |
| `--quiet`, `-q` | `false` | Suppress informational diagnostic output (errors still printed) |
| `--verbose`, `-v` | `false` | Enable verbose output |

## Index

- [`srv add`](#srv-add) — Add a site
- [`srv alias`](#srv-alias) — Manage extra hostnames for a site
  - [`srv alias add`](#srv-alias-add) — Add an alias hostname to a site
  - [`srv alias list`](#srv-alias-list) — List a site's canonical domain and aliases
  - [`srv alias remove`](#srv-alias-remove) — Remove an alias hostname from a site
- [`srv daemon`](#srv-daemon) — Manage the srv daemon
  - [`srv daemon install`](#srv-daemon-install) — Install daemon as a system service
  - [`srv daemon logs`](#srv-daemon-logs) — Show daemon logs
  - [`srv daemon restart`](#srv-daemon-restart) — Restart the daemon
  - [`srv daemon start`](#srv-daemon-start) — Start the srv daemon
  - [`srv daemon status`](#srv-daemon-status) — Show daemon status
  - [`srv daemon stop`](#srv-daemon-stop) — Stop the srv daemon
  - [`srv daemon uninstall`](#srv-daemon-uninstall) — Uninstall daemon system service
- [`srv doctor`](#srv-doctor) — Run diagnostic checks
- [`srv import`](#srv-import) — Import site configurations from other tools
  - [`srv import valet`](#srv-import-valet) — Translate ~/.valet/Nginx/* into srv commands
- [`srv info`](#srv-info) — Show site info
- [`srv install`](#srv-install) — Install srv environment
- [`srv internal`](#srv-internal) — Manage the plain-HTTP internal listener (port 88) for a site
  - [`srv internal disable`](#srv-internal-disable) — Disable the internal listener on a site
  - [`srv internal enable`](#srv-internal-enable) — Enable the internal listener on a site
  - [`srv internal list`](#srv-internal-list) — List sites with the internal listener enabled
- [`srv list`](#srv-list) — List all sites
- [`srv logs`](#srv-logs) — Show site logs
- [`srv mcp`](#srv-mcp) — Start the srv MCP server (stdio, or --http for a shared daemon)
- [`srv metrics`](#srv-metrics) — Manage the optional metrics stack (prometheus + grafana)
  - [`srv metrics disable`](#srv-metrics-disable) — Stop and remove the metrics stack containers
  - [`srv metrics enable`](#srv-metrics-enable) — Render the metrics compose stack and start containers
  - [`srv metrics status`](#srv-metrics-status) — Show whether the metrics stack is running
- [`srv network`](#srv-network) — Manage extra Docker networks attached to a site
  - [`srv network attach`](#srv-network-attach) — Attach a site's container to an external Docker network
  - [`srv network detach`](#srv-network-detach) — Detach a site from an external Docker network
  - [`srv network list`](#srv-network-list) — List extra Docker networks attached to a site
- [`srv open`](#srv-open) — Open a site in the default browser
- [`srv paths`](#srv-paths) — Show config paths
- [`srv proxy`](#srv-proxy) — Manage proxy routes
  - [`srv proxy add`](#srv-proxy-add) — Add a proxy
  - [`srv proxy list`](#srv-proxy-list) — List all proxies
  - [`srv proxy remove`](#srv-proxy-remove) — Remove a proxy
- [`srv redirect`](#srv-redirect) — Manage HTTP redirects
  - [`srv redirect add`](#srv-redirect-add) — Add a redirect
  - [`srv redirect list`](#srv-redirect-list) — List all redirects
  - [`srv redirect reload`](#srv-redirect-reload) — Re-apply every redirect-*.yml file
  - [`srv redirect remove`](#srv-redirect-remove) — Remove a redirect
- [`srv reload`](#srv-reload) — Re-apply a site's metadata.yml without restarting (unless --restart)
- [`srv remove`](#srv-remove) — Remove a site
- [`srv restart`](#srv-restart) — Restart a site
- [`srv route`](#srv-route) — Manage extra Traefik routers attached to a site
  - [`srv route add`](#srv-route-add) — Attach a route to a site
  - [`srv route list`](#srv-route-list) — List routes attached to a site
  - [`srv route remove`](#srv-route-remove) — Remove a route from a site
- [`srv shell`](#srv-shell) — Open an interactive shell in a site's container
- [`srv start`](#srv-start) — Start a site
- [`srv stop`](#srv-stop) — Stop a site
- [`srv uninstall`](#srv-uninstall) — Completely remove srv from the system
- [`srv update`](#srv-update) — Update Traefik and DNS images
- [`srv validate`](#srv-validate) — Validate a site's metadata.yml without applying changes
- [`srv version`](#srv-version) — Show version info
- [`srv volume`](#srv-volume) — Manage extra host bind-mounts attached to a site
  - [`srv volume add`](#srv-volume-add) — Attach a bind-mount to a site
  - [`srv volume list`](#srv-volume-list) — List bind-mounts attached to a site
  - [`srv volume remove`](#srv-volume-remove) — Remove a bind-mount from a site by its container target path

## `srv add`

Add a site

```
Register a new site with srv and generate Traefik configuration.

If the PATH contains a docker-compose.yml file, srv will configure Traefik
to route traffic to the specified service. No files are created in the
project directory - all config is stored in ~/.config/srv.

If no docker-compose.yml is found, srv will serve the directory as static
files using nginx.

SSL certificates:
  - Use --local to generate a local certificate with mkcert
  - Without --local, Let's Encrypt will be used for production SSL

Examples:
  srv add /path/to/site --domain example.com          # Production with Let's Encrypt
  srv add /path/to/site --domain myapp.test --local   # Local dev with mkcert
  srv add . --domain example.com --start              # Add and start immediately
  srv add /path/to/static --domain site.test --local  # Static files with nginx
```

Usage:

```
srv add PATH [flags]
```

| Flag | Default | Description |
|---|---|---|
| `--alias` | `[]` | Additional hostname mapped to the same site (repeatable) |
| `--cache` | `true` | Enable caching headers for static assets |
| `--cors` | `false` | Enable CORS headers (allow all origins) |
| `--domain`, `-d` | — | Domain/hostname (e.g., example.com or myapp.test) |
| `--force`, `-f` | `false` | Overwrite existing configuration |
| `--internal-http` | `false` | Expose the site on the internal plain-HTTP entrypoint (port 88) in addition to HTTPS |
| `--local`, `-l` | `false` | Use local SSL via mkcert (otherwise Let's Encrypt) |
| `--name`, `-n` | — | Site name (default: directory name) |
| `--port`, `-p` | `80` | Container port |
| `--profile` | — | Docker Compose profile (required when the selected service declares multiple) |
| `--service` | — | Container name to route to |
| `--skip-validation` | `false` | Skip compose file validation |
| `--spa` | `true` | Enable SPA mode (fallback to index.html) |
| `--type` | — | Force site type: dockerfile, static, compose |
| `--volume` | `[]` | Extra bind-mount in HOST:CONTAINER[:ro] form; repeatable |
| `--wildcard` | `false` | Also match one-level subdomains (e.g. *.foo.test); local sites only |

## `srv alias`

Manage extra hostnames for a site

```
Each site has one canonical domain plus zero or more aliases. All hostnames
share a single set of generated configs (cert, DNS, Traefik router) so that
multiple hostnames route into the same container.
```

Usage:

```
srv alias
```

Subcommands:

- `srv alias add` — Add an alias hostname to a site
- `srv alias list` — List a site's canonical domain and aliases
- `srv alias remove` — Remove an alias hostname from a site

## `srv alias add`

Add an alias hostname to a site

Usage:

```
srv alias add SITE DOMAIN
```

## `srv alias list`

List a site's canonical domain and aliases

Usage:

```
srv alias list SITE
```

## `srv alias remove`

Aliases: `rm`

Remove an alias hostname from a site

Usage:

```
srv alias remove SITE DOMAIN
```

## `srv daemon`

Manage the srv daemon

```
The srv daemon watches for Docker container start events and automatically
connects registered site containers to the srv network.

This ensures that containers are properly connected even when started
outside of srv (e.g., via docker compose up directly).
```

Usage:

```
srv daemon
```

Subcommands:

- `srv daemon install` — Install daemon as a system service
- `srv daemon logs` — Show daemon logs
- `srv daemon restart` — Restart the daemon
- `srv daemon start` — Start the srv daemon
- `srv daemon status` — Show daemon status
- `srv daemon stop` — Stop the srv daemon
- `srv daemon uninstall` — Uninstall daemon system service

## `srv daemon install`

Install daemon as a system service

```
Install the srv daemon as a system service that starts automatically.

On Linux, this creates a systemd user service.
On macOS, this creates a launchd agent.

The daemon will start automatically on login and restart if it crashes.
```

Usage:

```
srv daemon install
```

## `srv daemon logs`

Show daemon logs

Usage:

```
srv daemon logs [flags]
```

| Flag | Default | Description |
|---|---|---|
| `--follow`, `-f` | `false` | Follow log output |
| `--tail`, `-n` | `50` | Number of lines to show |

## `srv daemon restart`

Restart the daemon

```
Restart the srv daemon service.
```

Usage:

```
srv daemon restart
```

## `srv daemon start`

Start the srv daemon

```
Start the srv daemon.

The daemon watches Docker events and automatically connects containers
from registered sites to the srv network when they start.

Use --foreground to run in the foreground (useful for debugging).
```

Usage:

```
srv daemon start [flags]
```

| Flag | Default | Description |
|---|---|---|
| `--foreground`, `-f` | `false` | Run in foreground (don't daemonize) |
| `--no-watch` | `false` | Disable the metadata.yml file watcher (hot-reload) |

## `srv daemon status`

Show daemon status

Usage:

```
srv daemon status
```

## `srv daemon stop`

Stop the srv daemon

Usage:

```
srv daemon stop
```

## `srv daemon uninstall`

Uninstall daemon system service

```
Remove the srv daemon system service. The daemon will no longer start automatically.
```

Usage:

```
srv daemon uninstall
```

## `srv doctor`

Run diagnostic checks

```
Run diagnostic checks to identify common issues with your srv setup.

Checks performed:
  - Docker availability and status
  - Required ports (80, 443, 8080)
  - Docker network existence
  - Traefik container status
  - Local SSL certificate validity
  - mkcert installation
  - Site metadata validity
  - .env host-loopback references in container-backed sites
  - Ownership of ~/.config/srv (use --fix-perms to repair)
```

Usage:

```
srv doctor [flags]
```

| Flag | Default | Description |
|---|---|---|
| `--fix-perms` | `false` | Interactively sudo chown ~/.config/srv back to the current user when files are root-owned |

## `srv import`

Import site configurations from other tools

Usage:

```
srv import
```

Subcommands:

- `srv import valet` — Translate ~/.valet/Nginx/* into srv commands

## `srv import valet`

Translate ~/.valet/Nginx/* into srv commands

```
Reads every Valet nginx config in --valet-dir (default ~/.valet) and prints
the equivalent srv commands. Recognises PHP/FastCGI sites, reverse proxies,
:88 internal listeners, /path → port splits, regex rewrite locations, and
@fallback prod-mirror locations.

PHP sites are emitted as commented-out 'srv add' lines: srv no longer
manages language runtimes, so each project needs a user-provided
Dockerfile or docker-compose.yml before the add line can be uncommented
and run.

Default mode is dry-run: it only prints. Pass --apply to execute each
command via the same shell.
```

Usage:

```
srv import valet [flags]
```

| Flag | Default | Description |
|---|---|---|
| `--apply` | `false` | Execute the generated srv commands instead of just printing them |
| `--dry-run` | `false` | Explicit no-op alias for the default print-only mode (mutually exclusive with --apply) |
| `--list-sites` | `false` | List discovered Valet sites and exit; build no plan |
| `--reset-decisions` | `false` | Forget previously-recorded skip decisions before running |
| `--skip` | `[]` | Plan-line substring to skip during --apply; repeatable. Skipped lines are added to ~/.config/srv/import-decisions.yml. |
| `--valet-dir` | — | Path to valet config dir (default ~/.valet or ~/.config/valet, whichever has content) |

## `srv info`

Show site info

```
Display detailed information about a site including:
  - Site name and path
  - Domain and type (local/production)
  - Container status
  - SSL certificate status (for local sites)
```

Usage:

```
srv info SITE
```

## `srv install`

Install srv environment

```
Install the srv environment:
  1. Creates the Docker network
  2. Generates Traefik configuration
  3. Starts Traefik container
  4. Installs the daemon service
  5. Starts all registered sites

Use --fresh to remove all existing configuration and start fresh.
```

Usage:

```
srv install [flags]
```

| Flag | Default | Description |
|---|---|---|
| `--email` | — | Let's Encrypt account email for production SSL. Stored on disk after first set; only required once. Pass an empty string to disable production SSL entirely. |
| `--fresh` | `false` | Remove existing configuration and start fresh |
| `--yes`, `-y` | `false` | Assume yes to every confirmable action (firewall open, port conflict auto-fix, valet stop, mkcert CA install retry). Required for non-interactive runs. |

## `srv internal`

Manage the plain-HTTP internal listener (port 88) for a site

```
The 'internal' Traefik entrypoint exposes a site over plain HTTP on port 88
in addition to its normal HTTPS routing. Used for container-to-host calls that
need to skip TLS verification (e.g. server-side fetches from another container
on the same machine).
```

Usage:

```
srv internal
```

Subcommands:

- `srv internal disable` — Disable the internal listener on a site
- `srv internal enable` — Enable the internal listener on a site
- `srv internal list` — List sites with the internal listener enabled

## `srv internal disable`

Disable the internal listener on a site

Usage:

```
srv internal disable SITE
```

## `srv internal enable`

Enable the internal listener on a site

Usage:

```
srv internal enable SITE
```

## `srv internal list`

List sites with the internal listener enabled

Usage:

```
srv internal list
```

## `srv list`

Aliases: `ls`

List all sites

Usage:

```
srv list
```

## `srv logs`

Show site logs

Usage:

```
srv logs [SITE] [flags]
```

| Flag | Default | Description |
|---|---|---|
| `--all`, `-a` | `false` | Multiplex logs from every running site (colour-prefixed) |
| `--follow`, `-f` | `false` | Follow log output |
| `--since` | — | Show logs since timestamp (e.g., 10m, 1h) |
| `--tail` | — | Number of lines to show from the end |

## `srv mcp`

Start the srv MCP server (stdio, or --http for a shared daemon)

```
Run the Model Context Protocol server so AI agents can drive srv the
same way a human does from the CLI — inspecting and mutating sites, proxies,
redirects, routes, and networks.

The tool surface is lazy-loaded: at startup only 'version' and 'srv_activate'
are advertised, so srv costs no context in sessions that never use it. The
agent calls srv_activate(group="read") to unlock inspection + diagnostics, or
srv_activate(group="write") to also unlock mutations; the client refreshes its
tool list automatically.

Transports:

  stdio (default)   One server per client, launched on demand:

      { "mcpServers": { "srv": { "command": "srv", "args": ["mcp"] } } }

  --http            One long-running daemon shared by every MCP client on the
                    host (each Claude Code instance, etc.). Per-request
                    workspace context is taken from the client's MCP roots or an
                    X-Repo-Root header, so a single instance serves all clients:

      srv mcp --http                  # listens on 127.0.0.1:8765/mcp
      srv mcp --http=0.0.0.0:9000     # bind elsewhere (loopback only by default)

                    Then point clients at the URL:

      { "mcpServers": { "srv": { "url": "http://127.0.0.1:8765/mcp" } } }

The HTTP endpoint trusts local processes (loopback, no auth) — it mutates a
privileged Traefik edge, so keep it behind a reverse proxy if bound off-host.
```

Usage:

```
srv mcp [flags]
```

| Flag | Default | Description |
|---|---|---|
| `--http` | — | serve over HTTP at this address instead of stdio (default 127.0.0.1:8765 when given without a value; env SRV_MCP_HTTP_ADDR) |
| `--http-path` | — | HTTP endpoint path (default /mcp; env SRV_MCP_HTTP_PATH) |
| `--tool-timeout` | `10m0s` | max duration for a single tool call before it is cancelled (0 disables; a hung mutation otherwise blocks the shared write lock) |
| `--trusted-origin` | `[]` | browser Origin allowed through cross-origin protection (repeatable; only for browser MCP clients on an off-host bind) |

## `srv metrics`

Manage the optional metrics stack (prometheus + grafana)

```
Prometheus scrapes Traefik's existing /metrics endpoint; Grafana ships with
a pre-wired Prometheus datasource. Both UIs route through Traefik with
mkcert-signed TLS:

    Grafana:     https://grafana.local   (admin / admin)
    Prometheus:  https://prometheus.local

Import a Traefik dashboard in Grafana (dashboard ID 17347) to see request
rates, latency, and error percentages per router.
```

Usage:

```
srv metrics
```

Subcommands:

- `srv metrics disable` — Stop and remove the metrics stack containers
- `srv metrics enable` — Render the metrics compose stack and start containers
- `srv metrics status` — Show whether the metrics stack is running

## `srv metrics disable`

Stop and remove the metrics stack containers

Usage:

```
srv metrics disable
```

## `srv metrics enable`

Render the metrics compose stack and start containers

Usage:

```
srv metrics enable
```

## `srv metrics status`

Show whether the metrics stack is running

Usage:

```
srv metrics status
```

## `srv network`

Manage extra Docker networks attached to a site

```
Attach a site's container(s) to additional external Docker networks so the
in-container process can reach user-managed containers by name.

Typical use: you run MySQL/Redis/Elasticsearch via your own docker-compose
elsewhere, and want srv-managed sites to talk to those containers by their
container hostname (e.g. DB_HOST=mysql01) without falling back to
host.docker.internal.
```

Usage:

```
srv network
```

Subcommands:

- `srv network attach` — Attach a site's container to an external Docker network
- `srv network detach` — Detach a site from an external Docker network
- `srv network list` — List extra Docker networks attached to a site

## `srv network attach`

Attach a site's container to an external Docker network

Usage:

```
srv network attach SITE NETWORK
```

## `srv network detach`

Aliases: `remove`, `rm`

Detach a site from an external Docker network

Usage:

```
srv network detach SITE NETWORK
```

## `srv network list`

List extra Docker networks attached to a site

Usage:

```
srv network list SITE
```

## `srv open`

Open a site in the default browser

```
Open the site's HTTPS URL in the system default browser using xdg-open.
```

Usage:

```
srv open SITE
```

## `srv paths`

Show config paths

```
Display all directories and files used by srv.
```

Usage:

```
srv paths
```

## `srv proxy`

Manage proxy routes

```
Proxy local domains to services running outside of Docker.

This is useful for proxying to local development servers or other
applications running on localhost ports.

Proxies always use local SSL (mkcert) and register with local DNS.
```

Usage:

```
srv proxy
```

Subcommands:

- `srv proxy add` — Add a proxy
- `srv proxy list` — List all proxies
- `srv proxy remove` — Remove a proxy

## `srv proxy add`

Add a proxy

```
Create a proxy from a local domain to a localhost port or Docker container.

Examples:
  # Proxy to a localhost port
  srv proxy add --domain api.test --port 3000
  srv proxy add -d myapp.test -p 8080

  # Proxy to a Docker container (container_name:port)
  srv proxy add --domain api.test --container myapp:3000
  srv proxy add -d myapp.test -c postgres:5432
```

Usage:

```
srv proxy add [flags]
```

| Flag | Default | Description |
|---|---|---|
| `--container`, `-c` | — | Docker container to proxy to (container:port) |
| `--domain`, `-d` | — | Domain name (e.g., api.test) |
| `--fallback` | — | URL to proxy to when the primary upstream returns 5xx (e.g. https://prod.example.com) |
| `--fallback-timeout` | `2s` | Connect timeout to the primary upstream before falling back |
| `--force`, `-f` | `false` | Overwrite existing proxy configuration |
| `--name`, `-n` | — | Proxy name (default: derived from domain) |
| `--port`, `-p` | — | Localhost port to proxy to |
| `--wildcard` | `false` | Also match one-level subdomains (e.g. *.foo.test) |

## `srv proxy list`

Aliases: `ls`

List all proxies

Usage:

```
srv proxy list
```

## `srv proxy remove`

Aliases: `rm`

Remove a proxy

Usage:

```
srv proxy remove NAME
```

## `srv redirect`

Manage HTTP redirects

```
Redirect a local domain to another URL (301 permanent or 302 temporary).

Useful for mapping legacy hostnames to a new canonical URL while preserving the
request path and query string. The redirect is served with a trusted mkcert TLS
certificate so browsers do not warn before following it.
```

Usage:

```
srv redirect
```

Subcommands:

- `srv redirect add` — Add a redirect
- `srv redirect list` — List all redirects
- `srv redirect reload` — Re-apply every redirect-*.yml file
- `srv redirect remove` — Remove a redirect

## `srv redirect add`

Add a redirect

```
Create an HTTP redirect from a local domain to another URL.

The incoming request path and query string are preserved and appended to the
target URL. Both http:// and https:// requests are redirected.

Examples:
  # Redirect jira.example.com to jira.myapp.com (301 permanent)
  srv redirect add --domain jira.example.com --to https://jira.myapp.com

  # Temporary (302) redirect
  srv redirect add -d old.test --to https://new.test --temporary

  # Wildcard subdomains also redirect
  srv redirect add -d legacy.test --to https://new.test --wildcard
```

Usage:

```
srv redirect add [flags]
```

| Flag | Default | Description |
|---|---|---|
| `--dns-only` | `false` | Skip Traefik and TLS; emit a dnsmasq address= record so the source name resolves to the target's IP |
| `--domain`, `-d` | — | Domain to redirect (e.g., old.test) |
| `--force`, `-f` | `false` | Overwrite existing redirect configuration |
| `--name`, `-n` | — | Redirect name (default: derived from domain) |
| `--permanent` | `true` | Use 301 permanent redirect (default) |
| `--temporary` | `false` | Use 302 temporary redirect (overrides --permanent) |
| `--to` | — | Target URL (e.g., https://new.example.com) |
| `--wildcard` | `false` | Also match one-level subdomains (e.g. *.foo.test) |

## `srv redirect list`

Aliases: `ls`

List all redirects

Usage:

```
srv redirect list
```

## `srv redirect reload`

Re-apply every redirect-*.yml file

```
Re-scan redirect-*.yml files and re-render the dnsmasq + Traefik dynamic
configs from them. Useful after hand-editing a redirect yaml or when a DNS-only
redirect's target IP has moved and needs to be re-resolved.
```

Usage:

```
srv redirect reload
```

## `srv redirect remove`

Aliases: `rm`

Remove a redirect

Usage:

```
srv redirect remove NAME
```

## `srv reload`

Re-apply a site's metadata.yml without restarting (unless --restart)

```
Re-applies generated artifacts (nginx.conf, Traefik routing, certs, DNS)
from the site's metadata.yml.

Compose-type sites pick up routing changes via Traefik's file provider
without a restart. Srv-managed sites (static, dockerfile) need a
container restart to apply changes baked into Docker labels — pass
--restart to do that in one step.
```

Usage:

```
srv reload [SITE] [flags]
```

| Flag | Default | Description |
|---|---|---|
| `--all`, `-a` | `false` | Reload all registered sites |
| `--restart` | `false` | Restart the site's container after reload (required for label-based sites to pick up changes) |

## `srv remove`

Aliases: `rm`

Remove a site

```
Stop a site's containers and remove it from srv.
```

Usage:

```
srv remove SITE
```

## `srv restart`

Restart a site

```
Restart a site's containers.

Use --all to restart all registered sites in parallel.
```

Usage:

```
srv restart SITE [flags]
```

| Flag | Default | Description |
|---|---|---|
| `--all`, `-a` | `false` | Restart all sites |
| `--build` | `false` | Rebuild images before restarting |

## `srv route`

Manage extra Traefik routers attached to a site

```
Each route adds a higher-priority router for one site/host that matches
a path prefix or regex and forwards to a separate upstream. Used for
WebSocket splits (e.g. /app → :6001) or regex rewrites (e.g. /videos/...
rewritten and proxied to an S3 gateway).
```

Usage:

```
srv route
```

Subcommands:

- `srv route add` — Attach a route to a site
- `srv route list` — List routes attached to a site
- `srv route remove` — Remove a route from a site

## `srv route add`

Attach a route to a site

Usage:

```
srv route add SITE [flags]
```

| Flag | Default | Description |
|---|---|---|
| `--container` | — | Upstream container (container[:port]) |
| `--id` | — | Stable identifier for this route (auto-derived from --path if omitted) |
| `--pass-range-headers` | `false` | Documentation-only; Traefik forwards Range headers by default |
| `--path` | — | Path prefix to match (e.g. /app); mutually exclusive with --path-regex |
| `--path-regex` | — | Regex matcher for the request path; mutually exclusive with --path |
| `--port` | `0` | Upstream localhost port |
| `--preserve-host` | `true` | Forward the Host header unchanged to the upstream |
| `--priority` | `0` | Override the auto-computed Traefik router priority |
| `--rewrite` | — | Replacement pattern (requires --path-regex) |
| `--url` | — | Upstream URL (http:// or https://) |

## `srv route list`

List routes attached to a site

Usage:

```
srv route list SITE
```

## `srv route remove`

Aliases: `rm`

Remove a route from a site

Usage:

```
srv route remove SITE ID
```

## `srv shell`

Open an interactive shell in a site's container

```
Open an interactive shell (sh) in the primary container for a site.

For static and dockerfile sites the single container is used.

For compose sites the first service container is used; pass --service to
pick a different one.

Examples:
  srv site shell mysite
  srv site shell mysite --service api
```

Usage:

```
srv shell SITE [flags]
```

| Flag | Default | Description |
|---|---|---|
| `--service` | — | Container name or service to shell into |

## `srv start`

Start a site

```
Start a site's containers.

Use --all to start all registered sites in parallel.
```

Usage:

```
srv start SITE [flags]
```

| Flag | Default | Description |
|---|---|---|
| `--all`, `-a` | `false` | Start all sites |
| `--build` | `false` | Rebuild images before starting |

## `srv stop`

Stop a site

```
Stop a site's containers.

Use --all to stop all registered sites in parallel.
```

Usage:

```
srv stop SITE [flags]
```

| Flag | Default | Description |
|---|---|---|
| `--all`, `-a` | `false` | Stop all sites |

## `srv uninstall`

Completely remove srv from the system

```
Completely remove srv and all its components from the system:
  1. Stops and removes the Traefik container
  2. Stops and removes the DNS container
  3. Removes system DNS configuration
  4. Removes the daemon service
  5. Removes the Docker network
  6. Removes the config directory (~/.config/srv)
  7. Removes the srv binary

WARNING: This will remove all srv configuration and registered sites.
Site directories and their contents are NOT removed.

Use --force to skip the confirmation prompt.
```

Usage:

```
srv uninstall [flags]
```

| Flag | Default | Description |
|---|---|---|
| `--force`, `-f` | `false` | Skip confirmation prompt |

## `srv update`

Update Traefik and DNS images

```
Pull the latest Traefik and DNS images and restart the containers.

This ensures you're running the latest versions with security
patches and new features.
```

Usage:

```
srv update
```

## `srv validate`

Validate a site's metadata.yml without applying changes

Usage:

```
srv validate [SITE] [flags]
```

| Flag | Default | Description |
|---|---|---|
| `--all`, `-a` | `false` | Validate all registered sites |

## `srv version`

Show version info

Usage:

```
srv version
```

## `srv volume`

Manage extra host bind-mounts attached to a site

```
Add or remove host directories that should be bind-mounted into the
site's container.

Useful for exposing tooling installed on the host (e.g. ~/.nix-profile),
shared temp directories, or static asset trees that live outside the
project root.

Each mount is specified as HOST:CONTAINER[:ro] where both paths are
absolute. A trailing :ro makes the mount read-only.

Examples:
  srv volume add app ~/.nix-profile:/home/$USER/.nix-profile:ro
  srv volume add app /nix:/nix:ro
  srv volume add app /tmp/uploads:/tmp/uploads
```

Usage:

```
srv volume
```

Subcommands:

- `srv volume add` — Attach a bind-mount to a site
- `srv volume list` — List bind-mounts attached to a site
- `srv volume remove` — Remove a bind-mount from a site by its container target path

## `srv volume add`

Attach a bind-mount to a site

Usage:

```
srv volume add SITE HOST:CONTAINER[:ro]
```

## `srv volume list`

List bind-mounts attached to a site

Usage:

```
srv volume list SITE
```

## `srv volume remove`

Aliases: `rm`, `detach`

Remove a bind-mount from a site by its container target path

Usage:

```
srv volume remove SITE TARGET
```

