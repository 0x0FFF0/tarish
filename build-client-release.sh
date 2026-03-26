#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OUTPUT_DIR="${SCRIPT_DIR}/dist/release"
VERSION=""

usage() {
    cat <<EOF
Usage: ./build-client-release.sh [options]

Build a multi-platform Tarish client bundle that includes:
  - Linux amd64 client binary
  - Linux arm64 client binary
  - macOS amd64 client binary
  - macOS arm64 client binary
  - install-client.sh for local zsh/bash installation
  - VERSION

Options:
  --version <value>     Release version to embed in binaries and archive names
  --output-dir <path>   Output directory for the archive (default: dist/release)
  -h, --help            Show this help
EOF
}

while [ $# -gt 0 ]; do
    case "$1" in
        --version)
            shift
            VERSION="${1:-}"
            ;;
        --output-dir)
            shift
            OUTPUT_DIR="${1:-}"
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            echo "Error: unknown option '$1'" >&2
            usage >&2
            exit 1
            ;;
    esac

    if [ -z "${1:-}" ]; then
        echo "Error: missing value for option" >&2
        usage >&2
        exit 1
    fi

    shift
done

if [ -z "${VERSION}" ]; then
    VERSION="$(git -C "${SCRIPT_DIR}" describe --tags --always --dirty 2>/dev/null || true)"
fi

if [ -z "${VERSION}" ]; then
    VERSION="$(git -C "${SCRIPT_DIR}" rev-parse --short HEAD 2>/dev/null || echo dev)"
fi

ARCHIVE_STEM="tarish-client_${VERSION}_bundle"
ARCHIVE_PATH="${OUTPUT_DIR}/${ARCHIVE_STEM}.tar.gz"
CHECKSUM_PATH="${ARCHIVE_PATH}.sha256"
STAGING_ROOT="$(mktemp -d)"
PACKAGE_DIR="${STAGING_ROOT}/${ARCHIVE_STEM}"
BINARY_DIR="${PACKAGE_DIR}/binaries"

cleanup() {
    rm -rf "${STAGING_ROOT}"
}
trap cleanup EXIT

mkdir -p "${OUTPUT_DIR}" "${BINARY_DIR}"

TARGETS=(
    "linux/amd64"
    "linux/arm64"
    "darwin/amd64"
    "darwin/arm64"
)

echo "=== Building tarish client release bundle ==="
echo "Version: ${VERSION}"
echo "Output: ${ARCHIVE_PATH}"
echo ""

for target in "${TARGETS[@]}"; do
    IFS='/' read -r GOOS GOARCH <<< "${target}"
    OS_NAME="${GOOS}"
    if [ "${GOOS}" = "darwin" ]; then
        OS_NAME="macos"
    fi

    OUTPUT_PATH="${BINARY_DIR}/tarish_${OS_NAME}_${GOARCH}"

    echo "Building client for ${GOOS}/${GOARCH}..."
    (
        cd "${SCRIPT_DIR}"
        env CGO_ENABLED=0 GOOS="${GOOS}" GOARCH="${GOARCH}" \
            go build -trimpath -ldflags="-s -w -X main.Version=${VERSION}" -o "${OUTPUT_PATH}" .
    )
done

install -m 0755 "${SCRIPT_DIR}/packaging/install-client.sh" "${PACKAGE_DIR}/install-client.sh"
printf '%s\n' "${VERSION}" > "${PACKAGE_DIR}/VERSION"
cat > "${PACKAGE_DIR}/README.md" <<EOF
# Tarish Client Bundle

This bundle contains Tarish client binaries for Linux and macOS on amd64 and arm64.

## Install from the extracted bundle

\`\`\`bash
./install-client.sh
tarish install
\`\`\`

Set \`TARISH_INSTALL_DIR\` if you want to override the install destination.
EOF

echo "Creating archive..."
tar -czf "${ARCHIVE_PATH}" -C "${STAGING_ROOT}" "${ARCHIVE_STEM}"
shasum -a 256 "${ARCHIVE_PATH}" > "${CHECKSUM_PATH}"

echo ""
echo "Client release bundle created:"
echo "  ${ARCHIVE_PATH}"
echo "  ${CHECKSUM_PATH}"
