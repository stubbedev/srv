#!/usr/bin/env bash

function init() {
	local current_dir=$PWD
	cd $PWD/traefik
	docker compose up -d
	cd $current_dir
	docker network inspect web >/dev/null 2>&1 || docker network create web
	for d in sites/*/; do
  		[ -d "$d" ] && cd "$current_dir/$d" && docker compose up -d && cd $current_dir
	done
}

init
