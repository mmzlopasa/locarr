#!/usr/bin/env bash
# Runs the real Streamero image (with its PostgreSQL and Valkey) on a private
# Docker network, then a Locarr test in a golang container on the same
# network (go test -tags streamero). The test configures Streamero through its
# own API to use Locarr's Seerr/Radarr/Sonarr compatible URLs, uses Streamero's
# discover, request and upcoming endpoints, and checks what reached Locarr.
# Everything is removed afterwards.
#
#   STREAMERO_IMAGE        image to test (default ghcr.io/streamero-app/streamero:latest)
#   LOCARR_COMPAT_PREFIX   container, network and volume prefix (default locarr-compat)
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PREFIX="${LOCARR_COMPAT_PREFIX:-locarr-compat}"
IMAGE="${STREAMERO_IMAGE:-ghcr.io/streamero-app/streamero:latest}"
NET="$PREFIX-net"
SETUP_CODE="COMPAT-TEST-0001"
DB_PASSWORD="compat-test-$$"

cleanup() {
    docker rm -f -v "$PREFIX-streamero" "$PREFIX-pg" "$PREFIX-valkey" "$PREFIX-gotest" >/dev/null 2>&1 || true
    docker network rm "$NET" >/dev/null 2>&1 || true
}
trap cleanup EXIT
cleanup
docker network create "$NET" >/dev/null
docker run -d --name "$PREFIX-pg" --network "$NET" -e POSTGRES_USER=streamero -e POSTGRES_PASSWORD="$DB_PASSWORD" \
    -e POSTGRES_DB=streamero --tmpfs /var/lib/postgresql/data pgvector/pgvector:pg17 >/dev/null
docker run -d --name "$PREFIX-valkey" --network "$NET" valkey/valkey:8-alpine valkey-server --save "" --appendonly no >/dev/null
for _ in $(seq 1 60); do
    docker exec "$PREFIX-pg" pg_isready -U streamero -d streamero >/dev/null 2>&1 && break
    sleep 1
done
docker run -d --name "$PREFIX-streamero" --network "$NET" \
    -e STREAMERO_DB_URL="postgres://streamero:$DB_PASSWORD@$PREFIX-pg:5432/streamero?sslmode=disable" \
    -e STREAMERO_VALKEY_URL="redis://$PREFIX-valkey:6379/0" -e STREAMERO_SETUP_CODE="$SETUP_CODE" \
    -e STREAMERO_AUTO_MIGRATE=true -e STREAMERO_DISCOVERY=false -e STREAMERO_DLNA=false \
    "$IMAGE" serve >/dev/null
ready=""
for _ in $(seq 1 90); do
    if docker exec "$PREFIX-streamero" /app/streamero healthcheck >/dev/null 2>&1; then ready=1; break; fi
    sleep 1
done
if [ -z "$ready" ]; then
    docker logs --tail 50 "$PREFIX-streamero" >&2
    echo "Streamero did not start" >&2
    exit 1
fi
GOMOD="${LOCARR_GOMOD_VOLUME:-$PREFIX-gomod}"
GOCACHE_VOL="${LOCARR_GOCACHE_VOLUME:-$PREFIX-gocache}"
# Fresh volumes belong to root; the test runs as the calling user.
docker run --rm -v "$GOMOD:/m" -v "$GOCACHE_VOL:/c" alpine:3 chown "$(id -u):$(id -g)" /m /c
echo "Streamero $(docker exec "$PREFIX-streamero" /app/streamero version) is up"
docker run --rm --name "$PREFIX-gotest" --network "$NET" -u "$(id -u):$(id -g)" -e HOME=/tmp -e GOCACHE=/tmp/gocache \
    -e GOFLAGS=-buildvcs=false -v "$GOMOD:/go/pkg/mod" \
    -v "$GOCACHE_VOL:/tmp/gocache" -v "$ROOT:/src" -w /src/server \
    -e LOCARR_TEST_STREAMERO_URL="http://$PREFIX-streamero:8787" -e LOCARR_TEST_STREAMERO_SETUP="$SETUP_CODE" \
    -e LOCARR_TEST_LOCARR_HOST="$PREFIX-gotest" \
    golang:1.26-trixie go test -tags streamero -count=1 -v -run 'TestRealStreameroDiscover' ./internal/app/
