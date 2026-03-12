#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT_DIR"

APP_NAME="${APP_NAME:-sudoku4x4}"
APP_BUNDLE_NAME="${APP_BUNDLE_NAME:-${APP_NAME}}"

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
  APP_PATH=""
  declare -a candidates=(
    "${APP_BUNDLE_NAME}"
    "${APP_NAME}"
    "4x4-sudoku"
    "sudoku4x4"
  )

  for c in "${candidates[@]}"; do
    if [[ -d "${BUILD_BIN}/${c}.app" ]]; then
      APP_PATH="${BUILD_BIN}/${c}.app"
      break
    fi
  done

  if [[ -z "$APP_PATH" ]]; then
    shopt -s nullglob
    apps=("${BUILD_BIN}"/*.app)
    shopt -u nullglob
    if [[ "${#apps[@]}" -eq 1 ]]; then
      APP_PATH="${apps[0]}"
      echo "[warn] App bundle name mismatch; auto-detected: ${APP_PATH##*/}" >&2
    else
      echo "Missing app bundle under ${BUILD_BIN}" >&2
      if [[ "${#apps[@]}" -gt 0 ]]; then
        echo "Found app bundles:" >&2
        for a in "${apps[@]}"; do
          echo "  - ${a##*/}" >&2
        done
      fi
      exit 3
    fi
  fi

  DEST_DIR="${APP_PATH}/Contents/Resources/runtime/bin/${PLATFORM_DIR}"
  mkdir -p "$DEST_DIR"
  cp -f "${SRC_DIR}/"* "$DEST_DIR/"
  echo "[ok] Bundled runtime binaries -> ${DEST_DIR}"
  exit 0
fi

echo "[ok] Runtime binaries are embedded into executable for ${PLATFORM_DIR}; skip filesystem bundle."
