#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")" && pwd)"

PREFIX="${PREFIX:-$HOME/.local}"
BIN_DIR="${PREFIX}/bin"
APP_DIR="${PREFIX}/share/applications"
ICON_DIR="${PREFIX}/share/icons/hicolor/512x512/apps"

APP_NAME="sudoku4x4"
SRC_BIN="${ROOT_DIR}/${APP_NAME}"
SRC_ICON="${ROOT_DIR}/${APP_NAME}.png"
SRC_DESKTOP_IN="${ROOT_DIR}/${APP_NAME}.desktop.in"

if [[ ! -f "${SRC_BIN}" ]]; then
  echo "Missing binary: ${SRC_BIN}" >&2
  exit 2
fi
if [[ ! -f "${SRC_ICON}" ]]; then
  echo "Missing icon: ${SRC_ICON}" >&2
  exit 2
fi
if [[ ! -f "${SRC_DESKTOP_IN}" ]]; then
  echo "Missing desktop template: ${SRC_DESKTOP_IN}" >&2
  exit 2
fi

exec_path="${BIN_DIR}/${APP_NAME}"
desktop_path="${APP_DIR}/${APP_NAME}.desktop"
icon_path="${ICON_DIR}/${APP_NAME}.png"

mkdir -p "${BIN_DIR}" "${APP_DIR}" "${ICON_DIR}"

install -m 0755 "${SRC_BIN}" "${exec_path}"
install -m 0644 "${SRC_ICON}" "${icon_path}"

tmp="$(mktemp)"
cleanup() { rm -f "${tmp}"; }
trap cleanup EXIT

escaped_exec_path="${exec_path//&/\\&}"
sed "s|@EXEC@|${escaped_exec_path}|g" "${SRC_DESKTOP_IN}" > "${tmp}"
install -m 0644 "${tmp}" "${desktop_path}"

if command -v update-desktop-database >/dev/null 2>&1; then
  update-desktop-database "${APP_DIR}" >/dev/null 2>&1 || true
fi
if command -v gtk-update-icon-cache >/dev/null 2>&1; then
  gtk-update-icon-cache -f -t "${PREFIX}/share/icons/hicolor" >/dev/null 2>&1 || true
fi

echo "[ok] Installed:"
echo "  - ${exec_path}"
echo "  - ${desktop_path}"
echo "  - ${icon_path}"
echo ""
echo "You can launch it via your desktop menu, or run:"
echo "  ${exec_path}"
