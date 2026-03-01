#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
OUT_ROOT="${ROOT_DIR}/runtime/bin"
SUDOKU_VERSION="${SUDOKU_VERSION:-v0.3.0}"
TARGETS=(
  "darwin/amd64"
  "darwin/arm64"
  "linux/amd64"
  "linux/arm64"
  "windows/amd64"
)

tmpdir="$(mktemp -d)"
cleanup() { rm -rf "$tmpdir"; }
trap cleanup EXIT

cat >"$tmpdir/go.mod" <<MOD
module sudoku-builder

go 1.26

require github.com/saba-futai/sudoku ${SUDOKU_VERSION}
MOD

(
  cd "$tmpdir"
  go mod tidy
)

for target in "${TARGETS[@]}"; do
  os="${target%/*}"
  arch="${target#*/}"
  out_dir="${OUT_ROOT}/${os}-${arch}"
  mkdir -p "$out_dir"

  ext=""
  if [[ "$os" == "windows" ]]; then
    ext=".exe"
  fi

  out_file="${out_dir}/sudoku${ext}"
  echo "[build] ${os}/${arch} -> ${out_file}"

  (
    cd "$tmpdir"
    CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
      go build -trimpath -ldflags "-s -w" \
      -o "$out_file" \
      github.com/saba-futai/sudoku/cmd/sudoku-tunnel
  )
done

echo "Sudoku binaries are ready under ${OUT_ROOT}" 
