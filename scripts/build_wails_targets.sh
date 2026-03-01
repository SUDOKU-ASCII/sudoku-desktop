#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT_DIR"

if ! command -v wails >/dev/null 2>&1; then
  echo "wails CLI not found. Install: go install github.com/wailsapp/wails/v2/cmd/wails@latest"
  exit 1
fi

# Build host platform package.
wails build -clean

echo "Host package built under ./build/bin"
echo "For cross-platform CI packaging, run this script on each target OS (Windows/macOS/Linux)."
