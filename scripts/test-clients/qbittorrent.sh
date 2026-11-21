#!/usr/bin/env bash
# Contract suite against a real qBittorrent (linuxserver image).
set -euo pipefail
. "$(dirname "$0")/lib.sh"
NAME="$PREFIX-qbittorrent"
IMAGE="${QBITTORRENT_IMAGE:-lscr.io/linuxserver/qbittorrent:latest}"
PORT="$(free_port)"
make_seed_dir
trap 'cleanup_container "$NAME"' EXIT
docker rm -f "$NAME" >/dev/null 2>&1 || true
docker run -d --name "$NAME" -e PUID="$PUID" -e PGID="$PGID" -e TZ=Etc/UTC -e WEBUI_PORT="$PORT" \
    -p "127.0.0.1:$PORT:$PORT" -v "$SEED_DIR:/downloads/seed" "$IMAGE" >/dev/null
# The WebUI checks the Host header port, so it listens on the published port.
wait_for "http://127.0.0.1:$PORT/"
# qBittorrent 4.6.1+ prints a temporary password for "admin" on first start.
PASS=""
for _ in $(seq 1 60); do
    PASS="$(docker logs "$NAME" 2>&1 | sed -n 's/.*temporary password is provided for this session: *\([^ ]*\).*/\1/p' | tail -1)"
    [ -n "$PASS" ] && break
    sleep 1
done
[ -n "$PASS" ] || { echo "no temporary password in the qBittorrent log" >&2; docker logs "$NAME" | tail -20; exit 1; }
export LOCARR_TEST_QBITTORRENT_HOST=127.0.0.1 LOCARR_TEST_QBITTORRENT_PORT="$PORT" \
    LOCARR_TEST_QBITTORRENT_USER=admin LOCARR_TEST_QBITTORRENT_PASS="$PASS" \
    LOCARR_TEST_QBITTORRENT_SEED_CLIENT=/downloads/seed LOCARR_TEST_QBITTORRENT_SEED_LOCAL="$SEED_DIR"
run_contract qbittorrent
