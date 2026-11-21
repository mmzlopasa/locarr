#!/usr/bin/env bash
# Contract suite against a real Transmission (linuxserver image).
set -euo pipefail
. "$(dirname "$0")/lib.sh"
NAME="$PREFIX-transmission"
IMAGE="${TRANSMISSION_IMAGE:-lscr.io/linuxserver/transmission:latest}"
PORT="$(free_port)"
make_seed_dir
trap 'cleanup_container "$NAME"' EXIT
docker rm -f "$NAME" >/dev/null 2>&1 || true
docker run -d --name "$NAME" -e PUID="$PUID" -e PGID="$PGID" -e TZ=Etc/UTC -e USER=locarr -e PASS=locarr-test \
    -p "127.0.0.1:$PORT:9091" -v "$SEED_DIR:/downloads/seed" "$IMAGE" >/dev/null
# The RPC answers 401/409 once it is up.
wait_for "http://127.0.0.1:$PORT/transmission/rpc"
export LOCARR_TEST_TRANSMISSION_HOST=127.0.0.1 LOCARR_TEST_TRANSMISSION_PORT="$PORT" \
    LOCARR_TEST_TRANSMISSION_USER=locarr LOCARR_TEST_TRANSMISSION_PASS=locarr-test \
    LOCARR_TEST_TRANSMISSION_SEED_CLIENT=/downloads/seed LOCARR_TEST_TRANSMISSION_SEED_LOCAL="$SEED_DIR"
run_contract transmission
run_app_test TestMovieFlowTransmission
