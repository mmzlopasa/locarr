#!/usr/bin/env bash
# Shared helpers for scripts/test-sources/<app>.sh.
#
#   LOCARR_SOURCES_PREFIX  container name prefix (default locarr-test-src)
#   LOCARR_SOURCES_TMP     parent folder for the media folder (default $TMPDIR or /tmp)
#   LOCARR_GO_DOCKER=1     run go in a golang container instead of the local toolchain
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PREFIX="${LOCARR_SOURCES_PREFIX:-locarr-test-src}"
PUID="$(id -u)"
PGID="$(id -g)"
KEY="locarrtest0123456789abcdef012345"
NET="$PREFIX-net"

# start_fakes: the fake indexers and download clients on the test network, so
# the applications accept the indexers and clients the scripts add (they test
# them before saving). Reachable as http://fake-indexer:9117 on "$NET".
start_fakes() {
    docker network inspect "$NET" >/dev/null 2>&1 || docker network create "$NET" >/dev/null
    if ! docker ps --format '{{.Names}}' | grep -qx "$PREFIX-fakes"; then
        docker build -q -t "$PREFIX-fakes:test" -f "$ROOT/deploy/fakes.Dockerfile" "$ROOT" >/dev/null
        docker rm -f "$PREFIX-fakes" >/dev/null 2>&1 || true
        docker run -d --name "$PREFIX-fakes" --network "$NET" --network-alias fake-indexer --read-only "$PREFIX-fakes:test" >/dev/null
    fi
}

stop_fakes() {
    docker rm -f "$PREFIX-fakes" >/dev/null 2>&1 || true
    docker network rm "$NET" >/dev/null 2>&1 || true
}

# make_media_dir creates the media folder shared by the application and the test.
make_media_dir() {
    MEDIA_DIR="$(mktemp -d "${LOCARR_SOURCES_TMP:-${TMPDIR:-/tmp}}/locarr-src.XXXXXX")"
    chmod 0777 "$MEDIA_DIR"
    CONFIG_DIR="$(mktemp -d "${LOCARR_SOURCES_TMP:-${TMPDIR:-/tmp}}/locarr-src-config.XXXXXX")"
    chmod 0777 "$CONFIG_DIR"
}

# cleanup NAME: removes the container and the folders (files the application
# created may belong to another user, so a throwaway container deletes them).
cleanup() {
    docker rm -f -v "$1" >/dev/null 2>&1 || true
    for d in "${MEDIA_DIR:-}" "${CONFIG_DIR:-}"; do
        if [ -n "$d" ] && [ -d "$d" ]; then
            docker run --rm -v "$d:/s" alpine:3 sh -c 'rm -rf /s/* /s/.[!.]* 2>/dev/null; true' >/dev/null 2>&1 || true
            rmdir "$d" 2>/dev/null || rm -rf "$d" 2>/dev/null || true
        fi
    done
}

free_port() {
    python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()' 2>/dev/null ||
        echo $((20000 + RANDOM % 20000))
}

# wait_ok URL [SECONDS] [HEADER]: waits for a 200 answer.
wait_ok() {
    local url="$1" limit="${2:-120}" hdr="${3:-X-Unused: 1}"
    for _ in $(seq 1 "$limit"); do
        if [ "$(curl -s -o /dev/null -w '%{http_code}' --max-time 3 -H "$hdr" "$url")" = "200" ]; then return 0; fi
        sleep 1
    done
    echo "timed out waiting for $url" >&2
    return 1
}

# arr_config DIR: an *arr config.xml with a known API key.
arr_config() {
    cat >"$1/config.xml" <<XML
<Config>
  <ApiKey>$KEY</ApiKey>
  <AuthenticationMethod>Forms</AuthenticationMethod>
  <AuthenticationRequired>DisabledForLocalAddresses</AuthenticationRequired>
  <AnalyticsEnabled>False</AnalyticsEnabled>
  <UpdateMechanism>Docker</UpdateMechanism>
</Config>
XML
}

# api METHOD URL [JSON]: calls the test instance's API with the key and fails loudly.
api() {
    local method="$1" url="$2" data="${3:-}"
    local out
    if [ -n "$data" ]; then
        out="$(curl -sS -f -X "$method" -H "X-Api-Key: $KEY" -H 'Content-Type: application/json' -d "$data" "$url")" ||
            { echo "seed $method $url failed: $(curl -s -X "$method" -H "X-Api-Key: $KEY" -H 'Content-Type: application/json' -d "$data" "$url" | head -c 600)" >&2; return 1; }
    else
        out="$(curl -sS -f -X "$method" -H "X-Api-Key: $KEY" "$url")" || { echo "seed $method $url failed" >&2; return 1; }
    fi
    printf '%s' "$out"
}

# run_import: runs the import test; the caller exports LOCARR_TEST_SOURCE_* first.
run_import() {
    if [ "${LOCARR_GO_DOCKER:-}" = "1" ]; then
        local envs=()
        while IFS='=' read -r k _; do envs+=(-e "$k"); done < <(env | grep '^LOCARR_TEST_SOURCE_')
        docker run --rm -u "$PUID:$PGID" -e HOME=/tmp -e GOCACHE=/tmp/gocache -e GOFLAGS=-buildvcs=false \
            -v "${LOCARR_GOMOD_VOLUME:-locarr-test-gomod}:/go/pkg/mod" -v "${LOCARR_GOCACHE_VOLUME:-locarr-test-gocache}:/tmp/gocache" \
            -v "$ROOT:/src" -v "$MEDIA_DIR:$MEDIA_DIR" -w /src/server --network host "${envs[@]}" \
            golang:1.26-trixie go test -tags sources -count=1 -v -run 'TestRealSourceImport' ./internal/app/
    else
        (cd "$ROOT/server" && go test -tags sources -count=1 -v -run 'TestRealSourceImport' ./internal/app/)
    fi
}
