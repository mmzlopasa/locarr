#!/usr/bin/env bash
# Import from a real qBittorrent (linuxserver image): the connection and its
# categories (created through its WebUI API on this test instance).
. "$(dirname "$0")/lib.sh"
NAME="$PREFIX-qbittorrent"
IMAGE="${QBITTORRENT_IMAGE:-lscr.io/linuxserver/qbittorrent:latest}"
PORT="$(free_port)"
make_media_dir
trap 'cleanup "$NAME"' EXIT
docker rm -f "$NAME" >/dev/null 2>&1 || true
docker run -d --name "$NAME" -e PUID="$PUID" -e PGID="$PGID" -e TZ=Etc/UTC -e WEBUI_PORT="$PORT" \
    -p "127.0.0.1:$PORT:$PORT" -v "$MEDIA_DIR:/downloads" "$IMAGE" >/dev/null
U="http://127.0.0.1:$PORT"
for _ in $(seq 1 90); do curl -s -o /dev/null "$U/" && break; sleep 1; done
PASS=""
for _ in $(seq 1 60); do
    PASS="$(docker logs "$NAME" 2>&1 | sed -n 's/.*temporary password is provided for this session: *\([^ ]*\).*/\1/p' | tail -1)"
    [ -n "$PASS" ] && break
    sleep 1
done
[ -n "$PASS" ] || { echo "no temporary password in the qBittorrent log" >&2; exit 1; }
JAR="$(mktemp)"
curl -s -c "$JAR" -H "Referer: $U" --data-urlencode username=admin --data-urlencode "password=$PASS" "$U/api/v2/auth/login" >/dev/null
for c in radarr:/downloads/movies tv-sonarr: music:/downloads/music; do
    curl -s -b "$JAR" -H "Referer: $U" --data-urlencode "category=${c%%:*}" --data-urlencode "savePath=${c#*:}" "$U/api/v2/torrents/createCategory" >/dev/null
done
rm -f "$JAR"
echo "qBittorrent $(curl -s "$U/api/v2/app/version" || true)"
export LOCARR_TEST_SOURCE_KIND=qbittorrent LOCARR_TEST_SOURCE_URL="$U" LOCARR_TEST_SOURCE_KEY="$PASS" LOCARR_TEST_SOURCE_USER=admin \
    LOCARR_TEST_SOURCE_EXPECT="downloadclient=1"
run_import
