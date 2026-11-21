#!/usr/bin/env bash
# Runs the fake services used for manual and end-to-end testing on the Locarr
# Docker network, next to a running stack.
#
#   ./scripts/dev-fakes.sh up      build and start (or restart) the fakes
#   ./scripts/dev-fakes.sh down    stop and remove them
#   ./scripts/dev-fakes.sh status
#
# Fake indexers (container "fake-indexer" on the network, port 9117):
#   http://fake-indexer:9117/torznab   Torznab, API key "fakekey"
#   http://fake-indexer:9117/quirky    Torznab with real-world quirks
#   http://fake-indexer:9117/newznab   Newznab (usenet)
#   http://fake-indexer:9117/broken    always fails      /slow  answers after 3 s
#   http://fake-indexer:9117/prowlarr  Prowlarr-style source, API key "sourcekey"
#   http://fake-indexer:9117/jackett   Jackett-style source, API key "sourcekey"
#
#   LOCARR_FAKES_NETWORK   Docker network (default locarr-net)
#   LOCARR_FAKES_NAME      container name (default locarr-fakes)
#   LOCARR_FAKES_PORT      also publish on this host address, e.g. 127.0.0.1:9117 (default: not published)
#   LOCARR_FAKES_SEED_DIR  host folder (inside the Locarr data folder, e.g. <data>/downloads/fake-seed):
#                          the fake indexers then serve real tiny torrents whose data they write there,
#                          so a download client that saves into the same folder finishes them offline
#
# Fake TMDb: http://fake-indexer:9117/tmdb (start Locarr with LOCARR_TMDB_URL=http://fake-indexer:9117/tmdb,
# API key fake0000000000000000000000tmdb01).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
NETWORK="${LOCARR_FAKES_NETWORK:-locarr-net}"
NAME="${LOCARR_FAKES_NAME:-locarr-fakes}"
IMAGE="locarr-fakes:dev"

case "${1:-up}" in
up)
    docker network inspect "$NETWORK" >/dev/null 2>&1 || docker network create "$NETWORK" >/dev/null
    docker build -q -t "$IMAGE" -f "$ROOT/deploy/fakes.Dockerfile" "$ROOT" >/dev/null
    docker rm -f "$NAME" >/dev/null 2>&1 || true
    publish=()
    if [ -n "${LOCARR_FAKES_PORT:-}" ]; then publish=(-p "${LOCARR_FAKES_PORT}:9117"); fi
    seed=()
    if [ -n "${LOCARR_FAKES_SEED_DIR:-}" ]; then
        mkdir -p "$LOCARR_FAKES_SEED_DIR"
        seed=(-v "$LOCARR_FAKES_SEED_DIR:/seed" -e LOCARR_FAKES_SEED_DIR=/seed --user "$(id -u):$(id -g)")
    fi
    docker run -d --name "$NAME" --network "$NETWORK" --network-alias fake-indexer --restart unless-stopped \
        --read-only --cap-drop ALL --security-opt no-new-privileges:true "${publish[@]}" "${seed[@]}" "$IMAGE" >/dev/null
    echo "fake indexers running as $NAME on $NETWORK: http://fake-indexer:9117/torznab (key fakekey)"
    ;;
down)
    docker rm -f "$NAME" >/dev/null 2>&1 || true
    echo "removed $NAME"
    ;;
status)
    docker ps --filter "name=^${NAME}$" --format '{{.Names}}\t{{.Status}}'
    ;;
*)
    echo "usage: $0 up|down|status" >&2
    exit 2
    ;;
esac
