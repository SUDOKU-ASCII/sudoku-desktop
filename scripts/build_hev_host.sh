#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
HEV_REPO="${HEV_REPO:-https://github.com/heiher/hev-socks5-tunnel}"
OUT_DIR="${ROOT_DIR}/runtime/bin/$(go env GOOS)-$(go env GOARCH)"
HEV_VERSION="${HEV_VERSION:-2.14.4}"

if [[ "$(go env GOOS)" == "windows" ]]; then
  echo "[note] Windows host detected; using release assets to include wintun/msys dependencies"
  GOOS=windows GOARCH="$(go env GOARCH)" HEV_VERSION="${HEV_VERSION}" OUT_DIR="${OUT_DIR}" "${ROOT_DIR}/scripts/fetch_hev_release.sh"
  exit 0
fi

work="$(mktemp -d)"
cleanup() { rm -rf "$work"; }
trap cleanup EXIT

echo "[clone] ${HEV_REPO}"
git clone --recursive "$HEV_REPO" "$work/hev"

echo "[build] host binary"
(
  cd "$work/hev"
  make exec
)

mkdir -p "$OUT_DIR"
bin_dir="$work/hev/bin"
if [[ ! -d "$bin_dir" ]]; then
  echo "Missing build output directory: $bin_dir" >&2
  exit 2
fi

echo "[copy] ${bin_dir} -> ${OUT_DIR}"
find "$bin_dir" -maxdepth 1 -type f -print0 | while IFS= read -r -d '' f; do
  cp -f "$f" "$OUT_DIR/"
done

chmod +x "$OUT_DIR/hev-socks5-tunnel" 2>/dev/null || true

echo "HEV output ready at $OUT_DIR"
