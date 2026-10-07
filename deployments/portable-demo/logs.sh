#!/usr/bin/env sh
set -eu
cd "$(dirname "$0")"
docker compose logs -f --tail=200
