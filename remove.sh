#!/usr/bin/env bash

function remove_site() {
  # Ensure $PWD is the root of the current git repo
  local git_root
  git_root="$(git rev-parse --show-toplevel 2>/dev/null)"
  if [[ -z "$git_root" || "$PWD" != "$git_root" ]]; then
    echo "Error: Script must be run from the root of this git repository."
    return 1
  fi

  local input_path="$1"

  if [[ -z "$input_path" ]]; then
    echo "Error: No site name provided."
    return 1
  fi

  local orig_cwd="$PWD"
  local sites_dir="$PWD/sites"
  local target_dir="$sites_dir/$input_path"

  # Check if input matches a directory or symlink in $PWD/sites
  if [[ ! -d "$target_dir" ]]; then
    echo "Error: '$input_path' is not a directory in $sites_dir."
    exit 1
  fi

  cd "$target_dir" || exit 1
  docker compose down

  cd "$orig_cwd" || exit 1
  rm -rf "$target_dir"
}

remove_site "$@"
