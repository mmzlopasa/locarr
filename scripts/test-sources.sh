#!/usr/bin/env bash
# Imports from real applications in Docker, one at a time: each script starts
# the official (linuxserver) image, seeds it through the application's own
# public API (a throwaway test instance), runs the importer against it
# (go test -tags sources) and removes the container.
#
#   scripts/test-sources.sh                       all of them
#   scripts/test-sources.sh sonarr radarr qbittorrent
#
#   LOCARR_GO_DOCKER=1   run go in a golang container instead of the local toolchain
set -euo pipefail
DIR="$(cd "$(dirname "$0")" && pwd)/test-sources"
apps=("$@")
if [ ${#apps[@]} -eq 0 ]; then
    apps=(sonarr radarr lidarr prowlarr bazarr qbittorrent sabnzbd)
fi
. "$DIR/lib.sh"
start_fakes
trap stop_fakes EXIT
failed=()
for a in "${apps[@]}"; do
    echo "==> $a"
    if ! bash "$DIR/$a.sh"; then failed+=("$a"); fi
done
if [ ${#failed[@]} -gt 0 ]; then
    echo "failed: ${failed[*]}" >&2
    exit 1
fi
echo "all passed: ${apps[*]}"
