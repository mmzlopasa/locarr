#!/usr/bin/env bash
# Import from a real Bazarr (linuxserver image): language profiles (seeded
# through its settings API) and providers (from its configuration).
. "$(dirname "$0")/lib.sh"
NAME="$PREFIX-bazarr"
IMAGE="${BAZARR_IMAGE:-lscr.io/linuxserver/bazarr:latest}"
PORT="$(free_port)"
make_media_dir
trap 'cleanup "$NAME"' EXIT
docker rm -f "$NAME" >/dev/null 2>&1 || true
mkdir -p "$CONFIG_DIR/config"
cat >"$CONFIG_DIR/config/config.yaml" <<YAML
auth:
  apikey: $KEY
  type: null
general:
  enabled_providers:
  - opensubtitlescom
  - subdl
  - podnapisi
opensubtitlescom:
  username: subsuser
  password: subspass
subdl:
  api_key: subdlkey123
YAML
chmod -R 0777 "$CONFIG_DIR/config"
docker run -d --name "$NAME" -e PUID="$PUID" -e PGID="$PGID" -e TZ=Etc/UTC -p "127.0.0.1:$PORT:6767" -v "$CONFIG_DIR:/config" "$IMAGE" >/dev/null
U="http://127.0.0.1:$PORT"
wait_ok "$U/api/system/status" 180 "X-API-KEY: $KEY"
curl -sS -f -X POST -H "X-API-KEY: $KEY" -F 'languages-enabled=en' -F 'languages-enabled=hr' \
    -F 'languages-profiles=[{"profileId":1,"name":"Croatian + English","cutoff":null,"items":[{"id":1,"language":"hr","forced":"False","hi":"False","audio_exclude":"False"},{"id":2,"language":"en","forced":"False","hi":"True","audio_exclude":"False"}],"mustContain":[],"mustNotContain":[],"originalFormat":false}]' \
    "$U/api/system/settings" >/dev/null
echo "Bazarr $(curl -s -H "X-API-KEY: $KEY" "$U/api/system/status" | python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["bazarr_version"])'), profiles: $(curl -s -H "X-API-KEY: $KEY" "$U/api/system/languages/profiles" | python3 -c 'import json,sys; print(len(json.load(sys.stdin)))')"
export LOCARR_TEST_SOURCE_KIND=bazarr LOCARR_TEST_SOURCE_URL="$U" LOCARR_TEST_SOURCE_KEY="$KEY" \
    LOCARR_TEST_SOURCE_EXPECT="subtitleprofile=1"
run_import
