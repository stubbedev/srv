# Deploy Utility Scripts

This repository provides a lightweight pattern for managing multiple containerized “sites” (each site = a directory that contains a `docker-compose.yml` / `docker-compose.yaml`) from a single controlling Git repository using three utility scripts:

- `init.sh` (initialize repo state – and initialize all linked sites)
- `add.sh` (register a site by creating a symlink under `./sites`)
- `remove.sh` (stop and unregister a site)

> NOTE: The code excerpts available so far include `add.sh` and `remove.sh`. The description of `init.sh` is based on conventional expectations; update this README once the actual script exists or if it differs.

---

## Repository Layout (after using the scripts)

```
repo-root/
  add.sh
  remove.sh
  init.sh            (expected / planned)
  sites/             (auto-created; holds symlinks to real site directories)
    my-site -> /absolute/path/to/my-site
```

Each entry inside `sites/` is a **symlink** pointing to a real directory containing a valid Docker Compose project.

---

## 1. Initialization (`init.sh`)

(Planned / Expected Behavior)

Typical responsibilities you may want `init.sh` to perform:

1. Verify you are at the repository root.
2. Create the `sites/` directory if it does not exist.
3. Optionally create a `.env` template or any shared network / volume scaffolding.
4. Optionally perform a health check on existing symlinks (remove broken ones, etc.).

### Hypothetical Usage

```bash
./init.sh
```

### Suggested Features (if not yet implemented)

- Idempotent creation of `sites/`
- Validation that `docker` and `docker compose` are available
- Scan existing symlinks: warn about targets that no longer exist
- Optionally prompt to auto-repair or prune broken links

---

## 2. Adding a Site (`add.sh`)

`add.sh` registers an existing site directory (or one of its compose files) by creating a symlink inside `./sites`.

### Key Behaviors (from script)

- Must be run from the **root of this git repository**. (It compares `$PWD` with `git rev-parse --show-toplevel`.)
- Requires an **absolute path** argument.
- Argument can be:
  - A directory containing a compose file, OR
  - A direct path to `docker-compose.yml` / `docker-compose.yaml`
- Ensures `./sites` exists (creates it if missing).
- Uses the basename of the target directory as the symlink name.
- Fails if a file/symlink/directory with that name already exists under `./sites`.

### Usage

```bash
# Add by directory
./add.sh /absolute/path/to/my-site

# Add by compose file
./add.sh /absolute/path/to/another-site/docker-compose.yml
```

### Result

```
sites/
  my-site -> /absolute/path/to/my-site
  another-site -> /absolute/path/to/another-site
```

### Typical Next Step

After adding, you can start containers by changing into the symlink or directly running (depending on workflow):

```bash
./init.sh
```


---

## 3. Removing a Site (`remove.sh`)

`remove.sh` stops the running containers for a registered site and removes its entry under `./sites`.

### Key Behaviors (from script)

- Must be run from repo root (same git root check pattern).
- Takes a single argument: the **site name** (i.e., the symlink name inside `./sites`).
- Verifies `./sites/<name>` is a directory (the `-d` test will succeed for a symlink pointing to an existing directory).
- Changes into that directory and executes:
  ```bash
  docker compose down
  ```
- Returns to repo root and removes the (symlink) directory entry with `rm -rf`.

### Usage

```bash
./remove.sh my-site
```


---

## Common Workflow Example

```bash

# 1. Add a new site (absolute path!)
./add.sh /srv/sites/blog

# 2. Initialize repository and sites
./init.sh

# 3. Later, stop and remove it
./remove.sh blog
```

---

## Error Handling Summary

| Script    | Error Condition                                   | Behavior / Message |
|-----------|----------------------------------------------------|--------------------|
| add.sh    | Not run from repo root                             | "Error: Script must be run from the root..." |
| add.sh    | Missing argument                                   | "Error: No site path provided." |
| add.sh    | Non-absolute path                                  | "Error: Path must be absolute." |
| add.sh    | Path invalid / not directory / wrong file          | "Error: Path does not exist or is not valid." / file-type errors |
| add.sh    | Symlink already exists                             | "Error: Symlink or directory '...' already exists." |
| remove.sh | Not run from repo root                             | Same style root error |
| remove.sh | Missing argument                                   | "Error: No site name provided." |
| remove.sh | Target not a directory under sites/                | "Error: 'name' is not a directory in sites/." |
| remove.sh | (Current bug) Misspelled function invocation       | Prevents execution until fixed |


---

## Troubleshooting

- Q: I ran `./add.sh relative/path` and it failed.  
  A: Paths must be absolute. Use `$(pwd)/relative/path` or convert with `realpath`.

- Q: `./remove.sh name` does nothing.  
  A: Check (and fix) the typo in the script (`remoive_site` -> `remove_site`).

- Q: `docker compose down` fails inside `remove.sh`.  
  A: Ensure Docker Engine and the Docker Compose plugin are installed and the project is valid.

---

## Conventions

- All scripts are intended to be executed from the repo root.
- Symlinks keep the actual site code bases elsewhere (e.g., under `/srv/sites`), letting this repo serve as a control panel.
- Absolute paths prevent ambiguity if this repo is cloned in different locations.

---

