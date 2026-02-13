#!/usr/bin/env bash
# Download relay leaf native libraries for embedding into the binary.
# Usage: ./scripts/download-libs.sh <platform> [platform...]
#
# Platforms: windows-x64, windows-x86, darwin-arm64, darwin-amd64,
#            linux-x64, linux-arm64
#
# Example:
#   ./scripts/download-libs.sh darwin-arm64 darwin-amd64   # macOS universal
#   ./scripts/download-libs.sh windows-x64                  # Windows 64-bit
#   ./scripts/download-libs.sh linux-x64                    # Linux amd64

set -euo pipefail

DEST_DIR="$(cd "$(dirname "$0")/.." && pwd)/pkg/relayleaf/libs"

SERVERS=(
  "https://release.prx.network"
)

platform_to_lib() {
  case "$1" in
    windows-x64)  echo "relay_leaf-windows-x64.dll" ;;
    windows-x86)  echo "relay_leaf-windows-x86.dll" ;;
    darwin-arm64)  echo "librelay_leaf-darwin-arm64.dylib" ;;
    darwin-amd64)  echo "librelay_leaf-darwin-amd64.dylib" ;;
    linux-x64)     echo "librelay_leaf-linux-x64.so" ;;
    linux-arm64)   echo "librelay_leaf-linux-arm64.so" ;;
    *) echo ""; return 1 ;;
  esac
}

download_lib() {
  local lib_name="$1"
  local dest="$DEST_DIR/$lib_name"

  for server in "${SERVERS[@]}"; do
    local url="$server/$lib_name"
    echo "  Trying $url ..."
    if curl -fSL --connect-timeout 15 --max-time 120 -o "$dest" "$url" 2>/dev/null; then
      echo "  Downloaded $lib_name ($(wc -c < "$dest" | tr -d ' ') bytes)"
      return 0
    fi
  done

  echo "  FAILED to download $lib_name from all servers"
  return 1
}

if [ $# -eq 0 ]; then
  echo "Usage: $0 <platform> [platform...]"
  echo "Platforms: windows-x64 windows-x86 darwin-arm64 darwin-amd64 linux-x64 linux-arm64"
  exit 1
fi

mkdir -p "$DEST_DIR"

failed=0
for platform in "$@"; do
  lib_name=$(platform_to_lib "$platform") || {
    echo "Unknown platform: $platform"
    failed=1
    continue
  }
  echo "Downloading $lib_name for $platform..."
  if ! download_lib "$lib_name"; then
    failed=1
  fi
done

echo ""
echo "Libraries in $DEST_DIR:"
ls -lh "$DEST_DIR"/ 2>/dev/null | grep -v total || echo "  (none)"

exit $failed
