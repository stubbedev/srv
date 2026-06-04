# e2e tests

End-to-end suite that drives the real `srv` binary against a real Traefik
booted via docker compose, then makes HTTP requests that travel through
Traefik to assert routing actually works — not just that the right config
files were written.

Each test file is gated with `//go:build e2e` so the default
`go test ./...` skips them.

## Running

```sh
just test-e2e
# or
go test -tags=e2e ./e2e/... -timeout 30m
```

## Requirements

- **docker** (daemon reachable) — Traefik runs in a container.
- **mkcert** — `srv proxy add` always issues a local TLS cert.
- **Free host ports 80/443/88/8080** — Traefik binds them under host
  networking on Linux. The suite self-skips (not fails) when a port is
  already in use, so running `srv` locally won't make the suite red.

When docker or mkcert is missing, or the ports are busy, each test calls
`t.Skip` with the reason. CI runs on a clean host where everything is
available.

## Coverage

| Suite | What it asserts |
|---|---|
| `proxy/` | `srv proxy add` → Traefik file-provider hot-loads the router + mkcert cert → a request to the websecure entrypoint (matched by Host rule) is forwarded to a localhost upstream and returns its body. |

## Harness

`harness/` is a thin, build-tagged helper layer:

- `SkipIfNoDocker` / `SkipIfNoMkcert` / `SkipIfPortsBusy` — environment guards.
- `BuildSrv` — compiles the `srv` binary once per run.
- `NewRoot` — a throwaway `SRV_ROOT`.
- `TraefikUp` — writes config via `traefik.EnsureConfig` and starts only the
  `traefik` service (the compose file also defines an unneeded `dns`
  service), with teardown registered via `t.Cleanup`.
- `RunSrv` — runs the built binary with `SRV_ROOT` pinned.
- `GetHTTPS` / `WaitForHTTPS` — drive Traefik's websecure entrypoint by Host
  rule without DNS (custom dialer to `127.0.0.1:443`, `InsecureSkipVerify`
  for the untrusted-in-CI mkcert cert).

## Adding a new e2e

1. New subdir under `e2e/` with `<name>_test.go` carrying `//go:build e2e`.
2. Start with the guard calls (`harness.SkipIfNoDocker`, `SkipIfNoMkcert`,
   `SkipIfPortsBusy`), then `harness.NewRoot` + `harness.TraefikUp`.
3. Drive `srv` via `harness.RunSrv`, assert via `harness.WaitForHTTPS`.
