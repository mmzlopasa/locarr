#!/usr/bin/env bash
# Contract suite against a real Deluge (linuxserver image: daemon + Web UI,
# default Web UI password "deluge"). The Label plugin is enabled first, since
# Locarr's categories are Deluge labels.
set -euo pipefail
. "$(dirname "$0")/lib.sh"
NAME="$PREFIX-deluge"
IMAGE="${DELUGE_IMAGE:-lscr.io/linuxserver/deluge:latest}"
PORT="$(free_port)"
make_seed_dir
JAR="$(mktemp "${LOCARR_CLIENTS_TMP:-${TMPDIR:-/tmp}}/locarr-deluge-jar.XXXXXX")"
trap 'rm -f "$JAR"; cleanup_container "$NAME"' EXIT
docker rm -f "$NAME" >/dev/null 2>&1 || true
docker run -d --name "$NAME" -e PUID="$PUID" -e PGID="$PGID" -e TZ=Etc/UTC \
    -p "127.0.0.1:$PORT:8112" -v "$SEED_DIR:/downloads/seed" "$IMAGE" >/dev/null
URL="http://127.0.0.1:$PORT/json"
wait_for "http://127.0.0.1:$PORT/"
rpc() { curl -fsS -b "$JAR" -c "$JAR" -H 'Content-Type: application/json' -d "$1" "$URL"; }
for _ in $(seq 1 60); do
    if rpc '{"method":"auth.login","params":["deluge"],"id":1}' 2>/dev/null | grep -q '"result": *true'; then break; fi
    sleep 1
done
# Connect the Web UI to its daemon, then enable the Label plugin.
for _ in $(seq 1 60); do
    host="$(rpc '{"method":"web.get_hosts","params":[],"id":2}' | python3 -c 'import json,sys; h=json.load(sys.stdin)["result"]; print(h[0][0] if h else "")')"
    if [ -n "$host" ]; then
        rpc "{\"method\":\"web.connect\",\"params\":[\"$host\"],\"id\":3}" >/dev/null || true
        if rpc '{"method":"web.connected","params":[],"id":4}' | grep -q '"result": *true'; then break; fi
    fi
    sleep 1
done
rpc '{"method":"core.enable_plugin","params":["Label"],"id":5}' >/dev/null
for _ in $(seq 1 30); do
    if rpc '{"method":"core.get_enabled_plugins","params":[],"id":6}' | grep -q Label; then break; fi
    sleep 1
done
export LOCARR_TEST_DELUGE_HOST=127.0.0.1 LOCARR_TEST_DELUGE_PORT="$PORT" LOCARR_TEST_DELUGE_PASS=deluge \
    LOCARR_TEST_DELUGE_SEED_CLIENT=/downloads/seed LOCARR_TEST_DELUGE_SEED_LOCAL="$SEED_DIR"
run_contract deluge
