#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SOURCE="$ROOT/deployments/portable-demo"
VERSION="$(cd "$ROOT" && go run ./cmd/compatcheck -output summary | sed -E 's/^Praetor ([^ ]+).*/\1/')"
OFFLINE_PLATFORM=""
if [[ ${1:-} == --offline ]]; then
  OFFLINE_PLATFORM="${2:-}"
  shift 2 || true
  [[ $OFFLINE_PLATFORM == linux/amd64 || $OFFLINE_PLATFORM == linux/arm64 ]] || {
    echo "usage: $0 [--offline linux/amd64|linux/arm64] [output-directory]" >&2
    exit 2
  }
fi
OUTPUT="${1:-$ROOT/dist}"
SUFFIX=""
[[ -z $OFFLINE_PLATFORM ]] || SUFFIX="-${OFFLINE_PLATFORM//\//-}"
NAME="praetor-demo-$VERSION$SUFFIX"
STAGE="$(mktemp -d "${TMPDIR:-/tmp}/praetor-demo.XXXXXX")"
trap 'rm -rf "$STAGE"' EXIT

mkdir -p "$OUTPUT" "$STAGE/$NAME"
cp -R "$SOURCE"/. "$STAGE/$NAME/"
rm -f "$STAGE/$NAME/.env"

# Rebuild the image block from the compatibility manifest so the distributed
# bundle cannot silently drift from the declared component set.
IMAGE_ENV="$(cd "$ROOT" && go run ./cmd/compatcheck -output demo-env)"
awk '/^PRAETOR_REGISTRY=/{exit} {print}' "$SOURCE/.env.template" >"$STAGE/header"
awk 'found || /^# start.sh/{found=1; print}' "$SOURCE/.env.template" >"$STAGE/tail"
{
  cat "$STAGE/header"
  printf '%s\n\n' "$IMAGE_ENV"
  cat "$STAGE/tail"
} >"$STAGE/$NAME/.env.template"

if [[ -n $OFFLINE_PLATFORM ]]; then
  command -v docker >/dev/null 2>&1 || { echo "docker is required for offline packaging" >&2; exit 1; }
  images=()
  while IFS= read -r image; do images+=("$image"); done < <(
    docker compose --env-file "$STAGE/$NAME/.env.template" -f "$STAGE/$NAME/compose.yaml" config --images
  )
  for image in "${images[@]}"; do
    echo "Pulling $image for $OFFLINE_PLATFORM"
    docker pull --platform "$OFFLINE_PLATFORM" "$image"
  done
  docker image save -o "$STAGE/$NAME/images.tar" "${images[@]}"
  printf '%s\n' "$OFFLINE_PLATFORM" >"$STAGE/$NAME/OFFLINE_PLATFORM"
fi

(cd "$STAGE" && tar -czf "$OUTPUT/$NAME.tar.gz" "$NAME")
(cd "$STAGE" && zip -qr "$OUTPUT/$NAME.zip" "$NAME")

if command -v shasum >/dev/null 2>&1; then
  (cd "$OUTPUT" && shasum -a 256 "$NAME.tar.gz" "$NAME.zip" > "$NAME.sha256")
else
  (cd "$OUTPUT" && sha256sum "$NAME.tar.gz" "$NAME.zip" > "$NAME.sha256")
fi

printf 'Created:\n  %s\n  %s\n  %s\n' \
  "$OUTPUT/$NAME.tar.gz" \
  "$OUTPUT/$NAME.zip" \
  "$OUTPUT/$NAME.sha256"
