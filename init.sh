#!/usr/bin/env bash

function init() {
	local current_dir=$PWD
	cd $PWD/traefik
	docker compose up -d
	cd $current_dir
	for d in sites/*/; do
  		[ -d "$d" ] && cd "$current_dir/$d" && docker compose up -d && cd $current_dir
	done
}

init
