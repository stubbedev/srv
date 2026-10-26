# Getting started

## Requirements

- **A container runtime** with a Docker-compatible API and Compose v2:
  Docker, Podman, Colima, OrbStack or Rancher Desktop (see
  [Container runtimes](container-runtimes.md)). Traefik runs as a
  container even when sites are served by the daemon.
- **Ports 80 and 443** for Traefik. Rootless engines need the
  unprivileged port floor lowered; see
  [rootful Podman](container-runtimes.md#podman).

Nothing else. mkcert is vendored in (same `CAROOT` layout as the
standalone tool), and DNS is an embedded resolver in the daemon on
`127.0.0.1:15353`: no dnsmasq container, no privileged port.

**Platforms:** Linux (amd64, arm64, armv7, 386) and macOS (arm64).

## Install

=== "Homebrew"

    ```sh
    brew install stubbedev/tap/srv
    ```

    The formula covers linux amd64/arm64 and darwin arm64. Don't also
    run `brew services start srv`: `srv install` sets up its own daemon
    service, and two supervisor units race over the same job.

=== "Install script"

    ```sh
    curl -fsSL https://raw.githubusercontent.com/stubbedev/srv/master/install.sh | sh
    ```

=== "Nix"

    ```sh
    nix profile install github:stubbedev/srv
    ```

=== "Release tarball"

    Download the tarball for your platform from the
    [latest release](https://github.com/stubbedev/srv/releases/latest),
    extract it, and put `srv` on your `PATH`. armv7 and 386 are only
    available this way or through the install script.

## Set up the machine

```sh
srv install
```

`srv install` creates srv's Docker network, writes the Traefik
configuration and starts Traefik, installs the daemon as a user service
(systemd or launchd), and starts every registered site. It also points
your system resolver (systemd-resolved, NetworkManager or macOS) at srv's
DNS server with a single sudo prompt; on systemd-resolved it checks that
`/etc/resolv.conf` goes through resolved's stub, and `--yes` re-points it
when it does not. On a production host it asks once for the Let's
Encrypt email address (`--email`).

`srv doctor` checks the whole setup and prints the fix for anything
missing.

## Serve a site locally

```sh
srv add ~/my-project --domain mysite.test --local            # nginx container
srv add ~/my-project --domain mysite.test --local --daemon   # no container
```

Visit `https://mysite.test`. Restart the browser after adding the first
local site: the CA is installed system-wide, but browsers only pick it
up on restart.

## Serve a site in production

```sh
srv add /var/www/myapp --domain example.com
```

Traefik provisions the Let's Encrypt certificate automatically. The
domain's DNS must point at the server and ports 80 and 443 must be open.

## Next steps

- [Sites](sites.md): static, Dockerfile and compose projects.
- [Routing](routing.md): proxies, redirects, aliases, routes.
- [Daemon](daemon.md): hot reload and the embedded servers.
