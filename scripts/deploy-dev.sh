#!/usr/bin/env bash
# Rebuilds the image from this checkout, (re)starts the dev stack and checks
# that /api/v1/system/info reports the current commit.
#
#   LOCARR_DEV_DATA   host folder for config and data (default ~/locarr-data)
#   LOCARR_DEV_PORT   host address to publish on (default 127.0.0.1:8484)
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DATA="${LOCARR_DEV_DATA:-$HOME/locarr-data}"
PORT="${LOCARR_DEV_PORT:-127.0.0.1:8484}"
COMMIT="$(git -C "$ROOT" rev-parse HEAD)"
if [ -n "$(git -C "$ROOT" status --porcelain)" ]; then
    COMMIT="$COMMIT-dirty"
fi

mkdir -p "$DATA/config" "$DATA/data/media/movies" "$DATA/data/media/tv" "$DATA/data/media/music" \
    "$DATA/data/downloads/torrents" "$DATA/data/downloads/usenet"

export CONFIG_DIR="$DATA/config" DATA_DIR="$DATA/data" HTTP_PORT="$PORT" \
    PUID="$(id -u)" PGID="$(id -g)" LOCARR_BUILD_COMMIT="$COMMIT" LOCARR_BUILD_VERSION="dev"
compose=(docker compose -p locarr -f "$ROOT/deploy/docker-compose.yml" -f "$ROOT/deploy/compose.dev.yml")
"${compose[@]}" up -d --build --wait

url="http://${PORT}/api/v1/system/info"
case "$PORT" in *:*) ;; *) url="http://127.0.0.1:${PORT}/api/v1/system/info" ;; esac
for _ in $(seq 1 30); do
    if info="$(curl -fsS "$url" 2>/dev/null)"; then
        if printf '%s' "$info" | grep -q "\"commit\":\"$COMMIT\""; then
            echo "dev stack is running $COMMIT at ${url%/api/v1/system/info}"
            if printf '%s' "$info" | grep -q '"setupRequired":true'; then
                "${compose[@]}" logs locarr 2>/dev/null | grep -o 'setup code [A-Z0-9-]*' | tail -1 || true
            fi
            exit 0
        fi
    fi
    sleep 1
done
echo "the server did not report commit $COMMIT; last answer: ${info:-none}" >&2
exit 1
