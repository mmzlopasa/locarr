#!/usr/bin/env bash
# Import from a real Lidarr (linuxserver image), seeded through its API with a
# root folder and an artist (its albums come from Lidarr's metadata service,
# so this needs internet).
. "$(dirname "$0")/lib.sh"
NAME="$PREFIX-lidarr"
IMAGE="${LIDARR_IMAGE:-lscr.io/linuxserver/lidarr:latest}"
PORT="$(free_port)"
make_media_dir
trap 'cleanup "$NAME"' EXIT
docker rm -f "$NAME" >/dev/null 2>&1 || true
arr_config "$CONFIG_DIR"
docker run -d --name "$NAME" --network "$NET" -e PUID="$PUID" -e PGID="$PGID" -e TZ=Etc/UTC -p "127.0.0.1:$PORT:8686" \
    -v "$CONFIG_DIR:/config" -v "$MEDIA_DIR:/music" "$IMAGE" >/dev/null
B="http://127.0.0.1:$PORT/api/v1"
wait_ok "$B/system/status" 180 "X-Api-Key: $KEY"
api POST "$B/tag" '{"label":"loud"}' >/dev/null
api POST "$B/rootfolder" '{"name":"Music","path":"/music","defaultQualityProfileId":1,"defaultMetadataProfileId":1,"defaultMonitorOption":"all","defaultTags":[]}' >/dev/null
# Daft Punk.
lookup="$(api GET "$B/artist/lookup?term=lidarr%3A056e4f3e-d505-4dad-8ec1-d04f521cbb56")"
artist="$(printf '%s' "$lookup" | python3 -c 'import json,sys; a=json.load(sys.stdin)[0]; a.update({"qualityProfileId":1,"metadataProfileId":1,"rootFolderPath":"/music","path":"/music/Daft Punk","monitored":True,"tags":[1],"addOptions":{"monitor":"all","searchForMissingAlbums":False}}); print(json.dumps(a))')"
api POST "$B/artist" "$artist" >/dev/null
for _ in $(seq 1 90); do
    n="$(api GET "$B/album" | python3 -c 'import json,sys; print(len(json.load(sys.stdin)))')"
    [ "$n" -gt 0 ] && break
    sleep 2
done
echo "Lidarr $(api GET "$B/system/status" | python3 -c 'import json,sys; print(json.load(sys.stdin)["version"])'), albums: ${n:-0}"
export LOCARR_TEST_SOURCE_KIND=lidarr LOCARR_TEST_SOURCE_URL="http://127.0.0.1:$PORT" LOCARR_TEST_SOURCE_KEY="$KEY" \
    LOCARR_TEST_SOURCE_ROOT=/music LOCARR_TEST_SOURCE_LOCAL="$MEDIA_DIR" \
    LOCARR_TEST_SOURCE_EXPECT="artist=1,rootfolder=1,metadataprofile=1,qualityprofile=1"
run_import
