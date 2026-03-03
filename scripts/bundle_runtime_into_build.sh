#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT_DIR"

APP_NAME="${APP_NAME:-}"
APP_BUNDLE_NAME="${APP_BUNDLE_NAME:-}"
if [[ -z "${APP_NAME}" ]]; then
  PYTHON="${PYTHON:-python3}"
  if ! command -v "$PYTHON" >/dev/null 2>&1; then
    PYTHON="python"
  fi

  APP_NAME="$(
    "$PYTHON" - <<'PY'
import json
from pathlib import Path

p = Path("wails.json")
data = json.loads(p.read_text(encoding="utf-8"))
print(data.get("outputfilename", "app"))
PY
  )"
fi

WAILS_JSON_NAME=""
WAILS_JSON_OUTPUT=""
PYTHON="${PYTHON:-python3}"
if ! command -v "$PYTHON" >/dev/null 2>&1; then
  PYTHON="python"
fi
read -r WAILS_JSON_NAME WAILS_JSON_OUTPUT < <(
  "$PYTHON" - <<'PY'
import json
from pathlib import Path

data = json.loads(Path("wails.json").read_text(encoding="utf-8"))
print(data.get("name", ""), data.get("outputfilename", ""))
PY
)

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
  declare -a candidates=()
  if [[ -n "$APP_BUNDLE_NAME" ]]; then
    candidates+=("$APP_BUNDLE_NAME")
  fi
  if [[ -n "$APP_NAME" ]]; then
    candidates+=("$APP_NAME")
  fi
  if [[ -n "$WAILS_JSON_NAME" ]]; then
    candidates+=("$WAILS_JSON_NAME")
  fi
  if [[ -n "$WAILS_JSON_OUTPUT" ]]; then
    candidates+=("$WAILS_JSON_OUTPUT")
  fi

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
      echo "Missing app bundle under ${BUILD_BIN} (tried: ${candidates[*]:-<none>})." >&2
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
else
  echo "[ok] Runtime binaries are embedded into executable for ${PLATFORM_DIR}; skip filesystem bundle."
  exit 0
fi

mkdir -p "$DEST_DIR"
cp -f "${SRC_DIR}/"* "$DEST_DIR/"

echo "[ok] Bundled runtime binaries -> ${DEST_DIR}"
