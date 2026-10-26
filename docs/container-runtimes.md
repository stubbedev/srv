# Container runtimes

srv needs a **Docker-compatible API endpoint** from the runtime (shared
network, container inspection, image pulls; Traefik watches labels
through it) plus **Compose v2**, whose `com.docker.compose.*` labels srv
finds containers by. `podman compose` delegates to Compose v2 and works;
the Python `podman-compose` does not. `srv doctor` checks both.

## Detection

With no `container_engine` set, srv probes for a usable runtime (CLI on
`$PATH` and API socket present) in this order:

| Runtime | CLI | Socket probed |
|---|---|---|
| `docker` | `docker` | `/var/run/docker.sock`, `~/.docker/run/docker.sock` |
| `orbstack` | `docker` | `~/.orbstack/run/docker.sock` |
| `colima` | `docker` | `~/.colima/default/docker.sock`, `~/.colima/docker.sock` |
| `rancher-desktop` | `docker` | `~/.rd/docker.sock` |
| `podman` | `podman` | `$XDG_RUNTIME_DIR/podman/podman.sock`, `/run/podman/podman.sock` |

If nothing is detected, srv falls back to Docker. To pin a runtime:

```yaml
# ~/.config/srv/config.yml
container_engine: podman   # auto (default), docker, colima, orbstack, rancher-desktop
```

## `DOCKER_HOST`

An exported `DOCKER_HOST` takes precedence and is used verbatim. That is
also how to reach anything not in the table: a remote daemon, a rootless
socket in a non-standard location, a Docker-API shim. Otherwise srv
exports `DOCKER_HOST` from the runtime it resolved, so the Docker SDK and
Compose v2 follow the same choice, and bind-mounts that socket into
Traefik.

## Podman

**Rootful Podman is the supported Podman target.** Traefik binds :80 and
:443, below the kernel's unprivileged port floor, so rootless Podman
refuses them until the host lowers it:

```sh
sudo sysctl -w net.ipv4.ip_unprivileged_port_start=53
```

## Unsupported

**nerdctl and Finch** are not supported: containerd's socket is a
different protocol, not a Docker-compatible API. If a Docker-API shim is
available, point `DOCKER_HOST` at it.

`srv doctor` reports which runtime it resolved and how, its endpoint, its
Compose implementation, and the port floor when it applies.
