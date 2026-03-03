#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT_DIR"

if ! command -v wails3 >/dev/null 2>&1; then
  echo "wails3 CLI not found. Install: go install github.com/wailsapp/wails/v3/cmd/wails3@latest"
  exit 1
fi

# Build host platform package.
wails3 build

echo "Host package built under ./build/bin"
echo "For cross-platform CI packaging, run this script on each target OS (Windows/macOS/Linux)."
