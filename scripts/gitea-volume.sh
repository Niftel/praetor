#!/usr/bin/env bash
# gitea-volume.sh — manage the external `gitea-data` Docker volume that holds
# Gitea's config, DB, repos, and the Execution Pack registry.
#
# The volume is declared `external: true` in docker-compose.yml ON PURPOSE: it must
# survive `docker compose down -v` (which would otherwise wipe the pack registry).
# The trade-off is that a fresh checkout has no such volume, so `docker compose up`
# fails until it is created. This script creates it and backs it up/restores it.
#
# Usage:
#   scripts/gitea-volume.sh init              # create the volume if absent (run once on a fresh checkout)
#   scripts/gitea-volume.sh backup [FILE]     # tar the volume to FILE (default: gitea-data-<date>.tar.gz)
#   scripts/gitea-volume.sh restore FILE      # replace the volume's contents from FILE
#
# Config:
#   VOLUME   volume name (default: gitea-data)
set -euo pipefail

VOLUME="${VOLUME:-gitea-data}"
cmd="${1:-}"

exists() { docker volume inspect "$VOLUME" >/dev/null 2>&1; }

case "$cmd" in
  init)
    if exists; then
      echo "volume '$VOLUME' already exists"
    else
      docker volume create "$VOLUME" >/dev/null
      echo "created volume '$VOLUME'"
    fi
    ;;

  backup)
    exists || { echo "error: volume '$VOLUME' does not exist (run: $0 init)" >&2; exit 1; }
    out="${2:-gitea-data-backup.tar.gz}"
    # Stream a tar of the volume out through a throwaway container.
    docker run --rm -v "$VOLUME":/data:ro -v "$(pwd)":/backup alpine \
      tar -czf "/backup/$out" -C /data .
    echo "backed up '$VOLUME' -> $out"
    ;;

  restore)
    src="${2:-}"
    [ -n "$src" ] && [ -f "$src" ] || { echo "usage: $0 restore FILE (existing tar.gz)" >&2; exit 1; }
    if ! exists; then docker volume create "$VOLUME" >/dev/null; fi
    # Wipe then extract, so the restore is exact (not a merge). Gitea must be stopped.
    docker run --rm -v "$VOLUME":/data -v "$(pwd)":/backup alpine \
      sh -c "rm -rf /data/* /data/..?* /data/.[!.]* 2>/dev/null; tar -xzf \"/backup/$src\" -C /data"
    echo "restored '$VOLUME' <- $src (restart gitea-host to pick it up)"
    ;;

  *)
    echo "usage: $0 {init|backup [FILE]|restore FILE}" >&2
    exit 1
    ;;
esac
