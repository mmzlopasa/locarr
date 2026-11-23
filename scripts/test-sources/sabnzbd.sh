#!/usr/bin/env bash
# Import from a real SABnzbd (linuxserver image): the connection and categories.
. "$(dirname "$0")/lib.sh"
NAME="$PREFIX-sabnzbd"
IMAGE="${SABNZBD_IMAGE:-lscr.io/linuxserver/sabnzbd:latest}"
PORT="$(free_port)"
make_media_dir
trap 'cleanup "$NAME"' EXIT
docker rm -f "$NAME" >/dev/null 2>&1 || true
docker run -d --name "$NAME" -e PUID="$PUID" -e PGID="$PGID" -e TZ=Etc/UTC -p "127.0.0.1:$PORT:8080" "$IMAGE" >/dev/null
U="http://127.0.0.1:$PORT"
wait_ok "$U/api?mode=version&output=json" 120
SKEY=""
for _ in $(seq 1 60); do
    SKEY="$(docker exec "$NAME" sh -c "grep -E '^api_key *=' /config/sabnzbd.ini 2>/dev/null | head -1 | sed 's/^api_key *= *//'" || true)"
    [ -n "$SKEY" ] && break
    sleep 1
done
[ -n "$SKEY" ] || { echo "no SABnzbd API key found" >&2; exit 1; }
# The default install has movies, tv, audio and software categories.
echo "SABnzbd $(curl -s "$U/api?mode=version&output=json")"
export LOCARR_TEST_SOURCE_KIND=sabnzbd LOCARR_TEST_SOURCE_URL="$U" LOCARR_TEST_SOURCE_KEY="$SKEY" \
    LOCARR_TEST_SOURCE_EXPECT="downloadclient=1"
run_import
