# Config file reference

> Auto-generated from the Go structs behind the published JSON Schemas by `go run ./cmd/gen-docs`.
> Run `just sync-docs` after changing it to refresh.

Every file srv reads, field by field. See
[Configuration](configuration.md) for how the files fit together.

## Site — `metadata.yml`

_Path: `~/.config/srv/sites/<name>/metadata.yml`_

| Field | Type | Required | Description |
|---|---|---|---|
| `schema_version` | `integer` | no | metadata.yml schema version (1 = current). |
| `type` | `string` | no | Site runtime type. |
| `domains` | `array<string>` | no | All hostnames; the first entry is canonical. |
| `project_path` | `string` | no | Absolute path to the project on disk. |
| `service_name` | `string` | no | Container name used for Traefik routing. |
| `compose_service_name` | `string` | no | docker-compose service name (for compose commands). |
| `profile` | `string` | no | docker-compose profile (if the service uses profiles). |
| `port` | `integer` | no | Port the service listens on inside the container. |
| `is_local` | `boolean` | no | Whether to use a locally-issued (mkcert) SSL certificate. |
| `wildcard` | `boolean` | no | Match apex + one-level subdomains (*.example.com). |
| `network_name` | `string` | no | Docker network the site joins. |
| `extra_networks` | `array<string>` | no | Extra external Docker networks the site joins (for reaching user-managed containers like mysql01). |
| `volumes` | `array<object>` | no | Extra host bind-mounts attached to the site's container (e.g. ~/.nix-profile, TEMP dirs). |
| `listeners` | `array<string>` | no | Extra Traefik entrypoints (e.g. 'internal' for plain HTTP on :88). |
| `routes` | `array<object>` | no | Extra Traefik routers (path-prefix / regex-rewrite splits). |
| `spa` | `boolean` | no | Single-page-app mode (fall back to /index.html). |
| `cache` | `boolean` | no | Emit aggressive caching headers for static assets. |
| `cors` | `boolean` | no | Emit permissive CORS headers. |
| `daemon_served` | `boolean` | no | Serve the static files from the srv daemon itself — no nginx container and no Docker; one embedded server hosts every daemon-served site. |
| `dockerfile_port` | `integer` | no | Port discovered from the Dockerfile EXPOSE directive. |

## Proxy — `proxy-<name>.yml`

_Path: `~/.config/srv/proxies/proxy-<name>.yml`_

| Field | Type | Required | Description |
|---|---|---|---|
| `schema_version` | `integer` | no | metadata.yml schema version (1 = current). |
| `name` | `string` | no | Proxy name; also the basename of the generated proxy-<name>.yml. |
| `domains` | `array<string>` | no | All hostnames routed to this proxy; the first entry is canonical. |
| `wildcard` | `boolean` | no | Match apex + one-level subdomains (*.example.com); local proxies only. |
| `is_local` | `boolean` | no | Use a locally-issued (mkcert) SSL certificate instead of Let's Encrypt. |
| `port` | `integer` | no | Port of the primary upstream (a localhost service the daemon's embedded components dial); 0 for container-primary proxies. |
| `routes` | `array<object>` | no | Extra Traefik routers (path-prefix / regex-rewrite splits) attached via `srv route`. |
| `fallback_url` | `string` | no | Remote URL that 5xx responses are re-proxied to (--fallback); empty when the proxy has no fallback. Rendered as Traefik's native failover service. |
| `fallback_timeout` | `string` | no | Connect timeout to the primary upstream before Traefik fails over (--fallback-timeout). |
| `fallback_port` | `integer` | no | Deprecated: unused. srv drops it from existing metadata when re-rendering. |

## DNS-only redirect

_Path: `~/.config/srv/traefik/conf/redirect-<name>.yml`_

| Field | Type | Required | Description |
|---|---|---|---|
| `dns` | `object` | no | source → target hostname pair for the DNS-layer redirect. |

## User config — `config.yml`

_Path: `~/.config/srv/config.yml`_

| Field | Type | Required | Description |
|---|---|---|---|
| `container_engine` | `string` | no | Container runtime srv drives. Omit it (or set auto) to detect one; name it to pin. Requires a Docker-compatible API endpoint and Compose v2. |
| `upstream_dns` | `array<string>` | no | Upstream DNS resolvers; each entry is an IP address with an optional #port suffix. Defaults to Google DNS (8.8.8.8 and 8.8.4.4). |
