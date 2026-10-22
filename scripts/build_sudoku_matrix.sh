#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
OUT_ROOT="${ROOT_DIR}/runtime/bin"
TARGETS=(
  "darwin/amd64"
  "darwin/arm64"
  "linux/amd64"
  "linux/arm64"
  "windows/amd64"
)

if [[ "${RELEASE_TAG:-}" == *rc* ]]; then
  filtered=()
  for target in "${TARGETS[@]}"; do
    case "$target" in
      darwin/amd64|linux/arm64) ;;
      *) filtered+=("$target") ;;
    esac
  done
  TARGETS=("${filtered[@]}")
fi

for target in "${TARGETS[@]}"; do
  GOOS="${target%/*}" GOARCH="${target#*/}" \
    OUT_DIR="${OUT_ROOT}/${target%/*}-${target#*/}" \
    "${ROOT_DIR}/scripts/build_sudoku_target.sh"
done
