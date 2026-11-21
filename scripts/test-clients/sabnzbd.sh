#!/usr/bin/env bash
# Contract suite against a real SABnzbd (linuxserver image). No news server is
# configured, so jobs never download; the suite checks the API behaviour only.
. "$(dirname "$0")/lib.sh"
NAME="$PREFIX-sabnzbd"
IMAGE="${SABNZBD_IMAGE:-lscr.io/linuxserver/sabnzbd:latest}"
PORT="$(free_port)"
make_seed_dir
trap 'cleanup_container "$NAME"' EXIT
cleanup_container "$NAME"
docker run -d --name "$NAME" -e PUID="$PUID" -e PGID="$PGID" -e TZ=Etc/UTC \
    -p "127.0.0.1:$PORT:8080" "$IMAGE" >/dev/null
wait_for "http://127.0.0.1:$PORT/api?mode=version&output=json" 120
# The API key is generated on first start.
KEY=""
for _ in $(seq 1 60); do
    KEY="$(docker exec "$NAME" sh -c "grep -E '^api_key *=' /config/sabnzbd.ini 2>/dev/null | head -1 | sed 's/^api_key *= *//'" || true)"
    [ -n "$KEY" ] && break
    sleep 1
done
[ -n "$KEY" ] || { echo "no SABnzbd API key found" >&2; exit 1; }
echo "SABnzbd $(curl -s "http://127.0.0.1:$PORT/api?mode=version&output=json")"
export LOCARR_TEST_SABNZBD_HOST=127.0.0.1 LOCARR_TEST_SABNZBD_PORT="$PORT" LOCARR_TEST_SABNZBD_APIKEY="$KEY"
run_contract sabnzbd
