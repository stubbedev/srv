#!/usr/bin/env bash

function add_site() {
  # Ensure $PWD is the root of the current git repo
  local git_root
  git_root="$(git rev-parse --show-toplevel 2>/dev/null)"
  if [[ -z "$git_root" || "$PWD" != "$git_root" ]]; then
    echo "Error: Script must be run from the root of this git repository."
    return 1
  fi

  local input_path="$1"

  if [[ -z "$input_path" ]]; then
    echo "Error: No site path provided."
    return 1
  fi

  # Check if path is absolute
  if [[ ! "$input_path" = /* ]]; then
    echo "Error: Path must be absolute."
    return 1
  fi

  # Check if path is a directory
  if [[ -d "$input_path" ]]; then
    local site_path="$input_path"
  elif [[ -f "$input_path" ]]; then
    local filename
    filename="$(basename "$input_path")"
    if [[ "$filename" == "docker-compose.yml" || "$filename" == "docker-compose.yaml" ]]; then
      site_path="$(dirname "$input_path")"
    else
      echo "Error: File must be docker-compose.yml or docker-compose.yaml."
      return 1
    fi
  else
    echo "Error: Path does not exist or is not valid."
    return 1
  fi


  # Ensure $PWD/sites exists
  local sites_dir="$PWD/sites"
  mkdir -p "$sites_dir"

  # Get the name of the source directory
  local link_name
  link_name="$(basename "$site_path")"

  # Create symlink
  local link_path="$sites_dir/$link_name"
  if [[ -e "$link_path" ]]; then
    echo "Error: Symlink or directory '$link_path' already exists."
    return 1
  fi

  ln -s "$site_path" "$link_path"
  echo "Symlink created: $link_path -> $site_path"
}

add_site "$@"
