#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"

HEV_VERSION="${HEV_VERSION:-2.14.4}"
GOOS="${GOOS:-$(go env GOOS)}"
GOARCH="${GOARCH:-$(go env GOARCH)}"
PLATFORM_DIR="${GOOS}-${GOARCH}"
OUT_DIR="${OUT_DIR:-${ROOT_DIR}/runtime/bin/${PLATFORM_DIR}}"

mkdir -p "$OUT_DIR"

PYTHON="${PYTHON:-python3}"
if ! command -v "$PYTHON" >/dev/null 2>&1; then
  PYTHON="python"
fi

asset=""
case "${GOOS}/${GOARCH}" in
  darwin/arm64) asset="hev-socks5-tunnel-darwin-arm64" ;;
  darwin/amd64) asset="hev-socks5-tunnel-darwin-x86_64" ;;
  linux/arm64) asset="hev-socks5-tunnel-linux-arm64" ;;
  linux/amd64) asset="hev-socks5-tunnel-linux-x86_64" ;;
  windows/amd64) asset="hev-socks5-tunnel-win64.zip" ;;
  *)
    echo "Unsupported platform for HEV: ${GOOS}/${GOARCH}" >&2
    exit 2
    ;;
esac

url="https://github.com/heiher/hev-socks5-tunnel/releases/download/${HEV_VERSION}/${asset}"
work="$(mktemp -d)"
cleanup() { rm -rf "$work"; }
trap cleanup EXIT

echo "[download] ${url}"
curl -fsSL --retry 3 --retry-delay 2 -o "$work/$asset" "$url"

if [[ "$asset" == *.zip ]]; then
  echo "[extract] ${asset}"
  if command -v unzip >/dev/null 2>&1; then
    unzip -q "$work/$asset" -d "$work/unzipped"
  else
    "$PYTHON" - "$work/$asset" "$work/unzipped" <<'PY'
import sys
import zipfile
from pathlib import Path

zip_path = Path(sys.argv[1])
out_dir = Path(sys.argv[2])
out_dir.mkdir(parents=True, exist_ok=True)

with zipfile.ZipFile(zip_path) as z:
    z.extractall(out_dir)
PY
  fi
  # Expected layout:
  #   hev-socks5-tunnel/hev-socks5-tunnel.exe
  #   hev-socks5-tunnel/msys-2.0.dll
  #   hev-socks5-tunnel/wintun.dll
  src_dir="$(find "$work/unzipped" -type d -name "hev-socks5-tunnel" -print | head -n 1)"
  if [[ -z "${src_dir}" ]]; then
    echo "Failed to locate hev-socks5-tunnel directory inside zip" >&2
    exit 3
  fi
  cp -f "$src_dir/"* "$OUT_DIR/"
else
  out="$OUT_DIR/hev-socks5-tunnel"
  if [[ "$GOOS" == "windows" ]]; then
    out+=".exe"
  fi
  cp -f "$work/$asset" "$out"
fi

chmod +x "$OUT_DIR/hev-socks5-tunnel" 2>/dev/null || true
chmod +x "$OUT_DIR/hev-socks5-tunnel.exe" 2>/dev/null || true

if [[ "${GOOS}" == "windows" ]]; then
  for dep in "hev-socks5-tunnel.exe" "wintun.dll" "msys-2.0.dll"; do
    if [[ ! -f "$OUT_DIR/$dep" ]]; then
      echo "Missing Windows dependency after extraction: ${OUT_DIR}/${dep}" >&2
      exit 4
    fi
  done
fi

echo "[ok] HEV ready at ${OUT_DIR}"
