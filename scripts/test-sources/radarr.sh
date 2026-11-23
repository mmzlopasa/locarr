#!/usr/bin/env bash
# Import from a real Radarr (linuxserver image), seeded through its API with a
# tag, a root folder, a movie whose file is already on disk, a custom format,
# an indexer, a download client and an exclusion.
. "$(dirname "$0")/lib.sh"
NAME="$PREFIX-radarr"
IMAGE="${RADARR_IMAGE:-lscr.io/linuxserver/radarr:latest}"
PORT="$(free_port)"
make_media_dir
trap 'cleanup "$NAME"' EXIT
docker rm -f "$NAME" >/dev/null 2>&1 || true
arr_config "$CONFIG_DIR"
mkdir -p "$MEDIA_DIR/Big Buck Bunny (2008)"
head -c 2000000 /dev/urandom >"$MEDIA_DIR/Big Buck Bunny (2008)/Big.Buck.Bunny.2008.1080p.BluRay.x264-OPEN.mkv"
docker run -d --name "$NAME" --network "$NET" -e PUID="$PUID" -e PGID="$PGID" -e TZ=Etc/UTC -p "127.0.0.1:$PORT:7878" \
    -v "$CONFIG_DIR:/config" -v "$MEDIA_DIR:/movies" "$IMAGE" >/dev/null
B="http://127.0.0.1:$PORT/api/v3"
wait_ok "$B/system/status" 180 "X-Api-Key: $KEY"
api POST "$B/tag" '{"label":"kids"}' >/dev/null
api POST "$B/rootfolder" '{"path":"/movies"}' >/dev/null
lookup="$(api GET "$B/movie/lookup/tmdb?tmdbId=10378")"
movie="$(printf '%s' "$lookup" | python3 -c 'import json,sys; m=json.load(sys.stdin); m=m[0] if isinstance(m,list) else m; m.update({"qualityProfileId":1,"rootFolderPath":"/movies","path":"/movies/Big Buck Bunny (2008)","monitored":True,"minimumAvailability":"released","tags":[1],"addOptions":{"searchForMovie":False}}); print(json.dumps(m))')"
api POST "$B/movie" "$movie" >/dev/null
api POST "$B/customformat" '{"name":"x265","includeCustomFormatWhenRenaming":false,"specifications":[{"name":"x265","implementation":"ReleaseTitleSpecification","negate":false,"required":true,"fields":[{"name":"value","value":"[xh][ ._-]?265"}]}]}' >/dev/null
api POST "$B/indexer?forceSave=true" '{"name":"Test Torznab","implementation":"Torznab","configContract":"TorznabSettings","protocol":"torrent","enableRss":true,"enableAutomaticSearch":true,"enableInteractiveSearch":true,"priority":20,"tags":[],"fields":[{"name":"baseUrl","value":"http://fake-indexer:9117/torznab"},{"name":"apiPath","value":"/api"},{"name":"apiKey","value":"fakekey"},{"name":"categories","value":[2000,2040]}]}' >/dev/null
api POST "$B/downloadclient?forceSave=true" '{"name":"qBit","implementation":"QBittorrent","configContract":"QBittorrentSettings","protocol":"torrent","enable":true,"priority":1,"tags":[],"fields":[{"name":"host","value":"fake-indexer"},{"name":"port","value":9117},{"name":"urlBase","value":"/arr/qbittorrent"},{"name":"username","value":"admin"},{"name":"password","value":"migratepass"},{"name":"movieCategory","value":"radarr"}]}' >/dev/null
api POST "$B/exclusions" '{"tmdbId":45745,"movieTitle":"Sintel","movieYear":2010}' >/dev/null || true
for _ in $(seq 1 90); do
    has="$(api GET "$B/movie" | python3 -c 'import json,sys; print(sum(1 for m in json.load(sys.stdin) if m.get("hasFile")))')"
    [ "$has" -gt 0 ] && break
    sleep 2
done
echo "Radarr $(api GET "$B/system/status" | python3 -c 'import json,sys; print(json.load(sys.stdin)["version"])'), movies with files: ${has:-0}"
export LOCARR_TEST_SOURCE_KIND=radarr LOCARR_TEST_SOURCE_URL="http://127.0.0.1:$PORT" LOCARR_TEST_SOURCE_KEY="$KEY" \
    LOCARR_TEST_SOURCE_ROOT=/movies LOCARR_TEST_SOURCE_LOCAL="$MEDIA_DIR" LOCARR_TEST_SOURCE_FILES=1 \
    LOCARR_TEST_SOURCE_EXPECT="movie=1,rootfolder=1,indexer=1,downloadclient=1,customformat=1,qualityprofile=1,naming=1"
run_import
