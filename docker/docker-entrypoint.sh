#!/bin/sh
set -eu

DATA_PATH="${DATA_DIR:-/data}"
TARGET_UID="${PUID:-65532}"
TARGET_GID="${PGID:-65532}"

case "$TARGET_UID" in
  ''|*[!0-9]*)
    echo "[entrypoint] ERROR: PUID must be a numeric user ID (got '$TARGET_UID')" >&2
    exit 1
    ;;
esac
case "$TARGET_GID" in
  ''|*[!0-9]*)
    echo "[entrypoint] ERROR: PGID must be a numeric group ID (got '$TARGET_GID')" >&2
    exit 1
    ;;
esac

if [ "$(id -u)" = "0" ]; then
  mkdir -p "$DATA_PATH"
  chown -RP "${TARGET_UID}:${TARGET_GID}" "$DATA_PATH"
  chmod 0700 "$DATA_PATH"
  exec su-exec "${TARGET_UID}:${TARGET_GID}" /usr/local/bin/ovh-nexus "$@"
fi

mkdir -p "$DATA_PATH"
chmod 0700 "$DATA_PATH" 2>/dev/null || true
exec /usr/local/bin/ovh-nexus "$@"
