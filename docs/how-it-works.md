# How it works

- **Traefik** (pinned to `v3.7`) terminates TLS on :80 and :443 and
  routes by `Host` rule from generated file-provider configs in
  `~/.config/srv/traefik/conf/`. Its dashboard is at
  `http://127.0.0.1:8080/dashboard/`.
- **Local TLS** (`--local`): certificates from srv's vendored
  [mkcert](https://github.com/FiloSottile/mkcert) CA, trusted by the
  system and browsers.
- **Production TLS**: Let's Encrypt through Traefik's ACME resolver,
  renewed automatically.
- **DNS**: the daemon's embedded server answers every registered local
  domain with `127.0.0.1` (any TLD) on `127.0.0.1:15353` and forwards
  everything else upstream (`upstream_dns`). `srv install` points the
  system resolver at it. Zone files stay in hand-editable dnsmasq format,
  watched and re-read on `SIGHUP`.
- **The daemon** watches site metadata and Docker events and hosts the
  DNS and static servers; see [Daemon](daemon.md).
- **State** is plain YAML under `~/.config/srv/`; see
  [Configuration](configuration.md#all-paths).
