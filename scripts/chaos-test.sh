#!/usr/bin/env bash
# Chaos test harness: brings up throwaway Postgres + JetStream NATS, runs each
# failure-injection test against a FRESH pair of containers (so durable
# JetStream/consumer state can't leak between tests), and tears everything down.
#
# Usage: scripts/chaos-test.sh
set -euo pipefail

cd "$(dirname "$0")/.."

DB_NAME=praetor-chaos-db
NATS_NAME=praetor-chaos-nats
DB_PORT=5548
NATS_PORT=4227
DB_URL="postgres://postgres:postgres@localhost:${DB_PORT}/praetor?sslmode=disable"
NATS_URL="nats://localhost:${NATS_PORT}"

teardown() { docker rm -f "$DB_NAME" "$NATS_NAME" >/dev/null 2>&1 || true; }
trap teardown EXIT

bring_up() {
  teardown
  docker run -d --name "$DB_NAME" -e POSTGRES_USER=postgres -e POSTGRES_PASSWORD=postgres \
    -e POSTGRES_DB=praetor -p ${DB_PORT}:5432 postgres:15 >/dev/null
  docker run -d --name "$NATS_NAME" -p ${NATS_PORT}:4222 nats:2.10-alpine -js -sd /data >/dev/null

  for _ in $(seq 1 30); do
    docker exec "$DB_NAME" pg_isready -U postgres >/dev/null 2>&1 && break
    sleep 1
  done
  # Use the real migrator so chaos tests exercise the same schema and terminal
  # helpers as a deployed platform. Partial migration lists silently drift.
  DATABASE_URL="$DB_URL" PRAETOR_ALLOW_INSECURE_DEFAULTS=true GOWORK=off \
    go run ./cmd/migrator >/dev/null
}

run_one() {
  local name=$1
  echo "=================== CHAOS: ${name} ==================="
  bring_up
  CHAOS_DB_CONTAINER="$DB_NAME" CHAOS_NATS_CONTAINER="$NATS_NAME" \
  TEST_DATABASE_URL="$DB_URL" TEST_NATS_URL="$NATS_URL" \
    GOWORK=off go test ./tests/chaos/ -run "$name" -count=1 -v -timeout 180s
}

run_one TestDBOutageDuringActiveExecution
run_one TestNATSRestartDurability

echo "=================== CHAOS: all passed ==================="
