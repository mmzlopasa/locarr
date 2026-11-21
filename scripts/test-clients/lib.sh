#!/usr/bin/env bash
# Shared helpers for scripts/test-clients/<client>.sh: each script starts one
# real download client in Docker, runs the shared contract suite against it
# (go test -tags clients) and removes the container again.
#
#   LOCARR_CLIENTS_PREFIX  container name prefix (default locarr-test)
#   LOCARR_CLIENTS_TMP     parent folder for the shared seed folder (default $TMPDIR or /tmp)
#   LOCARR_GO_DOCKER=1     run go in a golang container instead of the local toolchain
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PREFIX="${LOCARR_CLIENTS_PREFIX:-locarr-test}"
PUID="$(id -u)"
PGID="$(id -g)"

# make_seed_dir creates a world-writable folder shared by the test and the client.
make_seed_dir() {
    SEED_DIR="$(mktemp -d "${LOCARR_CLIENTS_TMP:-${TMPDIR:-/tmp}}/locarr-seed.XXXXXX")"
    chmod 0777 "$SEED_DIR"
}

# cleanup_container NAME: removes the container and the seed folder (files the
# client created may belong to another user, so a throwaway container deletes them).
cleanup_container() {
    docker rm -f -v "$1" >/dev/null 2>&1 || true
    if [ -n "${SEED_DIR:-}" ] && [ -d "$SEED_DIR" ]; then
        docker run --rm -v "$SEED_DIR:/s" alpine:3 sh -c 'rm -rf /s/* /s/.[!.]* 2>/dev/null; true' >/dev/null 2>&1 || true
        rmdir "$SEED_DIR" 2>/dev/null || rm -rf "$SEED_DIR" 2>/dev/null || true
    fi
}

# free_port prints a free local TCP port.
free_port() {
    python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()' 2>/dev/null ||
        echo $((20000 + RANDOM % 20000))
}

# wait_for URL [SECONDS]: waits until the URL answers at all (any HTTP status).
wait_for() {
    local url="$1" limit="${2:-90}"
    for _ in $(seq 1 "$limit"); do
        if curl -s -o /dev/null --max-time 2 "$url"; then return 0; fi
        sleep 1
    done
    echo "timed out waiting for $url" >&2
    return 1
}

# run_contract PACKAGE: runs the contract test of one client package. The
# caller exports LOCARR_TEST_* variables first.
run_contract() {
    local pkg="./internal/downloaders/$1/"
    if [ "${LOCARR_GO_DOCKER:-}" = "1" ]; then
        local envs=()
        while IFS='=' read -r k _; do envs+=(-e "$k"); done < <(env | grep '^LOCARR_TEST_')
        docker run --rm -u "$PUID:$PGID" -e HOME=/tmp -e GOCACHE=/tmp/gocache -e GOFLAGS=-buildvcs=false \
            -v "${LOCARR_GOMOD_VOLUME:-locarr-test-gomod}:/go/pkg/mod" -v "${LOCARR_GOCACHE_VOLUME:-locarr-test-gocache}:/tmp/gocache" \
            -v "$ROOT:/src" -v "$SEED_DIR:$SEED_DIR" -w /src/server --network host "${envs[@]}" \
            golang:1.26-trixie go test -tags clients -count=1 -v -run 'TestContract' "$pkg"
    else
        (cd "$ROOT/server" && go test -tags clients -count=1 -v -run 'TestContract' "$pkg")
    fi
}

# run_app_test NAME: runs one app-level test with the clients tag (the movie
# flow against the real client). The caller exports LOCARR_TEST_* variables first.
run_app_test() {
    local name="$1"
    if [ "${LOCARR_GO_DOCKER:-}" = "1" ]; then
        local envs=()
        while IFS='=' read -r k _; do envs+=(-e "$k"); done < <(env | grep '^LOCARR_TEST_')
        docker run --rm -u "$PUID:$PGID" -e HOME=/tmp -e GOCACHE=/tmp/gocache -e GOFLAGS=-buildvcs=false \
            -v "${LOCARR_GOMOD_VOLUME:-locarr-test-gomod}:/go/pkg/mod" -v "${LOCARR_GOCACHE_VOLUME:-locarr-test-gocache}:/tmp/gocache" \
            -v "$ROOT:/src" -v "$SEED_DIR:$SEED_DIR" -w /src/server --network host "${envs[@]}" \
            golang:1.26-trixie go test -tags clients -count=1 -v -run "$name" ./internal/app/
    else
        (cd "$ROOT/server" && go test -tags clients -count=1 -v -run "$name" ./internal/app/)
    fi
}
