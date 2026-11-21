#!/usr/bin/env bash
# rTorrent (crazymax/rtorrent-rutorrent: rTorrent with XML-RPC over HTTP at :8000/RPC2).
source "$(dirname "$0")/lib.sh"
NAME="$PREFIX-rtorrent"
IMAGE="${RTORRENT_IMAGE:-crazymax/rtorrent-rutorrent:latest}"
make_seed_dir
trap 'cleanup_container "$NAME"' EXIT
PORT="$(free_port)"
docker run -d --name "$NAME" -e PUID="$PUID" -e PGID="$PGID" -p "127.0.0.1:$PORT:8000" \
    -v "$SEED_DIR:/downloads/seed" "$IMAGE" >/dev/null
wait_for "http://127.0.0.1:$PORT/RPC2"
# rTorrent itself starts a moment after the web server.
for _ in $(seq 1 60); do
    if curl -s --max-time 2 -H 'Content-Type: text/xml' \
        -d '<?xml version="1.0"?><methodCall><methodName>system.client_version</methodName><params/></methodCall>' \
        "http://127.0.0.1:$PORT/RPC2" | grep -q '<string>'; then break; fi
    sleep 1
done
export LOCARR_TEST_RTORRENT_HOST=127.0.0.1 LOCARR_TEST_RTORRENT_PORT="$PORT" LOCARR_TEST_RTORRENT_URLBASE=/RPC2 \
    LOCARR_TEST_RTORRENT_SEED_CLIENT=/downloads/seed LOCARR_TEST_RTORRENT_SEED_LOCAL="$SEED_DIR"
run_contract rtorrent
