#!/usr/bin/env bash
# Runs the download client contract suite against real clients in Docker, one
# client at a time (each container is removed before the next starts).
#
#   scripts/test-clients.sh                 all clients
#   scripts/test-clients.sh qbittorrent sabnzbd
set -euo pipefail
DIR="$(cd "$(dirname "$0")" && pwd)/test-clients"
clients=("$@")
if [ ${#clients[@]} -eq 0 ]; then
    clients=(qbittorrent transmission deluge rtorrent aria2 sabnzbd nzbget)
fi
failed=()
for c in "${clients[@]}"; do
    echo "==> $c"
    if ! bash "$DIR/$c.sh"; then failed+=("$c"); fi
done
if [ ${#failed[@]} -gt 0 ]; then
    echo "failed: ${failed[*]}" >&2
    exit 1
fi
echo "all passed: ${clients[*]}"
