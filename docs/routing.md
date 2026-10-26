# Routing

Besides sites, srv routes hostnames to dev servers and containers it does
not manage, redirects hostnames elsewhere, and attaches extra routes to a
site.

## Proxies

A proxy routes a hostname to a `localhost` port or a Docker container:

```sh
srv proxy add --domain api.test --port 3000              # local dev server
srv proxy add --domain db.test --container postgres:5432

srv proxy list
srv proxy remove api.test
```

All proxies use a local certificate and register with srv's DNS server.

### 5xx fallback

`--fallback` re-proxies to a remote URL when the primary upstream
returns a 5xx, using Traefik's native failover:

```sh
srv proxy add --domain myapp.com --port 3001 \
  --fallback https://myapp.com --fallback-timeout 2s
```

## Redirects

`srv redirect add` answers a hostname with a 301 (default) or 302
(`--temporary`). The request path and query are appended to the target,
so `https://jira.example.com/browse/X?y=1` lands on
`https://jira.myapp.com/browse/X?y=1`. A local certificate is provisioned
for the source hostname so browsers follow the redirect without a TLS
warning.

```sh
srv redirect add --domain jira.example.com --to https://jira.myapp.com
srv redirect add -d old.test --to https://new.test --temporary
srv redirect add -d legacy.test --to https://new.test --wildcard

srv redirect list
srv redirect remove jira-example-com
```

### DNS-layer redirects

`--dns-only` skips Traefik and TLS entirely: srv's DNS server pins the
source hostname to the target's resolved IP.

```sh
srv redirect add --domain jira.example.com.test --to jira.myapp.com --dns-only
```

The client never sees an HTTP redirect. It connects straight to the
target's IP with the original `Host` header, so whether the visible URL
changes depends on the backend. When the target's IP changes, run
`srv redirect reload` to resolve it again.

| | `--dns-only` | default (HTTP 301/302) |
|---|---|---|
| Browser URL bar | depends on the backend | always switches to the target |
| Path and query preserved | yes | yes |
| Works if the target is unreachable | no, TCP fails | yes, the redirect is the response |

## Aliases

Serve one site under many hostnames, for example a multi-tenant
application where every tenant maps to the same project:

```sh
srv add ~/git/work/myapp --domain myapp.test \
  --alias cms-myapp.test --alias jira.example.com.test --local --wildcard

srv alias add myapp jira-staging.test
srv alias remove myapp jira-staging.test
srv alias list myapp
```

One certificate covers every alias; all hostnames are registered with the
DNS server and matched by one Traefik `Host` rule. `--wildcard` also
matches one level of subdomains (`*.myapp.test`) on local sites.

## Internal plain-HTTP listener

Code inside a container often calls `https://myapp.test` from a client
that does not trust the local CA. srv can expose a second Traefik
entrypoint on `:88` serving the same routers without TLS:

```sh
srv add ./my-app --domain app.test --local --internal-http   # at add time
srv internal enable app.test                                  # or later
srv internal disable app.test
srv internal list
```

`https://app.test` and `http://app.test:88` then reach the same backend.

## Per-site routes

Extra Traefik routers send different paths of a site or proxy to
different upstreams:

```sh
# Path-prefix split (e.g. a WebSocket server on /app)
srv route add myapp.test --path /app --port 6001

# Regex rewrite
srv route add myapp.test \
  --path-regex '^/videos/([^/]+)/(.+)$' \
  --rewrite '/abs/videos/$1/$2' \
  --port 9080 --preserve-host

# Upstreams: a localhost port, container[:port], or an http(s):// URL
srv route add api.test --path /v2 --container backend-v2:3000
srv route add docs.test --path /sdk --url https://sdk.example.com

srv route list myapp.test
srv route remove myapp.test app
```

Routes are stored in the site's `metadata.yml` under `routes:` and
rendered to `~/.config/srv/traefik/conf/routes-<name>.yml`.
