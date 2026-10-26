# srv

**Traefik, TLS and DNS for every site on your machine.**

srv fronts local and production sites with Traefik: routing, trusted
local certificates (a vendored mkcert), Let's Encrypt certificates for
production domains, and local DNS, managed per site through one CLI.

```sh
srv install                                        # one-time setup
srv add ~/my-project --domain mysite.test --local  # https://mysite.test
```

srv does not manage language runtimes. Static files are served out of
the box; for anything else, provide a `Dockerfile` or
`docker-compose.yml` and srv attaches routing on top.

## Highlights

- **Static sites** served by nginx with sensible defaults (SPA fallback,
  caching, CORS, hidden-file blocks), or by the srv daemon itself with no
  per-site container.
- **Proxies** to `localhost` ports or Docker containers, with optional
  5xx failover to a remote URL.
- **Redirects** at the HTTP or DNS layer, with TLS-clean source hostnames.
- **Trusted local HTTPS** for `*.test`, `*.local` and any other TLD,
  without browser warnings.
- **Let's Encrypt** certificates provisioned and renewed automatically for
  production domains.
- **Aliases, wildcard hosts, path and regex routes**, and a plain-HTTP
  internal listener for containers that don't trust the local CA.
- **A daemon** that hot-reloads hand-edited config within ~300 ms and
  serves DNS on an unprivileged port.
- **An MCP server** so AI agents drive srv the same way the CLI does.

## When to use srv

srv is an edge layer: Traefik, certificate issuance, DNS and a daemon. It
is not a PaaS: no runtime, no buildpack, no application manager.

Use it when one machine fronts several sites, applications,
`localhost:PORT` dev servers and redirects behind a single TLS edge,
particularly a multi-tenant application served under many hostnames (one
SAN certificate, one router, many `Host` rules).

For a single project, Caddy or FrankenPHP with a self-signed certificate
is simpler. srv also overlaps with, rather than complements, an existing
reverse proxy (nginx-proxy, Caddy, bare Traefik, Kubernetes Ingress).

## Where to start

| If you want to… | Read |
|---|---|
| install srv and serve a first site | [Getting started](getting-started.md) |
| serve a static, Dockerfile or compose project | [Sites](sites.md) |
| proxy a dev server, redirect a host, add routes | [Routing](routing.md) |
| reach host databases from a container | [Host services](host-services.md) |
| hand-edit config | [Configuration](configuration.md) |
| connect an AI agent | [AI integration](mcp.md) |
| fix something | [Troubleshooting](troubleshooting.md) |

srv is MIT-licensed. Source and issues:
[github.com/stubbedev/srv](https://github.com/stubbedev/srv).
