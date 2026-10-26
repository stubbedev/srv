# MCP tools

> Auto-generated from the live MCP server by `go run ./cmd/gen-docs`.
> Run `just sync-docs` after changing it to refresh.

Every tool `srv mcp` exposes, by tier. Only the `core` tools are
advertised until `srv_activate` unlocks a tier; see
[AI integration](mcp.md).

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
