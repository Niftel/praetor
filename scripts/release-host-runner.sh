#!/usr/bin/env bash
# release-host-runner.sh — build the host-runner daemon for all supported arches
# and publish it as a versioned release in Gitea.
#
# Gitea is the source of truth for this infra artifact. The execution-pack build
# later pulls the daemon from here by version and bundles it into the finished
# pack; Praetor itself never handles the raw binary.
#
#   make release-host-runner VERSION=1.2.3
#   VERSION=1.2.3 ./scripts/release-host-runner.sh
#
# Config (env, with dev defaults):
#   GITEA_URL    Gitea base URL          (default http://localhost:3002)
#   GITEA_OWNER  repo owner              (default praetor)
#   GITEA_REPO   repo name               (default host-runner)
#   GITEA_TOKEN  API token (preferred). If unset, falls back to basic auth with
#   GITEA_USER / GITEA_PASSWORD          (default praetor / praetor)
set -euo pipefail

VERSION="${VERSION:-}"
[ -n "$VERSION" ] || { echo "error: VERSION is required (e.g. VERSION=1.2.3)" >&2; exit 1; }
TAG="v${VERSION#v}"

GITEA_URL="${GITEA_URL:-http://localhost:3002}"
GITEA_OWNER="${GITEA_OWNER:-praetor}"
GITEA_REPO="${GITEA_REPO:-host-runner}"
GITEA_USER="${GITEA_USER:-}"
GITEA_PASSWORD="${GITEA_PASSWORD:-}"
ARCHES="amd64 arm64"
API="${GITEA_URL%/}/api/v1"

# Require an explicit credential — no guessable default. Prefer a token; basic
# auth needs both user and password. Publishing to a real registry with baked-in
# praetor:praetor creds is a footgun, so fail closed instead.
if [ -n "${GITEA_TOKEN:-}" ]; then
  AUTH=(-H "Authorization: token ${GITEA_TOKEN}")
elif [ -n "$GITEA_USER" ] && [ -n "$GITEA_PASSWORD" ]; then
  AUTH=(-u "${GITEA_USER}:${GITEA_PASSWORD}")
else
  echo "error: set GITEA_TOKEN (preferred), or both GITEA_USER and GITEA_PASSWORD, to publish" >&2
  exit 1
fi

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"
command -v go >/dev/null || { echo "error: go toolchain not found" >&2; exit 1; }

OUT="$(mktemp -d)"
trap 'rm -rf "$OUT"' EXIT

# --- build every arch (static, reproducible-ish) ----------------------------
echo "==> building praetor-host-runner ${TAG}"
for arch in $ARCHES; do
  bin="$OUT/praetor-host-runner-linux-${arch}"
  CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath \
    -ldflags "-s -w -X main.version=${TAG}" \
    -o "$bin" ./cmd/host-runner
  echo "   built $(basename "$bin") ($(du -h "$bin" | cut -f1))"
done
( cd "$OUT" && shasum -a 256 praetor-host-runner-linux-* > SHA256SUMS )
echo "   wrote SHA256SUMS"

# --- ensure the Gitea repo exists -------------------------------------------
echo "==> ensuring ${GITEA_OWNER}/${GITEA_REPO} exists in Gitea"
if ! curl -sfS "${AUTH[@]}" "${API}/repos/${GITEA_OWNER}/${GITEA_REPO}" >/dev/null 2>&1; then
  curl -sfS "${AUTH[@]}" -H 'Content-Type: application/json' -X POST "${API}/user/repos" \
    -d "{\"name\":\"${GITEA_REPO}\",\"auto_init\":true,\"private\":false,\"description\":\"Praetor host-runner daemon release artifacts\"}" >/dev/null
  echo "   created repo"
else
  echo "   repo present"
fi

# --- idempotent: drop an existing release + tag for this version ------------
existing_id="$(curl -sS "${AUTH[@]}" "${API}/repos/${GITEA_OWNER}/${GITEA_REPO}/releases/tags/${TAG}" 2>/dev/null \
  | python3 -c 'import sys,json;
try: print(json.load(sys.stdin).get("id",""))
except Exception: print("")' 2>/dev/null || true)"
if [ -n "$existing_id" ]; then
  echo "==> replacing existing release ${TAG} (id ${existing_id})"
  curl -sfS "${AUTH[@]}" -X DELETE "${API}/repos/${GITEA_OWNER}/${GITEA_REPO}/releases/${existing_id}" >/dev/null || true
  curl -sfS "${AUTH[@]}" -X DELETE "${API}/repos/${GITEA_OWNER}/${GITEA_REPO}/tags/${TAG}" >/dev/null 2>&1 || true
fi

# --- create the release ------------------------------------------------------
echo "==> creating release ${TAG}"
rel_id="$(curl -sfS "${AUTH[@]}" -H 'Content-Type: application/json' -X POST \
  "${API}/repos/${GITEA_OWNER}/${GITEA_REPO}/releases" \
  -d "{\"tag_name\":\"${TAG}\",\"name\":\"${TAG}\",\"body\":\"host-runner ${TAG}\"}" \
  | python3 -c 'import sys,json;print(json.load(sys.stdin)["id"])')"
[ -n "$rel_id" ] || { echo "error: failed to create release" >&2; exit 1; }

# --- upload assets -----------------------------------------------------------
echo "==> uploading assets"
for f in "$OUT"/praetor-host-runner-linux-* "$OUT/SHA256SUMS"; do
  name="$(basename "$f")"
  curl -sfS "${AUTH[@]}" -X POST \
    "${API}/repos/${GITEA_OWNER}/${GITEA_REPO}/releases/${rel_id}/assets?name=${name}" \
    -F "attachment=@${f};type=application/octet-stream" >/dev/null
  echo "   uploaded ${name}"
done

echo "==> done: ${GITEA_URL%/}/${GITEA_OWNER}/${GITEA_REPO}/releases/tag/${TAG}"
