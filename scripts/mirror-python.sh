#!/usr/bin/env bash
# mirror-python.sh — mirror the pinned standalone CPython runtime into Gitea.
#
# The execution-pack build needs a relocatable CPython per arch. Upstream ships
# these as prebuilt tarballs (astral-sh/python-build-standalone). We mirror the
# pinned build into Gitea's generic package registry so pack builds are
# reproducible / air-gapped and never depend on GitHub at build time.
#
#   make mirror-python
#   PY_VERSION=3.11.9 PBS_TAG=20240814 ./scripts/mirror-python.sh
#
# Config (env, with defaults matching build/ansible-runtime/Dockerfile):
#   PY_VERSION   CPython version           (default 3.11.9)
#   PBS_TAG      python-build-standalone tag (default 20240814)
#   GITEA_URL    Gitea base URL            (default http://localhost:3002)
#   GITEA_OWNER  package owner             (default praetor)
#   GITEA_TOKEN  API token (required)
set -euo pipefail

PY_VERSION="${PY_VERSION:-3.11.9}"
PBS_TAG="${PBS_TAG:-20240814}"
GITEA_URL="${GITEA_URL:-http://localhost:3002}"
GITEA_OWNER="${GITEA_OWNER:-praetor}"
: "${GITEA_TOKEN:?GITEA_TOKEN is required (mint one: gitea admin user generate-access-token)}"

PKG="python-standalone"
PKG_VERSION="${PY_VERSION}+${PBS_TAG}"
GENERIC="${GITEA_URL%/}/api/packages/${GITEA_OWNER}/generic/${PKG}/${PKG_VERSION}"
AUTH=(-H "Authorization: token ${GITEA_TOKEN}")

# arch map: our arch name -> python-build-standalone triple
mirror_arch() {
  local arch="$1" pbs_arch="$2"
  local file="cpython-${PY_VERSION}+${PBS_TAG}-${pbs_arch}-unknown-linux-gnu-install_only.tar.gz"
  local url="https://github.com/astral-sh/python-build-standalone/releases/download/${PBS_TAG}/${file}"
  # store under a stable, arch-clear name in Gitea
  local dst="python-standalone-${PY_VERSION}-linux-${arch}.tar.gz"

  echo "==> ${arch}: fetching upstream"
  echo "    $url"
  curl -fsSL "$url" -o "${TMP}/${dst}"
  echo "    downloaded ${dst} ($(du -h "${TMP}/${dst}" | cut -f1))"

  echo "    uploading to Gitea generic registry as ${dst}"
  # generic registry rejects re-upload of an existing file; delete then put so
  # re-runs refresh the same version.
  curl -sS "${AUTH[@]}" -X DELETE "${GENERIC}/${dst}" >/dev/null 2>&1 || true
  # Gitea's generic registry reads the raw body only for application/octet-stream;
  # without it, it treats the PUT as multipart form-data and 500s.
  curl -fsS "${AUTH[@]}" -H "Content-Type: application/octet-stream" \
    -X PUT "${GENERIC}/${dst}" --data-binary "@${TMP}/${dst}" >/dev/null
  echo "    uploaded"
}

TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT

echo "Mirroring standalone CPython ${PKG_VERSION} -> ${GITEA_OWNER}/generic/${PKG}"
mirror_arch amd64 x86_64
mirror_arch arm64 aarch64

echo "==> verifying assets are downloadable from Gitea"
for arch in amd64 arm64; do
  dst="python-standalone-${PY_VERSION}-linux-${arch}.tar.gz"
  code=$(curl -s -o /dev/null -w "%{http_code}" "${GENERIC}/${dst}")
  echo "    ${dst}: HTTP ${code}"
  [ "$code" = "200" ] || { echo "error: ${dst} not downloadable" >&2; exit 1; }
done
echo "==> done. pack builds fetch Python from: ${GENERIC}/"
