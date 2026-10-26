# Importing from Laravel Valet

```sh
srv import valet             # print the equivalent srv commands (dry run)
srv import valet --apply     # run them
srv import valet --list-sites
```

srv reads every Valet nginx config (`~/.valet` or `~/.config/valet`,
whichever has content), resolves each host to its project through parked
paths and `Sites/` symlinks, folds hosts that share a root into one
`srv add --alias` call, and maps proxy passes, `:88` listeners, path
splits, regex rewrites and fallback blocks onto the matching srv
commands. `--skip` records decisions in
`~/.config/srv/import-decisions.yml`.

**PHP sites** come out as commented-out `srv add` lines: srv does not
manage runtimes, so each project needs a `Dockerfile` or
`docker-compose.yml` before the line can run. See
[Sites](sites.md#example-laravel-with-local-https) for a compose file
that serves Laravel.
