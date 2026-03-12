#!/usr/bin/env bash
set -euo pipefail

if [[ "${OSTYPE:-}" != darwin* ]]; then
  echo "macOS signing is only supported on macOS hosts." >&2
  exit 1
fi

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT_DIR"

APP_PATH="${1:-${APP_PATH:-build/bin/sudoku4x4.app}}"
IDENTITY="${MACOS_SIGN_IDENTITY:-saba-futai}"
SIGN_REQUIRED="${MACOS_SIGN_REQUIRED:-0}"

if [[ ! -d "$APP_PATH" ]]; then
  echo "App bundle not found: $APP_PATH" >&2
  exit 2
fi

if ! command -v codesign >/dev/null 2>&1; then
  echo "codesign is not available on this machine." >&2
  exit 3
fi

if ! security find-identity -v -p codesigning 2>/dev/null | grep -Fq "$IDENTITY"; then
  if [[ "$SIGN_REQUIRED" == "1" ]]; then
    echo "Signing identity not found in keychain: $IDENTITY" >&2
    echo "Install/import the certificate first, or override MACOS_SIGN_IDENTITY." >&2
    exit 4
  fi
  echo "[warn] Signing identity not found in keychain: $IDENTITY; skipping macOS codesign." >&2
  exit 0
fi

sign_target() {
  local target="$1"
  codesign --force --sign "$IDENTITY" --timestamp=none "$target"
}

should_sign_file() {
  local target="$1"

  if [[ -L "$target" ]]; then
    return 1
  fi

  if [[ -d "$target" ]]; then
    case "$target" in
      *.app|*.framework|*.xpc|*.appex|*.bundle)
        return 0
        ;;
    esac
    return 1
  fi

  case "$target" in
    *.dylib|*.so)
      return 0
      ;;
  esac

  if [[ -x "$target" ]]; then
    local kind
    kind="$(file -b "$target" 2>/dev/null || true)"
    case "$kind" in
      *Mach-O*)
        return 0
        ;;
    esac
  fi

  return 1
}

mapfile -t targets < <(
  find "$APP_PATH" -depth \
    \( -type f -o -type d \) \
    ! -path '*/_CodeSignature/*' \
    ! -path '*/Contents/_CodeSignature/*' \
    -print
)

xattr -cr "$APP_PATH"

signed_any=0
for target in "${targets[@]}"; do
  if should_sign_file "$target"; then
    sign_target "$target"
    signed_any=1
  fi
done

sign_target "$APP_PATH"

codesign --verify --deep --strict --verbose=2 "$APP_PATH"
spctl --assess --type execute --verbose=2 "$APP_PATH" || true

if [[ "$signed_any" -eq 1 ]]; then
  echo "[ok] Signed app bundle: ${APP_PATH} (identity: ${IDENTITY})"
else
  echo "[warn] Signed bundle root only: ${APP_PATH} (identity: ${IDENTITY})"
fi
