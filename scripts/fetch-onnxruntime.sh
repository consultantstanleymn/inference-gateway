#!/usr/bin/env bash
# Download the pinned ONNX Runtime CPU library into third_party/ and verify its SHA-256.
set -euo pipefail
VERSION=1.31.0
SHA256=cc5c72baf5ae5c8238a6841f0897227be2d02826b9cf98eaf02fdefaeeb03a57
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEST="$ROOT/third_party"
if [ -f "$DEST/libonnxruntime.so" ]; then echo "already present"; exit 0; fi
mkdir -p "$DEST"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
curl -fsSL -o "$TMP/ort.tgz" \
  "https://github.com/microsoft/onnxruntime/releases/download/v${VERSION}/onnxruntime-linux-x64-${VERSION}.tgz"
echo "${SHA256}  $TMP/ort.tgz" | sha256sum -c -
tar -xzf "$TMP/ort.tgz" -C "$TMP"
cp -L "$TMP/onnxruntime-linux-x64-${VERSION}/lib/libonnxruntime.so.${VERSION}" "$DEST/libonnxruntime.so"
echo "installed $DEST/libonnxruntime.so"
