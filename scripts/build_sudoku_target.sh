#!/usr/bin/env bash
# Build the stock upstream Sudoku core (cmd/sudoku-tunnel) at the version pinned in go.mod.
# No source patching: everything the desktop app needs goes through the upstream
# external controller API.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"

GOOS="${GOOS:-$(go env GOOS)}"
GOARCH="${GOARCH:-$(go env GOARCH)}"
OUT_DIR="${OUT_DIR:-${ROOT_DIR}/runtime/bin/${GOOS}-${GOARCH}}"

mkdir -p "$OUT_DIR"
out="$OUT_DIR/sudoku"
if [[ "$GOOS" == "windows" ]]; then
  out+=".exe"
fi

ver="$(cd "$ROOT_DIR" && go list -m -f '{{.Version}}' github.com/SUDOKU-ASCII/sudoku)"
echo "[build] sudoku ${ver} ${GOOS}/${GOARCH} -> ${out}"

# `go install pkg@version` builds with upstream's own go.mod/go.sum (so its CLI-only
# dependencies never leak into this module). A throw-away GOPATH sidesteps the
# "cross-compiled binaries cannot go to GOBIN" restriction.
# Pin the already-resolved toolchain so the throw-away GOPATH never re-downloads Go itself.
GOROOT_RESOLVED="$(go env GOROOT)"
tmp="$(mktemp -d)"
trap 'chmod -R u+w "$tmp" 2>/dev/null; rm -rf "$tmp"' EXIT
(
  cd "$tmp"
  PATH="$GOROOT_RESOLVED/bin:$PATH" GOTOOLCHAIN=local GOROOT="$GOROOT_RESOLVED" GOPATH="$tmp/gopath" GOMODCACHE="$(go env GOMODCACHE)" GOFLAGS=-mod=mod   CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH"     go install -trimpath -ldflags "-s -w"     "github.com/SUDOKU-ASCII/sudoku/cmd/sudoku-tunnel@${ver}"
)
built="$(find "$tmp/gopath/bin" -type f -name 'sudoku-tunnel*' | head -n1)"
[[ -n "$built" ]] || { echo "[error] build produced no binary" >&2; exit 1; }
install -m 0755 "$built" "$out"
echo "[ok] sudoku ready at ${out}"
