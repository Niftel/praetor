#!/usr/bin/env sh
set -eu
cd "$(dirname "$0")"

command -v docker >/dev/null 2>&1 || { echo "Docker Desktop is required." >&2; exit 1; }
docker compose version >/dev/null 2>&1 || { echo "Docker Compose v2 is required." >&2; exit 1; }

random_hex() { openssl rand -hex "$1"; }
if [ ! -f .env ]; then
  command -v openssl >/dev/null 2>&1 || { echo "OpenSSL is required for first-run secret generation." >&2; exit 1; }
  cp .env.template .env
  postgres_password=$(random_hex 16)
  jwt_secret=$(random_hex 32)
  praetor_secret_key=$(random_hex 16)
  internal_token=$(random_hex 32)
  admin_password=$(random_hex 12)
  sed -i.bak \
    -e "s/^POSTGRES_PASSWORD=GENERATE_ME$/POSTGRES_PASSWORD=$postgres_password/" \
    -e "s/^JWT_SECRET=GENERATE_ME$/JWT_SECRET=$jwt_secret/" \
    -e "s/^PRAETOR_SECRET_KEY=GENERATE_ME$/PRAETOR_SECRET_KEY=$praetor_secret_key/" \
    -e "s/^PRAETOR_INTERNAL_TOKEN=GENERATE_ME$/PRAETOR_INTERNAL_TOKEN=$internal_token/" \
    -e "s/^PRAETOR_ADMIN_PASSWORD=GENERATE_ME$/PRAETOR_ADMIN_PASSWORD=$admin_password/" .env
  rm -f .env.bak
  chmod 600 .env
fi

if grep -q GENERATE_ME .env; then
  echo ".env still contains an ungenerated secret; remove it and run start.sh again." >&2
  exit 1
fi

if [ -f images.tar ]; then
  echo "Loading the bundled Praetor images..."
  docker load -i images.tar
else
  echo "Pulling the pinned Praetor component set..."
  docker compose pull
fi
docker compose up -d --remove-orphans

echo "Waiting for the UI..."
ui_port=$(sed -n 's/^PRAETOR_UI_PORT=//p' .env)
i=0
until curl -fsS "http://127.0.0.1:${ui_port:-3000}/api/v1/ping" >/dev/null 2>&1; do
  i=$((i + 1))
  [ "$i" -lt 90 ] || { docker compose ps; echo "Praetor did not become ready. Run ./logs.sh." >&2; exit 1; }
  sleep 2
done

admin_user=$(sed -n 's/^PRAETOR_ADMIN_USERNAME=//p' .env)
admin_password=$(sed -n 's/^PRAETOR_ADMIN_PASSWORD=//p' .env)
echo
echo "Praetor is ready: http://localhost:${ui_port:-3000}"
echo "Username: $admin_user"
echo "Password: $admin_password"
echo "Credentials are stored locally in $(pwd)/.env"
