#!/usr/bin/env bash
set -euo pipefail

container="${1:?usage: wait-for-postgres.sh CONTAINER [TIMEOUT_SECONDS]}"
timeout_seconds="${2:-60}"
postgres_user="${POSTGRES_USER:-postgres}"
postgres_db="${POSTGRES_DB:-postgres}"
deadline=$((SECONDS + timeout_seconds))

# The official image starts a temporary PostgreSQL server while initializing a
# fresh data directory, stops it, and then execs the final server as PID 1.
# pg_isready alone can therefore report success immediately before a restart.
while (( SECONDS < deadline )); do
  pid_one="$(docker exec "$container" sh -c 'cat /proc/1/comm' 2>/dev/null | tr -d '\r\n' || true)"
  if [[ "$pid_one" == "postgres" ]] && \
    docker exec "$container" pg_isready -U "$postgres_user" -d "$postgres_db" >/dev/null 2>&1; then
    exit 0
  fi
  sleep 1
done

echo "PostgreSQL container $container did not reach its final ready state within ${timeout_seconds}s" >&2
docker logs --tail 100 "$container" >&2 || true
exit 1
