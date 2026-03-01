#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"

GOOS="${GOOS:-$(go env GOOS)}"
GOARCH="${GOARCH:-$(go env GOARCH)}"
PLATFORM_DIR="${GOOS}-${GOARCH}"
OUT_DIR="${OUT_DIR:-${ROOT_DIR}/runtime/bin/${PLATFORM_DIR}}"
SUDOKU_PKG="${SUDOKU_PKG:-github.com/saba-futai/sudoku/cmd/sudoku-tunnel}"
SUDOKU_VERSION="${SUDOKU_VERSION:-v0.3.0}"

mkdir -p "$OUT_DIR"

out="$OUT_DIR/sudoku"
if [[ "$GOOS" == "windows" ]]; then
  out+=".exe"
fi

tmpdir="$(mktemp -d)"
cleanup() { rm -rf "$tmpdir"; }
trap cleanup EXIT

cat >"$tmpdir/go.mod" <<'MOD'
module sudoku-builder

go 1.26
MOD

echo "[build] sudoku ${GOOS}/${GOARCH} (${SUDOKU_VERSION}) -> ${out}"
(
  cd "$tmpdir"
  go get "${SUDOKU_PKG}@${SUDOKU_VERSION}"
  CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" \
    go build -trimpath -ldflags "-s -w" \
    -o "$out" \
    "$SUDOKU_PKG"
)

echo "[ok] sudoku ready at ${out}"
