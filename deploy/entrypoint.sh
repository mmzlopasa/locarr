#!/bin/sh
# Runs Locarr as PUID:PGID (default 1000:1000). When the container starts as
# root, /config is handed to that user only if it is not theirs already, then
# root privileges are dropped. Started as a non-root user (user: in compose),
# it runs as that user directly. /data is never changed: its owner is yours.
set -eu

if [ "$(id -u)" = "0" ]; then
    PUID="${PUID:-1000}"
    PGID="${PGID:-1000}"
    case "$PUID$PGID" in
        *[!0-9]*) echo "PUID and PGID must be numbers" >&2; exit 1 ;;
    esac
    if [ "$PUID" = "0" ]; then
        echo "refusing to run as root (PUID=0); pick an unprivileged user" >&2
        exit 1
    fi
    mkdir -p "${LOCARR_CONFIG_DIR:-/config}"
    if [ "$(stat -c '%u:%g' "${LOCARR_CONFIG_DIR:-/config}")" != "$PUID:$PGID" ]; then
        chown -R "$PUID:$PGID" "${LOCARR_CONFIG_DIR:-/config}"
    fi
    umask "${UMASK:-002}"
    exec setpriv --reuid="$PUID" --regid="$PGID" --clear-groups --no-new-privs /app/locarr "$@"
fi
exec /app/locarr "$@"
