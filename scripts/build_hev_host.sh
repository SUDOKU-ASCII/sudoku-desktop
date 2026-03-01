#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
HEV_REPO="${HEV_REPO:-https://github.com/heiher/hev-socks5-tunnel}"
OUT_DIR="${ROOT_DIR}/runtime/bin/$(go env GOOS)-$(go env GOARCH)"

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
out="$OUT_DIR/hev-socks5-tunnel"
if [[ "$(go env GOOS)" == "windows" ]]; then
  out+=".exe"
fi
cp "$work/hev/bin/hev-socks5-tunnel" "$out"
chmod +x "$out" || true

echo "HEV binary copied to $out"
