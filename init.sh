#!/usr/bin/env bash

function ensure_traefik_env_email() {
  local env_file="traefik/.env"
  local email_regex="^[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}$"
  local email

  if [[ ! -f "$env_file" ]]; then
    echo "traefik/.env not found. Please enter a valid email for TRAEFIK_CERTBOT_EMAIL:"
    read -r email
    while [[ ! "$email" =~ $email_regex ]]; do
      echo "Invalid email. Please enter a valid email:"
      read -r email
    done
    echo "TRAEFIK_CERTBOT_EMAIL=$email" > "$env_file"
  else
    email=$(grep -E '^TRAEFIK_CERTBOT_EMAIL=' "$env_file" | cut -d= -f2)
    if [[ ! "$email" =~ $email_regex ]]; then
      echo "TRAEFIK_CERTBOT_EMAIL is missing or invalid in traefik/.env. Please enter a valid email:"
      read -r email
      while [[ ! "$email" =~ $email_regex ]]; do
        echo "Invalid email. Please enter a valid email:"
        read -r email
      done
      # Replace or add the line
      if grep -q '^TRAEFIK_CERTBOT_EMAIL=' "$env_file"; then
        sed -i "s/^TRAEFIK_CERTBOT_EMAIL=.*/TRAEFIK_CERTBOT_EMAIL=$email/" "$env_file"
      else
        echo "TRAEFIK_CERTBOT_EMAIL=$email" >> "$env_file"
      fi
    fi
  fi
}

function init() {
  # Ensure $PWD is the root of the current git repo
  local git_root
  git_root="$(git rev-parse --show-toplevel 2>/dev/null)"
  if [[ -z "$git_root" || "$PWD" != "$git_root" ]]; then
    echo "Error: Script must be run from the root of this git repository."
    return 1
  fi

  local current_dir="$PWD"
  ensure_traefik_env_email
  cd "$PWD/traefik" || exit
  docker compose up -d
  cd "$current_dir" || exit
  docker network inspect web >/dev/null 2>&1 || docker network create web

  for d in sites/*/; do
    site_name=$(basename "$d")
    if [[ "$site_name" == "site.example" ]]; then
      echo "Skipping test site: $site_name"
      continue
    fi

    if [[ ! -f "$d/docker-compose.yaml" && ! -f "$d/docker-compose.yml" ]]; then
      echo "Skipping $site_name: missing docker-compose.yaml/yml"
      continue
    fi

    if [[ ! -f "$d/.env" ]]; then
      echo "Skipping $site_name: missing .env file"
      continue
    fi


    cd "$current_dir/$d" && docker compose up -d && cd "$current_dir" || exit
  done
}

init
