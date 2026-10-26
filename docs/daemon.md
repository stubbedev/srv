# Daemon

The srv daemon is a user service (a systemd user unit or a launchd agent)
that runs srv's shared infrastructure and keeps sites in sync. `srv
install` installs it.

- **Hot reload.** It watches every
  `~/.config/srv/sites/<name>/metadata.yml` and re-applies changes within
  ~300 ms, debounced across editor saves: certificates refresh, DNS
  updates and routing regenerates. Hand-edit the YAML; no restart needed.
- **Docker events.** Containers started outside srv (a bare
  `docker compose up`) are connected to the srv network.
- **Embedded DNS** for every registered local domain on
  `127.0.0.1:15353`.
- **Embedded static server** for `--daemon` sites on `127.0.0.1:15380`.
- **Single instance.** A lock on `daemon.lock` makes a second daemon
  refuse to start, so one daemon serves every site and container.

```sh
srv daemon start      # --foreground to stay attached, --no-watch to disable hot reload
srv daemon stop | restart | status | logs
srv daemon install    # start at login (systemd user service / launchd agent)
srv daemon uninstall
```

## Manual reloads

When the watcher is off, or to force a change through:

```sh
srv reload SITE             # re-apply one site's metadata
srv reload --all            # every site
srv reload SITE --restart   # also restart the container (for changes baked into labels)
srv validate SITE           # check metadata.yml without applying it
```

## Doctor

```sh
srv doctor [--fix-perms]
```

`srv doctor` checks the user config, the container engine and Compose v2,
the firewall, port availability (80, 443, 8080), the srv Docker network,
the Traefik container, srv's DNS server and whether the system resolver
routes to it, local certificate validity and expiry, every site's
metadata, `.env` files of container sites that point at the host
loopback, and the ownership of `~/.config/srv`. `--fix-perms` runs
`sudo chown -R` to repair root-owned files.
