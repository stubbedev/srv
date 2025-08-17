#!/usr/bin/env bash

function init() {
  # Ensure $PWD is the root of the current git repo
  local git_root
  git_root="$(git rev-parse --show-toplevel 2>/dev/null)"
  if [[ -z "$git_root" || "$PWD" != "$git_root" ]]; then
    echo "Error: Script must be run from the root of this git repository."
    return 1
  fi

  local current_dir="$PWD"
  cd "$PWD/traefik" || exit
  docker compose up -d
  cd "$current_dir" || exit
  docker network inspect web >/dev/null 2>&1 || docker network create web
  for d in sites/*/; do
    [ -d "$d" ] && cd "$current_dir/$d" && docker compose up -d && cd "$current_dir" || exit
  done
}

init
