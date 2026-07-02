#!/usr/bin/env bash
# mirror-pip.sh — mirror Ansible + pip dependencies into Gitea's PyPI registry.
#
# Builds a wheelhouse (every wheel in the dependency closure, for linux amd64 +
# arm64 at the target Python) and uploads it to Gitea, so execution-pack builds
# pip-install from Gitea only (reproducible / air-gapped, no PyPI at build time).
#
# Wheels are fetched in a python:<ver> container (to get the right linux wheels)
# and uploaded from the host via curl straight to Gitea's PyPI API — we don't use
# twine, whose strict client-side metadata validation rejects some valid wheels
# (e.g. docker-7.x, metadata 2.4). Gitea just needs content + sha256_digest +
# name + version.
#
#   make mirror-pip
#   REQS="ansible docker" ./scripts/mirror-pip.sh
#
# Config (env, with defaults):
#   REQS         space-separated requirements  (default: ansible ansible-core docker jmespath netaddr)
#   PY_VERSION   target Python (major.minor)   (default: 3.11)
#   GITEA_URL    Gitea base URL                (default: http://localhost:3002)
#   GITEA_OWNER  package owner                 (default: praetor)
#   GITEA_TOKEN  API token with write:package  (required)
set -euo pipefail

REQS="${REQS:-ansible ansible-core docker jmespath netaddr}"
PY_VERSION="${PY_VERSION:-3.11}"
PY_ABI="cp${PY_VERSION//./}"          # 3.11 -> cp311
GITEA_URL="${GITEA_URL:-http://localhost:3002}"
GITEA_OWNER="${GITEA_OWNER:-praetor}"
: "${GITEA_TOKEN:?GITEA_TOKEN is required (needs write:package scope)}"
command -v docker >/dev/null || { echo "error: docker not found" >&2; exit 1; }

REPO="${GITEA_URL%/}/api/packages/${GITEA_OWNER}/pypi"
X86_PLATS="--platform manylinux2014_x86_64 --platform manylinux_2_17_x86_64 --platform manylinux_2_28_x86_64"
ARM_PLATS="--platform manylinux2014_aarch64 --platform manylinux_2_17_aarch64 --platform manylinux_2_28_aarch64"

WH="$(mktemp -d)"; trap 'rm -rf "$WH"' EXIT

echo "Mirroring pip wheelhouse -> ${GITEA_OWNER}/pypi"
echo "  requirements: ${REQS}"
echo "  python: ${PY_VERSION} (${PY_ABI}), arches: amd64 + arm64"

# --- download the wheel closure for both arches (pure-python wheels dedup) ---
docker run --rm -v "$WH:/wh" \
  -e REQS="${REQS}" -e PY_VERSION="${PY_VERSION}" -e PY_ABI="${PY_ABI}" \
  -e X86_PLATS="${X86_PLATS}" -e ARM_PLATS="${ARM_PLATS}" \
  "python:${PY_VERSION}-slim" bash -euo pipefail -c '
    common="--only-binary=:all: --python-version ${PY_VERSION} --implementation cp --abi ${PY_ABI} -d /wh -q"
    pip download ${REQS} ${common} ${X86_PLATS}
    pip download ${REQS} ${common} ${ARM_PLATS}
  '
echo "==> downloaded $(ls "$WH" | wc -l | tr -d ' ') wheels"

# --- upload each wheel to Gitea via curl (content + sha256 + name + version) --
fail=0
for whl in "$WH"/*.whl; do
  base="$(basename "$whl" .whl)"
  name="$(echo "$base" | cut -d- -f1)"
  ver="$(echo "$base" | cut -d- -f2)"
  sha="$(shasum -a 256 "$whl" | awk '{print $1}')"
  body="$(curl -s -w '\n%{http_code}' -H "Authorization: token ${GITEA_TOKEN}" \
    -F "content=@${whl}" -F "sha256_digest=${sha}" -F "name=${name}" -F "version=${ver}" \
    "${REPO}")"
  code="${body##*$'\n'}"
  case "$code" in
    200|201) echo "   uploaded $(basename "$whl")" ;;
    409)     echo "   skip (exists) $(basename "$whl")" ;;
    *) if echo "$body" | grep -qi "already exist"; then echo "   skip (exists) $(basename "$whl")";
       else echo "   FAILED ($code) $(basename "$whl"): ${body%$'\n'*}"; fail=1; fi ;;
  esac
done
[ "$fail" = 0 ] || { echo "one or more uploads failed" >&2; exit 1; }

# --- verify a few resolve from Gitea's simple index ------------------------
echo "==> verifying packages resolve from Gitea PyPI"
for p in ansible ansible-core docker pyyaml; do
  code=$(curl -s -o /dev/null -w "%{http_code}" "${REPO}/simple/${p}/")
  echo "   simple/${p}/ -> HTTP ${code}"
done
echo "==> done. pack builds pip-install via: ${REPO}/simple/"
