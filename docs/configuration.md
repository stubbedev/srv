# Configuration

Everything srv manages is described by YAML files under `~/.config/srv/`.
srv never writes into your project directories. The CLI writes these
files and the [daemon](daemon.md) re-applies any change to them within
~300 ms, so editing a file by hand is as good as running the command.

| File | What it describes |
|---|---|
| `~/.config/srv/config.yml` | user settings: container engine, upstream DNS |
| `~/.config/srv/sites/<name>/metadata.yml` | one site: domains, type, routes, mounts, networks |
| `~/.config/srv/proxies/proxy-<name>.yml` | one proxy |
| `~/.config/srv/traefik/conf/redirect-<name>.yml` | one redirect |

Every field is listed in the [config file reference](config-reference.md),
generated from the same Go structs as the published
[JSON Schemas](https://github.com/stubbedev/srv/tree/master/schemas), so
it always matches the binary.

## Example

A compose site on a local domain, `sites/app/metadata.yml`:

```yaml
type: compose
domains: [app.example.test]
is_local: true
```

`srv validate app` checks the file without applying it.

## User config

```yaml
# ~/.config/srv/config.yml
container_engine: podman            # see Container runtimes
upstream_dns: [1.1.1.1, 9.9.9.9#53] # resolvers for everything srv doesn't own
```

`container_engine` is covered in
[Container runtimes](container-runtimes.md). `upstream_dns` defaults to
Google DNS (8.8.8.8 and 8.8.4.4).

## All paths

`srv paths` prints the main ones.

| Path | Description |
|---|---|
| `~/.config/srv/config.yml` | user config |
| `~/.config/srv/daemon.log` | daemon and access log |
| `~/.config/srv/traefik/` | Traefik compose file and static config |
| `~/.config/srv/traefik/conf/` | dynamic Traefik routing configs |
| `~/.config/srv/traefik/conf/site-<name>.yml` | site routing (compose and daemon-served sites) |
| `~/.config/srv/traefik/conf/routes-<name>.yml` | per-site extra routes (`srv route`) |
| `~/.config/srv/traefik/conf/proxy-<name>.yml` | proxy routing (`srv proxy`) |
| `~/.config/srv/traefik/conf/redirect-<name>.yml` | redirects (`srv redirect`) |
| `~/.config/srv/traefik/conf/proxy-metrics.yml` | `grafana.local` / `prometheus.local` routers |
| `~/.config/srv/traefik/dnsmasq.conf`, `dnsmasq.hosts/` | DNS zone files (dnsmasq format, watched by the embedded server) |
| `~/.config/srv/traefik/certs/` | Let's Encrypt certificates (`acme.json`) |
| `~/.config/srv/sites/<name>/metadata.yml` | site metadata, the source of truth |
| `~/.config/srv/sites/<name>/certs/` | local certificates |
| `~/.config/srv/sites/<name>/docker-compose.yml`, `nginx.conf` | generated for static and Dockerfile sites |
| `~/.config/srv/sites/<name>/.reload-state` | hash of the last applied metadata (hot-reload short-circuit) |
| `~/.config/srv/proxies/` | proxy metadata (`proxy-<name>.yml`) |
| `~/.config/srv/metrics/` | Prometheus and Grafana compose stack |
