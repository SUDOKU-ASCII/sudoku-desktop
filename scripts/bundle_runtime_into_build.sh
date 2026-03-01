#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT_DIR"

PLATFORM="${PLATFORM:-$(go env GOOS)/$(go env GOARCH)}"
GOOS="${PLATFORM%/*}"
GOARCH="${PLATFORM#*/}"
PLATFORM_DIR="${GOOS}-${GOARCH}"

SRC_DIR="${ROOT_DIR}/runtime/bin/${PLATFORM_DIR}"
if [[ ! -d "$SRC_DIR" ]]; then
  echo "Missing runtime binaries: ${SRC_DIR}" >&2
  exit 2
fi

BUILD_BIN="${ROOT_DIR}/build/bin"
if [[ "$GOOS" == "darwin" ]]; then
  APP_PATH="${BUILD_BIN}/sudoku-desktop.app"
  if [[ ! -d "$APP_PATH" ]]; then
    echo "Missing app bundle: ${APP_PATH}" >&2
    exit 3
  fi
  DEST_DIR="${APP_PATH}/Contents/Resources/runtime/bin/${PLATFORM_DIR}"
else
  DEST_DIR="${BUILD_BIN}/runtime/bin/${PLATFORM_DIR}"
fi

mkdir -p "$DEST_DIR"
cp -f "${SRC_DIR}/"* "$DEST_DIR/"

echo "[ok] Bundled runtime binaries -> ${DEST_DIR}"

