#!/usr/bin/env bash
# Import from a real Sonarr (linuxserver image), seeded through its API with a
# tag, a root folder, a series with an episode file on disk, a custom format,
# a delay profile, an indexer, a download client and a remote path mapping.
. "$(dirname "$0")/lib.sh"
NAME="$PREFIX-sonarr"
IMAGE="${SONARR_IMAGE:-lscr.io/linuxserver/sonarr:latest}"
PORT="$(free_port)"
make_media_dir
trap 'cleanup "$NAME"' EXIT
docker rm -f "$NAME" >/dev/null 2>&1 || true
arr_config "$CONFIG_DIR"
mkdir -p "$MEDIA_DIR/Big Buck Bunny/Season 1"
head -c 2000000 /dev/urandom >"$MEDIA_DIR/Big Buck Bunny/Season 1/Big Buck Bunny - S01E01 - Pilot.mkv"
docker run -d --name "$NAME" --network "$NET" -e PUID="$PUID" -e PGID="$PGID" -e TZ=Etc/UTC -p "127.0.0.1:$PORT:8989" \
    -v "$CONFIG_DIR:/config" -v "$MEDIA_DIR:/tv" "$IMAGE" >/dev/null
B="http://127.0.0.1:$PORT/api/v3"
wait_ok "$B/system/status" 180 "X-Api-Key: $KEY"
api POST "$B/tag" '{"label":"kids"}' >/dev/null
api POST "$B/rootfolder" '{"path":"/tv"}' >/dev/null
# A series from Sonarr's own metadata service (needs internet), its folder already on disk.
lookup="$(api GET "$B/series/lookup?term=tvdb%3A81189")"
series="$(printf '%s' "$lookup" | python3 -c 'import json,sys; s=json.load(sys.stdin)[0]; s.update({"qualityProfileId":1,"languageProfileId":1,"rootFolderPath":"/tv","path":"/tv/Big Buck Bunny","monitored":True,"seasonFolder":True,"tags":[1],"addOptions":{"monitor":"all","searchForMissingEpisodes":False}}); print(json.dumps(s))')"
api POST "$B/series" "$series" >/dev/null
api POST "$B/customformat" '{"name":"x265","includeCustomFormatWhenRenaming":false,"specifications":[{"name":"x265","implementation":"ReleaseTitleSpecification","negate":false,"required":true,"fields":[{"name":"value","value":"[xh][ ._-]?265"}]}]}' >/dev/null
api POST "$B/delayprofile" '{"enableUsenet":true,"enableTorrent":true,"preferredProtocol":"torrent","usenetDelay":0,"torrentDelay":30,"bypassIfHighestQuality":true,"order":1,"tags":[1]}' >/dev/null
api POST "$B/indexer?forceSave=true" '{"name":"Test Torznab","implementation":"Torznab","configContract":"TorznabSettings","protocol":"torrent","enableRss":true,"enableAutomaticSearch":true,"enableInteractiveSearch":true,"priority":20,"tags":[],"fields":[{"name":"baseUrl","value":"http://fake-indexer:9117/torznab"},{"name":"apiPath","value":"/api"},{"name":"apiKey","value":"fakekey"},{"name":"categories","value":[5030,5040]}]}' >/dev/null
api POST "$B/downloadclient?forceSave=true" '{"name":"qBit","implementation":"QBittorrent","configContract":"QBittorrentSettings","protocol":"torrent","enable":true,"priority":1,"tags":[],"fields":[{"name":"host","value":"fake-indexer"},{"name":"port","value":9117},{"name":"urlBase","value":"/arr/qbittorrent"},{"name":"username","value":"admin"},{"name":"password","value":"migratepass"},{"name":"tvCategory","value":"tv-sonarr"}]}' >/dev/null
api POST "$B/remotepathmapping" '{"host":"qbittorrent","remotePath":"/downloads/","localPath":"/tv/"}' >/dev/null
# Let Sonarr scan the folder and register the episode file.
for _ in $(seq 1 90); do
    n="$(api GET "$B/episodefile?seriesId=1" | python3 -c 'import json,sys; print(len(json.load(sys.stdin)))')"
    [ "$n" -gt 0 ] && break
    sleep 2
done
echo "Sonarr $(api GET "$B/system/status" | python3 -c 'import json,sys; print(json.load(sys.stdin)["version"])'), episode files: ${n:-0}"
export LOCARR_TEST_SOURCE_KIND=sonarr LOCARR_TEST_SOURCE_URL="http://127.0.0.1:$PORT" LOCARR_TEST_SOURCE_KEY="$KEY" \
    LOCARR_TEST_SOURCE_ROOT=/tv LOCARR_TEST_SOURCE_LOCAL="$MEDIA_DIR" LOCARR_TEST_SOURCE_FILES=1 \
    LOCARR_TEST_SOURCE_EXPECT="series=1,rootfolder=1,indexer=1,downloadclient=1,pathmapping=0,customformat=1,delayprofile=1,qualityprofile=1,naming=1"
run_import
