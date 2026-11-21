#!/usr/bin/env bash
# aria2 (Alpine with the aria2 package, JSON-RPC on :6800 with a secret).
source "$(dirname "$0")/lib.sh"
NAME="$PREFIX-aria2"
IMAGE="${ARIA2_IMAGE:-alpine:3}"
SECRET="locarr-test-secret"
make_seed_dir
trap 'cleanup_container "$NAME"' EXIT
PORT="$(free_port)"
# The seed folder is the category folder "locarr-test" inside the download folder /downloads.
docker run -d --name "$NAME" -p "127.0.0.1:$PORT:6800" -v "$SEED_DIR:/downloads/locarr-test" "$IMAGE" \
    sh -c "apk add --no-cache aria2 >/dev/null && exec aria2c --enable-rpc --rpc-listen-all --rpc-listen-port=6800 \
        --rpc-secret=$SECRET --dir=/downloads --enable-dht=false --bt-enable-lpd=false --enable-peer-exchange=false \
        --seed-ratio=0 --follow-torrent=mem --quiet" >/dev/null
wait_for "http://127.0.0.1:$PORT/jsonrpc" 120
export LOCARR_TEST_ARIA2_HOST=127.0.0.1 LOCARR_TEST_ARIA2_PORT="$PORT" LOCARR_TEST_ARIA2_APIKEY="$SECRET" \
    LOCARR_TEST_ARIA2_DIR=/downloads LOCARR_TEST_ARIA2_CATEGORY=locarr-test \
    LOCARR_TEST_ARIA2_SEED_CLIENT=/downloads/locarr-test LOCARR_TEST_ARIA2_SEED_LOCAL="$SEED_DIR"
run_contract aria2
