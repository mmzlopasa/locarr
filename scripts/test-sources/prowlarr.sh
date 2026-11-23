#!/usr/bin/env bash
# Import from a real Prowlarr (linuxserver image), seeded through its API with
# a tag and a generic Torznab indexer (the fake indexer on the test network).
. "$(dirname "$0")/lib.sh"
NAME="$PREFIX-prowlarr"
IMAGE="${PROWLARR_IMAGE:-lscr.io/linuxserver/prowlarr:latest}"
PORT="$(free_port)"
make_media_dir
trap 'cleanup "$NAME"' EXIT
docker rm -f "$NAME" >/dev/null 2>&1 || true
arr_config "$CONFIG_DIR"
docker run -d --name "$NAME" --network "$NET" -e PUID="$PUID" -e PGID="$PGID" -e TZ=Etc/UTC -p "127.0.0.1:$PORT:9696" \
    -v "$CONFIG_DIR:/config" "$IMAGE" >/dev/null
B="http://127.0.0.1:$PORT/api/v1"
wait_ok "$B/system/status" 180 "X-Api-Key: $KEY"
api POST "$B/tag" '{"label":"public"}' >/dev/null
api POST "$B/indexer?forceSave=true" '{"name":"Fake Torznab","implementation":"Torznab","configContract":"TorznabSettings","protocol":"torrent","enable":true,"priority":20,"appProfileId":1,"tags":[1],"fields":[{"name":"baseUrl","value":"http://fake-indexer:9117/torznab"},{"name":"apiPath","value":"/api"},{"name":"apiKey","value":"fakekey"}]}' >/dev/null
echo "Prowlarr $(api GET "$B/system/status" | python3 -c 'import json,sys; print(json.load(sys.stdin)["version"])')"
export LOCARR_TEST_SOURCE_KIND=prowlarr LOCARR_TEST_SOURCE_URL="http://127.0.0.1:$PORT" LOCARR_TEST_SOURCE_KEY="$KEY" \
    LOCARR_TEST_SOURCE_EXPECT="indexer=1"
run_import
