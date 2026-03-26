#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OUTPUT_DIR="${SCRIPT_DIR}/dist/release"
VERSION=""
CLIENT_ARCHIVE=""
SERVER_ARCHIVE=""

usage() {
    cat <<EOF
Usage: ./build-complete-release.sh [options]

Combine a Tarish client bundle with a tarish-server release archive into a
complete release bundle for GitHub Releases.

Options:
  --version <value>         Release version to embed in archive names
  --client-archive <path>   Path to tarish-client_<version>_bundle.tar.gz
  --server-archive <path>   Path to tarish-server_<version>_<os>_<arch>.tar.gz
  --output-dir <path>       Output directory for the archive (default: dist/release)
  -h, --help                Show this help
EOF
}

while [ $# -gt 0 ]; do
    case "$1" in
        --version)
            shift
            VERSION="${1:-}"
            ;;
        --client-archive)
            shift
            CLIENT_ARCHIVE="${1:-}"
            ;;
        --server-archive)
            shift
            SERVER_ARCHIVE="${1:-}"
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

if [ -z "${VERSION}" ] || [ -z "${CLIENT_ARCHIVE}" ] || [ -z "${SERVER_ARCHIVE}" ]; then
    echo "Error: --version, --client-archive, and --server-archive are required" >&2
    usage >&2
    exit 1
fi

if [ ! -f "${CLIENT_ARCHIVE}" ]; then
    echo "Error: client archive not found: ${CLIENT_ARCHIVE}" >&2
    exit 1
fi

if [ ! -f "${SERVER_ARCHIVE}" ]; then
    echo "Error: server archive not found: ${SERVER_ARCHIVE}" >&2
    exit 1
fi

SERVER_BASENAME="$(basename "${SERVER_ARCHIVE}")"
TARGET_SUFFIX="${SERVER_BASENAME#tarish-server_${VERSION}_}"
TARGET_SUFFIX="${TARGET_SUFFIX%.tar.gz}"

if [ "${TARGET_SUFFIX}" = "${SERVER_BASENAME}" ]; then
    echo "Error: unable to derive target suffix from ${SERVER_BASENAME}" >&2
    exit 1
fi

ARCHIVE_STEM="tarish-complete_${VERSION}_${TARGET_SUFFIX}"
ARCHIVE_PATH="${OUTPUT_DIR}/${ARCHIVE_STEM}.tar.gz"
CHECKSUM_PATH="${ARCHIVE_PATH}.sha256"
STAGING_ROOT="$(mktemp -d)"
PACKAGE_DIR="${STAGING_ROOT}/${ARCHIVE_STEM}"
CLIENT_EXTRACT="${STAGING_ROOT}/client-extract"
SERVER_EXTRACT="${STAGING_ROOT}/server-extract"

cleanup() {
    rm -rf "${STAGING_ROOT}"
}
trap cleanup EXIT

mkdir -p "${OUTPUT_DIR}" "${PACKAGE_DIR}" "${CLIENT_EXTRACT}" "${SERVER_EXTRACT}"

echo "=== Building complete Tarish release bundle ==="
echo "Version: ${VERSION}"
echo "Target: ${TARGET_SUFFIX}"
echo "Client archive: ${CLIENT_ARCHIVE}"
echo "Server archive: ${SERVER_ARCHIVE}"
echo "Output: ${ARCHIVE_PATH}"
echo ""

tar -xzf "${CLIENT_ARCHIVE}" -C "${CLIENT_EXTRACT}"
tar -xzf "${SERVER_ARCHIVE}" -C "${SERVER_EXTRACT}"

CLIENT_ROOT="$(find "${CLIENT_EXTRACT}" -mindepth 1 -maxdepth 1 -type d | head -n 1)"
SERVER_ROOT="$(find "${SERVER_EXTRACT}" -mindepth 1 -maxdepth 1 -type d | head -n 1)"

if [ -z "${CLIENT_ROOT}" ] || [ -z "${SERVER_ROOT}" ]; then
    echo "Error: failed to extract expected client/server bundle directories" >&2
    exit 1
fi

mkdir -p "${PACKAGE_DIR}/client" "${PACKAGE_DIR}/server"
cp -R "${CLIENT_ROOT}/." "${PACKAGE_DIR}/client/"
cp -R "${SERVER_ROOT}/." "${PACKAGE_DIR}/server/"
printf '%s\n' "${VERSION}" > "${PACKAGE_DIR}/VERSION"

cat > "${PACKAGE_DIR}/README.md" <<EOF
# Tarish Complete Release Bundle

This package includes:

- \`client/\`: Tarish client binaries for Linux and macOS plus \`install-client.sh\`
- \`server/\`: tarish-server bundle for ${TARGET_SUFFIX}

## Quick start

Install the client from the extracted \`client/\` directory:

\`\`\`bash
cd client
./install-client.sh
tarish install
\`\`\`

Install the server from the extracted \`server/\` directory:

\`\`\`bash
cd server
sudo ./install.sh
\`\`\`
EOF

echo "Creating archive..."
tar -czf "${ARCHIVE_PATH}" -C "${STAGING_ROOT}" "${ARCHIVE_STEM}"
shasum -a 256 "${ARCHIVE_PATH}" > "${CHECKSUM_PATH}"

echo ""
echo "Complete release bundle created:"
echo "  ${ARCHIVE_PATH}"
echo "  ${CHECKSUM_PATH}"
