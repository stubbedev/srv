# srv

**Traefik, TLS and DNS for every site on your machine.**

srv fronts local and production sites with Traefik: routing, trusted
local certificates (a vendored mkcert), Let's Encrypt certificates for
production domains, and local DNS, managed per site through one CLI. It
does not manage language runtimes: static files are served out of the
box, and for anything else you provide a `Dockerfile` or
`docker-compose.yml` and srv attaches routing on top.

**Documentation: [stubbedev.github.io/srv](https://stubbedev.github.io/srv/)**

```sh
srv install                                        # one-time setup
srv add ~/my-project --domain mysite.test --local  # https://mysite.test
srv add /var/www/myapp --domain example.com        # Let's Encrypt in production
```

## Features

- Static sites served by nginx, or by the srv daemon itself with no
  container per site.
- Proxies to `localhost` ports or Docker containers, with 5xx failover.
- HTTP and DNS-layer redirects with TLS-clean source hostnames.
- Trusted local HTTPS on any TLD, and automatic Let's Encrypt
  certificates for production domains.
- Aliases, wildcard hosts, path and regex routes, and a plain-HTTP
  internal listener.
- A daemon that hot-reloads hand-edited config, and an MCP server for AI
  agents.

## Install

```sh
brew install stubbedev/tap/srv                                                     # Homebrew
curl -fsSL https://raw.githubusercontent.com/stubbedev/srv/master/install.sh | sh  # install script
nix profile install github:stubbedev/srv                                           # Nix
```

srv needs a container runtime with a Docker-compatible API and Compose v2
(Docker, Podman, Colima, OrbStack or Rancher Desktop) and ports 80 and
443. Linux (amd64, arm64, armv7, 386) and macOS (arm64) are supported.
See [Getting started](https://stubbedev.github.io/srv/getting-started/).

## Documentation

- [Getting started](https://stubbedev.github.io/srv/getting-started/)
- [Sites](https://stubbedev.github.io/srv/sites/) and
  [routing](https://stubbedev.github.io/srv/routing/)
- [Configuration](https://stubbedev.github.io/srv/configuration/)
- [AI integration (MCP)](https://stubbedev.github.io/srv/mcp/)
- [Troubleshooting](https://stubbedev.github.io/srv/troubleshooting/)
- [CLI reference](https://stubbedev.github.io/srv/cli/)

The site is built from [`docs/`](docs/) and redeployed on every push to
`master`. Preview it locally with `nix develop --command just docs-serve`.

## License

MIT
