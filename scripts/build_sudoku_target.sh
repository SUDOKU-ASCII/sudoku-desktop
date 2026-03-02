#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"

GOOS="${GOOS:-$(go env GOOS)}"
GOARCH="${GOARCH:-$(go env GOARCH)}"
PLATFORM_DIR="${GOOS}-${GOARCH}"
OUT_DIR="${OUT_DIR:-${ROOT_DIR}/runtime/bin/${PLATFORM_DIR}}"
SUDOKU_REPO="${SUDOKU_REPO:-https://github.com/SUDOKU-ASCII/sudoku.git}"
SUDOKU_REF="${SUDOKU_REF:-main}"
PATCH_DIR="${PATCH_DIR:-${ROOT_DIR}/scripts/sudoku_patches}"

mkdir -p "$OUT_DIR"

out="$OUT_DIR/sudoku"
if [[ "$GOOS" == "windows" ]]; then
  out+=".exe"
fi

tmpdir="$(mktemp -d)"
cleanup() { rm -rf "$tmpdir"; }
trap cleanup EXIT

SUDOKU_DIR="${tmpdir}/sudoku"

echo "[fetch] sudoku ${SUDOKU_REF} (${SUDOKU_REPO})"
if command -v git >/dev/null 2>&1; then
  if ! git clone --depth 1 --branch "${SUDOKU_REF}" "${SUDOKU_REPO}" "${SUDOKU_DIR}"; then
    echo "[warn] git clone failed; falling back to tarball download"
    mkdir -p "${SUDOKU_DIR}"
    curl -fsSL "https://codeload.github.com/SUDOKU-ASCII/sudoku/tar.gz/${SUDOKU_REF}" \
      | tar -xz -C "${SUDOKU_DIR}" --strip-components=1
  fi
else
  echo "[warn] git not found; downloading tarball"
  mkdir -p "${SUDOKU_DIR}"
  curl -fsSL "https://codeload.github.com/SUDOKU-ASCII/sudoku/tar.gz/${SUDOKU_REF}" \
    | tar -xz -C "${SUDOKU_DIR}" --strip-components=1
fi

# Relax upstream go.mod patch version (go 1.26.0 -> go 1.26) for toolchain compatibility.
SUDOKU_DIR="${SUDOKU_DIR}" python3 - <<'PY'
from __future__ import annotations

import os
import pathlib
import re

root = pathlib.Path(os.environ["SUDOKU_DIR"])
path = root / "go.mod"
if not path.exists():
    raise SystemExit(0)
data = path.read_text(encoding="utf-8")

def repl(m: re.Match[str]) -> str:
    major = m.group(1)
    minor = m.group(2)
    return f"go {major}.{minor}"

new = re.sub(r"(?m)^go\s+(\d+)\.(\d+)\.\d+\s*$", repl, data)
if new != data:
    path.write_text(new, encoding="utf-8")
PY

if [[ -d "${PATCH_DIR}" ]]; then
  echo "[patch] applying sudoku patches from ${PATCH_DIR}"
  # Overlay patch tree into upstream repo.
  (
    cd "${PATCH_DIR}"
    tar -cf - .
  ) | (
    cd "${SUDOKU_DIR}"
    tar -xf -
  )
fi

# Patch dialTarget() to wrap conns for traffic stats (direct/proxy).
SUDOKU_DIR="${SUDOKU_DIR}" python3 - <<'PY'
from __future__ import annotations

import os
import pathlib

root = pathlib.Path(os.environ["SUDOKU_DIR"])
path = root / "internal/app/client_target.go"
data = path.read_text(encoding="utf-8")

needle = "func dialTarget("
start = data.find(needle)
if start == -1:
    raise SystemExit("dialTarget not found (upstream changed?)")

brace_start = data.find("{", start)
if brace_start == -1:
    raise SystemExit("dialTarget brace not found")

level = 0
end = None
for i in range(brace_start, len(data)):
    ch = data[i]
    if ch == "{":
        level += 1
    elif ch == "}":
        level -= 1
        if level == 0:
            end = i + 1
            break
if end is None:
    raise SystemExit("dialTarget end not found")

func_text = data[start:end]
if "wrapConnForTrafficStats" in func_text:
    raise SystemExit(0)

func_text = func_text.replace("return conn, true", "return wrapConnForTrafficStats(conn, true), true", 1)
func_text = func_text.replace("return dConn, true", "return wrapConnForTrafficStats(dConn, false), true", 1)

path.write_text(data[:start] + func_text + data[end:], encoding="utf-8")
print("[patch] updated", path)
PY

# Patch SOCKS5 UDP associate DIRECT path to avoid TUN self-loop for outbound UDP.
SUDOKU_DIR="${SUDOKU_DIR}" python3 - <<'PY'
from __future__ import annotations

import os
import pathlib

root = pathlib.Path(os.environ["SUDOKU_DIR"])
path = root / "internal/app/client_socks5.go"
data = path.read_text(encoding="utf-8")

if "udpWriteTo(" in data:
    raise SystemExit(0)

before = data

data = data.replace(
    "s.udpConn.WriteToUDP(payload, directAddr)",
    "udpWriteTo(s.udpConn, payload, directAddr, true)",
    1,
)
data = data.replace(
    "s.udpConn.WriteToUDP(resp, clientAddr)",
    "udpWriteTo(s.udpConn, resp, clientAddr, false)",
    2,
)

if data == before:
    raise SystemExit("failed to patch client_socks5.go (upstream changed?)")

path.write_text(data, encoding="utf-8")
print("[patch] updated", path)
PY

echo "[build] sudoku ${GOOS}/${GOARCH} (ref=${SUDOKU_REF}) -> ${out}"
(
  cd "${SUDOKU_DIR}"
  CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" \
    go build -mod=mod -tags sudoku_patch -trimpath -ldflags "-s -w" \
    -o "$out" \
    ./cmd/sudoku-tunnel
)

echo "[ok] sudoku ready at ${out}"
