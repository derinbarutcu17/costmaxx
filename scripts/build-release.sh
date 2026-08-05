#!/usr/bin/env bash
set -euo pipefail

VERSION="${1:-1.0.0}"
OUTPUT_DIR="${2:-dist}"

echo "Building CostMax v${VERSION}..."

PLATFORMS=("darwin/amd64" "darwin/arm64" "linux/amd64" "linux/arm64" "windows/amd64")

mkdir -p "${OUTPUT_DIR}"

for PLATFORM in "${PLATFORMS[@]}"; do
  GOOS="${PLATFORM%/*}"
  GOARCH="${PLATFORM#*/}"
  EXT=""

  if [ "${GOOS}" = "windows" ]; then
    EXT=".exe"
  fi

  BINARY_NAME="costmaxx-${GOOS}-${GOARCH}${EXT}"
  echo "  Building ${BINARY_NAME}..."

  GOOS="${GOOS}" GOARCH="${GOARCH}" go build \
    -buildvcs=false \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o "${OUTPUT_DIR}/${BINARY_NAME}" \
    ./cmd/costmax/

  SHA256=$(shasum -a 256 "${OUTPUT_DIR}/${BINARY_NAME}" | cut -d' ' -f1)
  echo "${SHA256}  ${BINARY_NAME}" >> "${OUTPUT_DIR}/checksums.txt"
done

echo "Done. Binaries in ${OUTPUT_DIR}/"
echo "Checksums:"
cat "${OUTPUT_DIR}/checksums.txt"
