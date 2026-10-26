# Sites

A site is a project directory srv serves under one or more hostnames.
`srv add` registers it and generates its routing.

```sh
srv add PATH --domain HOST [flags]
```

The site type is detected from the directory:

1. **Compose**: the path contains a `docker-compose.yml`.
2. **Dockerfile**: the path contains a `Dockerfile`.
3. **Static**: anything else, served as static files.

A project that needs a runtime (PHP, Node, Ruby, Python, …) but has
neither file is served as static files; add a `Dockerfile` or
`docker-compose.yml` to run application code. `--type` forces a type.

```sh
srv add ./dist --domain docs.test --local                    # static (auto-detected)
srv add ./slides --domain slides.local --local --daemon      # static, no site container
srv add ./app --domain api.test --local --service backend --port 3000   # compose service + port
srv add ./mixed-project --domain x.test --local --type static           # ignore the Dockerfile
```

`--local` issues a certificate from srv's local CA; without it, Traefik
requests one from Let's Encrypt. Every flag is in the
[CLI reference](cli.md#srv-add).

## Static sites

For directories without a `docker-compose.yml` or `Dockerfile`, srv
generates an `nginx:alpine` container serving the directory with:

- SPA routing (fallback to `/index.html`, `--spa`) and asset caching
  (`--cache`), both on by default
- optional permissive CORS headers (`--cors`) and a custom `404.html`
- hidden-file and sensitive-extension blocks (`.env`, `.git`,
  `.htaccess`, …)
- gzip compression and standard security headers

### Served by the daemon

With `--daemon` there is no container: the daemon's embedded HTTP server
hosts the site on `127.0.0.1:15380`, multiplexing daemon-served sites by
`Host` header with the same semantics. `docker compose` is never invoked
(start and stop just add or remove the Traefik route file),
`srv logs SITE` shows per-request access logs, and `--port` and
`--volume` do not apply.

## Dockerfile and compose sites

A project root with a `Dockerfile` is a Dockerfile site; one with a
`docker-compose.yml` is a compose site. srv attaches Traefik routing and
never generates or edits your files. For a compose file with several
services, `--service` picks the one to route to and `--port` the port it
listens on inside the container.

### Example: Laravel with local HTTPS

```yaml
# docker-compose.yml
services:
  app:
    image: dunglas/frankenphp:alpine
    working_dir: /app
    volumes:
      - .:/app
    expose:
      - "80"
    environment:
      SERVER_NAME: ":80"   # Traefik terminates TLS; the container speaks plain HTTP
    extra_hosts:
      - "host.docker.internal:host-gateway"
```

```env
# .env
APP_URL=https://mylaravel.test
ASSET_URL=https://mylaravel.test
TRUSTED_PROXIES=*
```

`TRUSTED_PROXIES=*` makes Laravel respect `X-Forwarded-Proto: https`
from Traefik; without it, Laravel generates `http://` URLs and the
browser reports mixed content.

```sh
srv add . --domain mylaravel.test --local
```

srv detects the compose file, issues a local certificate, registers the
hostname with its DNS server, attaches routing labels to the `app`
service, and runs `docker compose up -d`. For MySQL or Redis running on
the host, point `.env` at `host.docker.internal`; see
[Host services](host-services.md).

## Managing sites

```sh
srv list                 # every site, its domain, type and status
srv info SITE            # one site in detail
srv start | stop | restart SITE
srv logs [SITE]
srv shell SITE           # interactive shell in the site's container
srv open SITE            # open in the default browser
srv remove SITE          # stop it and delete its routing, certificate and DNS entries
```
