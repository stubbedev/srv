# Host services

App code in a container has its own loopback, so `DB_HOST=127.0.0.1`
points at the app container, not at MySQL on the host. There are three
ways to reach what lives outside the container.

## Services on the host: `host.docker.internal`

Add `host.docker.internal` to the service in `docker-compose.yml` and
point `.env` at it:

```yaml
services:
  app:
    extra_hosts:
      - "host.docker.internal:host-gateway"
```

```env
DB_HOST=host.docker.internal
```

`srv doctor` warns when it finds `*_HOST=127.0.0.1`-style entries in a
container-backed site's `.env`.

## Services in another compose stack: `srv network attach`

Join the site to that stack's network and address its containers by
name:

```sh
srv network attach my-app mysql01_default
srv network detach my-app mysql01_default
srv network list my-app
```

The network must already exist. Run `srv restart SITE` after attaching.

## Host files and binaries: `srv volume add`

Mount what the app needs, such as `ffmpeg`, a shared temp directory or a
Nix profile:

```sh
srv volume add my-app ~/.nix-profile:/home/$USER/.nix-profile:ro
srv volume add my-app /nix:/nix:ro
srv volume list my-app
srv volume remove my-app /nix
```

Mount paths must be absolute (`~` is expanded), and `/app` is reserved
for the project itself.
