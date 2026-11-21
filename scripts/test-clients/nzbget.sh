#!/usr/bin/env bash
# Contract suite against a real NZBGet (linuxserver image). No news server is
# configured, so jobs never download; the suite checks the API behaviour only.
. "$(dirname "$0")/lib.sh"
NAME="$PREFIX-nzbget"
IMAGE="${NZBGET_IMAGE:-lscr.io/linuxserver/nzbget:latest}"
PORT="$(free_port)"
USER_="nzbget"
PASS_="locarr-test-$RANDOM"
make_seed_dir
trap 'cleanup_container "$NAME"' EXIT
cleanup_container "$NAME"
docker run -d --name "$NAME" -e PUID="$PUID" -e PGID="$PGID" -e TZ=Etc/UTC \
    -e NZBGET_USER="$USER_" -e NZBGET_PASS="$PASS_" \
    -p "127.0.0.1:$PORT:6789" "$IMAGE" >/dev/null
wait_for "http://127.0.0.1:$PORT/jsonrpc" 120
rpc() { curl -s -u "$USER_:$PASS_" -H 'Content-Type: application/json' -d "$1" "http://127.0.0.1:$PORT/jsonrpc"; }
for _ in $(seq 1 30); do
    rpc '{"method":"version","params":[],"id":1}' | grep -q '"result"' && break
    sleep 1
done
echo "NZBGet $(rpc '{"method":"version","params":[],"id":1}')"
# Categories live in the configuration; add the suite's category and reload.
docker exec "$NAME" sh -c 'f=/config/nzbget.conf; grep -q "^Category9.Name=locarr-test" $f || printf "\nCategory9.Name=locarr-test\nCategory9.DestDir=\n" >> $f'
rpc '{"method":"reload","params":[],"id":2}' >/dev/null || true
for _ in $(seq 1 30); do
    sleep 1
    rpc '{"method":"config","params":[],"id":3}' | grep -q 'locarr-test' && break
done
export LOCARR_TEST_NZBGET_HOST=127.0.0.1 LOCARR_TEST_NZBGET_PORT="$PORT" \
    LOCARR_TEST_NZBGET_USER="$USER_" LOCARR_TEST_NZBGET_PASS="$PASS_"
run_contract nzbget
