#!/usr/bin/env bash
# Runs the e2e suite inside the Dockerfile.dev image, never on the host.
#
#   e2e/run.sh [docker|podman] [go test args...]
#
# The test process drives the host's engine through its mounted socket, so
# the containers it starts (Traefik, the systemd host of the daemon suite)
# are siblings on the host. Two things make that work:
#
# - Host networking: Traefik binds the host's 80/443 and dials upstreams the
#   tests serve on the host loopback, so the test container shares it.
# - Path parity: srv hands SRV_ROOT and project paths to the engine as bind
#   mounts, which the engine resolves on the host. The repository is mounted
#   at its own path and TMPDIR points inside it, so every t.TempDir() exists
#   at the same absolute path in the container and on the host.
set -euo pipefail

engine=${1:-docker}
if [ "$#" -gt 0 ]; then
	shift
fi

if [ "$(uname -s)" != "Linux" ]; then
	echo "e2e/run.sh: the e2e suite needs a Linux host (Traefik uses host networking)" >&2
	exit 1
fi

repo=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)
state="$repo/.e2e"
image=srv-dev

case "$engine" in
docker)
	host=${DOCKER_HOST:-unix:///var/run/docker.sock}
	socket=${host#unix://}
	engine_env=(-e "DOCKER_HOST=unix://$socket")
	;;
podman)
	# The rootful socket is the supported Podman target. CONTAINER_HOST makes
	# the podman CLI a remote client of it; DOCKER_HOST points Compose and the
	# Docker SDK at the same socket.
	socket=/run/podman/podman.sock
	engine_env=(-e "CONTAINER_HOST=unix://$socket" -e "DOCKER_HOST=unix://$socket")
	;;
*)
	echo "e2e/run.sh: unknown engine \"$engine\" (want docker or podman)" >&2
	exit 1
	;;
esac
if [ ! -S "$socket" ]; then
	echo "e2e/run.sh: $engine socket $socket not found" >&2
	exit 1
fi

mkdir -p "$state/tmp" "$state/home" "$state/gomod" "$state/gocache"

docker build --quiet --tag "$image" --file "$repo/Dockerfile.dev" "$repo" >/dev/null

exec docker run --rm \
	--network host \
	--user "$(id -u):$(id -g)" \
	--group-add "$(stat -c %g "$socket")" \
	--volume "$repo:$repo" \
	--volume "$socket:$socket" \
	--workdir "$repo" \
	--env "HOME=$state/home" \
	--env "TMPDIR=$state/tmp" \
	--env "GOMODCACHE=$state/gomod" \
	--env "GOCACHE=$state/gocache" \
	--env "GOFLAGS=-modcacherw" \
	--env "SRV_CONTAINER_ENGINE=$engine" \
	"${engine_env[@]}" \
	"$image" \
	go test -tags=e2e -v -timeout 30m ./e2e/... "$@"
