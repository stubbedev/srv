# e2e tests

End-to-end suite that drives the real `srv` binary against a real Traefik
booted via docker compose, then makes HTTP requests that travel through
Traefik to assert routing actually works — not just that the right config
files were written.

Each test file is gated with `//go:build e2e` so the default
`go test ./...` skips them.

## Running

```sh
just test-e2e            # docker leg
just test-e2e podman     # podman leg (rootful socket)
just test-e2e docker -run TestSrvUnitLifecycle
```

The suite never runs on the host. `e2e/run.sh` runs `go test` inside the
`Dockerfile.dev` image, which drives the host engine through its mounted
socket, so every container a test starts (Traefik, the systemd host) is a
sibling on the host:

- **Host networking.** Traefik binds the host's ports and dials upstreams
  the tests serve on the host loopback, so the test container shares the
  host network.
- **Path parity.** srv hands `SRV_ROOT` and project paths to the engine as
  bind mounts, which the engine resolves on the host. The repository is
  mounted at its own path and `TMPDIR` points at `.e2e/tmp` inside it, so
  every `t.TempDir()` exists at the same path in both places.

Anything that touches a service manager (`srv daemon install`, `uninstall`,
`start`, `stop`) runs in a disposable systemd container
(`harness.NewSystemdHost`), never against the host's `systemctl`.

## Requirements

- **A Linux host** with docker (the runner) and the engine under test.
- **Free host ports 80/443/88/8080** for the routing suites. They self-skip
  (not fail) when a port is already in use, so running `srv` locally won't
  make the suite red.

## Coverage

The Traefik-booting suites live in one directory (`e2e/routing`, one test
package) on purpose: `go test` runs packages in parallel, and every one of
these boots Traefik with the same fixed container name and host ports —
parallel packages would race. Within the package, tests run sequentially.

| Suite | What the package asserts |
|---|---|
| `routing/proxy` | `srv proxy add` → Traefik file-provider hot-loads the router + mkcert cert → a request to the websecure entrypoint (matched by Host rule) is forwarded to a localhost upstream and returns its body. |
| `routing/site` | `srv add` on a static project → same chain, site leg. |
 `routing/fallback` | `srv proxy add --fallback` → Traefik's native failover service fronts the primary; when the primary dies (dial failure or 5xx), the same request returns the fallback's body with no Traefik reload and no extra proxy or container. |
| `daemonservice/` | `srv daemon install/start/stop/status/uninstall` against a real `systemctl --user` in a systemd container: srv's own unit, the unit `brew services start srv` writes (driven, never duplicated, never removed), and both at once (reported, and resolved by `srv daemon uninstall`). |
| `dns/` | `srv dnsd` (the embedded server the daemon hosts) answers registered domains from the generated zone files — exact, wildcard at every depth — and live-reloads on a zone-file rewrite, no signal or restart. Container-free, safe to run in parallel with anything. |

## Harness

`harness/` is a thin, build-tagged helper layer:

- `SkipIfNoEngine` / `SkipIfNoMkcert` / `SkipIfPortsBusy` — environment guards.
- `NewSystemdHost` — a throwaway container with systemd as PID 1 and the
  built srv inside; `Run`/`MustRun` execute as a user whose
  `systemctl --user` works.
- `BuildSrv` — compiles the `srv` binary once per run.
- `NewRoot` — a throwaway `SRV_ROOT`.
- `TraefikUp` — writes config via `traefik.EnsureConfig` and starts the
  `traefik` service, with teardown registered via `t.Cleanup`.
- `RunSrv` — runs the built binary with `SRV_ROOT` pinned.
- `GetHTTPS` / `WaitForHTTPS` — drive Traefik's websecure entrypoint by Host
  rule without DNS (custom dialer to `127.0.0.1:443`, `InsecureSkipVerify`
  for the untrusted-in-CI mkcert cert).

## Adding a new e2e

1. New subdir under `e2e/` with `<name>_test.go` carrying `//go:build e2e`.
2. Start with the guard calls (`harness.SkipIfNoEngine`, `SkipIfNoMkcert`,
   `SkipIfPortsBusy`), then `harness.NewRoot` + `harness.TraefikUp`.
3. Drive `srv` via `harness.RunSrv`, assert via `harness.WaitForHTTPS`.
