# CLI ergonomics proposal

Status: discussion / not implemented.

This is a written proposal — none of it has been merged. Each item lists
the change, the reasoning, and the breakage profile so we can decide
piece-by-piece.

## Snapshot

`srv` currently exposes **63 commands** across a flat top level and seven
sub-namespaces. The full reference lives at [docs/cli.md](../cli.md).

Top-level shape today:

```
srv add | remove | start | stop | restart | reload | list | info | open | logs |
        shell | validate | doctor | install | uninstall | update | paths | version
srv alias    {add|remove|list}
srv internal {enable|disable|list}
srv route    {add|remove|list}
srv volume   {add|remove|list}
srv network  {attach|detach|list}
srv proxy    {add|remove|list}
srv redirect {add|remove|list|reload}
srv daemon   {start|stop|restart|status|install|uninstall|logs}
srv metrics  {enable|disable|status}
srv import   {valet}
```

## Observation 1 — five site-scoped namespaces share an awkward seam

`alias`, `internal`, `route`, `volume`, `network` are all per-site operations
that take `SITE` as their first positional. They live at the top level today,
which means the user has to remember a flat list of nouns rather than the
pattern "every site has these knobs."

### Proposal 1a (additive)

Add `srv site` as an alias namespace that mirrors the five existing ones:

```
srv site alias    add|remove|list   →  delegates to srv alias    *
srv site internal enable|disable|…  →  delegates to srv internal *
srv site route    add|remove|list   →  delegates to srv route    *
srv site volume   add|remove|list   →  delegates to srv volume   *
srv site network  attach|detach|…   →  delegates to srv network  *
```

Both spellings work; old scripts unaffected. Discoverability improves because
`srv site --help` lists every site-level knob in one place.

### Proposal 1b (breaking, would require a deprecation window)

Make `srv site …` the canonical path and hide the top-level aliases from
`--help`. Old scripts keep working for one or two releases; we then remove
them.

Breakage: any user with `srv alias add` baked into muscle memory or
shell aliases would need to relearn. Probably worth doing only in a 1.0
push.

## Observation 2 — `srv internal` is a confusing top-level name

`srv internal enable foo.test` toggles whether `foo.test` is exposed on
Traefik's plain-HTTP `:88` entrypoint for container-to-host calls. The name
"internal" sounds like srv's own internals or a hidden command.

### Proposal 2

Rename the surface to something domain-bound. Options:

- `srv site http-port` (matches what it controls — adds the second HTTP listener)
- `srv site insecure-http`
- `srv site loopback`

The CLI would gain the new name and `srv internal …` would become a hidden
alias until a major release.

Breakage: pure additive if we keep `srv internal` as a hidden alias; the
README + cli.md need updating.

## Observation 3 — `srv add --start` semantics are ambiguous

`srv add` does not have a `--start` flag today; the description in some
older docs mentions one. Worth checking the parity between behaviour and
documentation in one pass. (Probably a docs bug, not a CLI change.)

## Observation 4 — `--type` accepts free strings; auto-detect is the default

Today `srv add` flags advertise `--type static|dockerfile|compose`, but
detection auto-selects based on what's in the project root. The flag is a
manual override.

### Proposal 4

Rename `--type` to `--force-type` and have it accept only the three known
strings (cobra StringEnum). The current `--type` becomes a hidden alias.

Breakage: scripts that pass `--type` keep working via the alias. The
positive case is clarity: in the help text, `--force-type` says "this is
an override," whereas `--type` reads like a required choice.

## Observation 5 — `--container NAME:PORT` and `--port PORT` overlap

`srv proxy add --domain api.test --port 3000` vs
`srv proxy add --domain db.test --container postgres:5432` are
mutually exclusive but expressed as separate flags. Forgetting which to
pass is an easy mistake.

### Proposal 5 (additive)

Accept a single `--upstream` flag that auto-detects the shape:

```
--upstream 3000                          → localhost:3000
--upstream postgres:5432                 → container postgres:5432
--upstream https://api.example.com/v2    → URL passthrough
```

This matches the `routes` upstream shape already supported in `srv route`.
The existing `--port` / `--container` flags stay as hidden aliases for
backwards compatibility.

## Observation 6 — `srv update` is overloaded

`srv update` today is about pulling new Traefik / dnsmasq container images.
A user might reasonably expect it to update the srv binary itself
(via brew / install.sh).

### Proposal 6

Rename to `srv update images` (subcommand), and reserve `srv update` as a
parent for future maintenance verbs (e.g. `srv update self` if we ever
ship a self-updater).

Breakage: `srv update` keeps working as a hidden alias for `srv update images`.

## Observation 7 — output verbs are consistent; that's worth preserving

Every list operation is `srv X list` (not `srv X ls`); every removal is
`srv X remove` (not `srv X rm` or `srv X delete`). Don't break this — any
new verbs added should match.

## Observation 8 — JSON output coverage is partial

`--format json` is a global flag, but only some commands honour it. Worth
auditing which list/info commands currently emit JSON and which still
print text-only tables. The MCP server work (separate item) depends on
JSON output being available wherever a UI table exists.

### Proposal 8

Treat "every `list`/`info` command must support `--format json`" as a
hard rule going forward. Add tests that assert non-empty JSON output for
each. Fill the gaps as we find them.

## What's not in this proposal

- Removing top-level commands. They're well-known and the muscle memory
  is real.
- Renaming `srv add` to `srv site add` as the canonical path. The
  shortcut is the whole point.
- Touching `srv proxy` / `srv redirect`. They're already namespaced
  consistently with each other.
- Touching `srv daemon` / `srv metrics`. Both already self-contained.

## Sequencing

If we proceed, the order I'd suggest:

1. **Proposal 8** (JSON parity) — small audit + tests; unblocks MCP.
2. **Proposal 5** (`--upstream`) — strictly additive, easy win.
3. **Proposal 4** (`--force-type`) — alias-only, no breakage.
4. **Proposal 6** (`srv update images`) — alias-only.
5. **Proposal 1a** (`srv site …` aliases) — alias-only.
6. **Proposal 2** (`srv internal` rename) — alias-only.
7. **Proposal 1b** (deprecate top-level site-scoped namespaces) — breaking,
   defer to a major version.

Steps 1–6 can land independently as small PRs; step 7 needs a deprecation
plan.
