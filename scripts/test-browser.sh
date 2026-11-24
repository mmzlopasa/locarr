#!/usr/bin/env bash
# Runs the built-in browser's integration tests: builds the companion browser
# image (browser/), starts it on a private Docker network and runs
# `go test -tags browser` in a Go container on the same network, where the
# tests serve fake sites behind a browser check.
#
#   LOCARR_BROWSER_TEST_KEEP=1   keep the image afterwards
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
NET="locarr-browser-test"
IMAGE="locarr-browser:test"
TOKEN="test-$(od -An -N12 -tx1 /dev/urandom | tr -d ' \n')"
GO_IMAGE="golang:1.26-trixie"

cleanup() {
    docker rm -f locarr-browser-test locarr-browser-test-go >/dev/null 2>&1 || true
    docker network rm "$NET" >/dev/null 2>&1 || true
    if [ -z "${LOCARR_BROWSER_TEST_KEEP:-}" ]; then docker rmi "$IMAGE" >/dev/null 2>&1 || true; fi
}
trap cleanup EXIT
cleanup

docker build -q -t "$IMAGE" "$ROOT/browser" >/dev/null
docker network create "$NET" >/dev/null
# The same limits as the compose service.
docker run -d --name locarr-browser-test --network "$NET" -e LOCARR_BROWSER_TOKEN="$TOKEN" \
    --read-only --tmpfs /tmp:size=512m --cap-drop ALL --security-opt no-new-privileges:true \
    --memory 1g --pids-limit 512 "$IMAGE" >/dev/null

docker run --rm --name locarr-browser-test-go --network "$NET" --network-alias sites \
    --user "$(id -u):$(id -g)" -e HOME=/tmp -e GOFLAGS=-buildvcs=false -e GOCACHE=/tmp/gocache -e GOMODCACHE=/tmp/gomod \
    -e LOCARR_TEST_BROWSER_URL=http://locarr-browser-test:8490 -e LOCARR_TEST_BROWSER_TOKEN="$TOKEN" \
    -e LOCARR_TEST_SITE_HOST=sites -v "$ROOT/server:/src" -w /src "$GO_IMAGE" \
    go test -tags browser -count=1 -v ./internal/indexers/browsertest/ \
    || { docker logs locarr-browser-test 2>&1 | tail -40; exit 1; }
