#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
WEB_DIR="${REPO_DIR}/web"
OUTPUT_DIR="${SCRIPT_DIR}/dist"

GOOS="$(go env GOOS)"
GOARCH="$(go env GOARCH)"
CC_VALUE=""
VERSION=""

usage() {
    cat <<EOF
Usage: ./server/build-release.sh [options]

Build a tarish-server release bundle that includes:
  - tarish-server binary
  - README.md
  - tarish-server.service
  - tarish-server.env.example
  - cloudflared.example.yml
  - VERSION

Options:
  --goos <value>        Target OS (default: host GOOS)
  --goarch <value>      Target architecture (default: host GOARCH)
  --cc <value>          C compiler to use for CGO builds
  --version <value>     Release version embedded in the bundle name
  --output-dir <path>   Output directory for the archive (default: server/dist)
  -h, --help            Show this help
EOF
}

while [ $# -gt 0 ]; do
    case "$1" in
        --goos)
            shift
            GOOS="${1:-}"
            ;;
        --goarch)
            shift
            GOARCH="${1:-}"
            ;;
        --cc)
            shift
            CC_VALUE="${1:-}"
            ;;
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
    VERSION="$(git -C "${REPO_DIR}" describe --tags --always --dirty 2>/dev/null || true)"
fi

if [ -z "${VERSION}" ]; then
    VERSION="$(git -C "${REPO_DIR}" rev-parse --short HEAD 2>/dev/null || echo dev)"
fi

if [ -z "${CC_VALUE}" ] && [ "${GOOS}" = "linux" ] && [ "${GOARCH}" = "arm64" ]; then
    CC_VALUE="aarch64-linux-gnu-gcc"
fi

ARCHIVE_STEM="tarish-server_${VERSION}_${GOOS}_${GOARCH}"
ARCHIVE_PATH="${OUTPUT_DIR}/${ARCHIVE_STEM}.tar.gz"
CHECKSUM_PATH="${ARCHIVE_PATH}.sha256"
STAGING_ROOT="$(mktemp -d)"
PACKAGE_DIR="${STAGING_ROOT}/${ARCHIVE_STEM}"

cleanup() {
    rm -rf "${STAGING_ROOT}"
}
trap cleanup EXIT

mkdir -p "${OUTPUT_DIR}" "${PACKAGE_DIR}"

echo "=== Building tarish-server release bundle ==="
echo "Target: ${GOOS}/${GOARCH}"
echo "Version: ${VERSION}"
echo "Output: ${ARCHIVE_PATH}"
echo ""

if [ ! -d "${WEB_DIR}/node_modules" ]; then
    echo "Installing frontend dependencies..."
    (
        cd "${WEB_DIR}"
        npm ci
    )
fi

echo "Building frontend..."
(
    cd "${WEB_DIR}"
    npm run build
)

echo "Refreshing embedded frontend assets..."
rm -rf "${SCRIPT_DIR}/web/dist"
mkdir -p "${SCRIPT_DIR}/web"
cp -r "${WEB_DIR}/dist" "${SCRIPT_DIR}/web/"

echo "Building server binary..."
BUILD_ENV=(
    CGO_ENABLED=1
    GOOS="${GOOS}"
    GOARCH="${GOARCH}"
)

if [ -n "${CC_VALUE}" ]; then
    BUILD_ENV+=("CC=${CC_VALUE}")
fi

(
    cd "${SCRIPT_DIR}"
    env "${BUILD_ENV[@]}" go build -trimpath -ldflags="-s -w" -o "${PACKAGE_DIR}/tarish-server" .
)

chmod +x "${PACKAGE_DIR}/tarish-server"
cp "${SCRIPT_DIR}/README.md" "${PACKAGE_DIR}/README.md"
cp "${SCRIPT_DIR}/tarish-server.service" "${PACKAGE_DIR}/tarish-server.service"
cp "${SCRIPT_DIR}/tarish-server.env.example" "${PACKAGE_DIR}/tarish-server.env.example"
cp "${SCRIPT_DIR}/cloudflared.example.yml" "${PACKAGE_DIR}/cloudflared.example.yml"
printf '%s\n' "${VERSION}" > "${PACKAGE_DIR}/VERSION"

echo "Creating archive..."
tar -czf "${ARCHIVE_PATH}" -C "${STAGING_ROOT}" "${ARCHIVE_STEM}"
shasum -a 256 "${ARCHIVE_PATH}" > "${CHECKSUM_PATH}"

echo ""
echo "Release bundle created:"
echo "  ${ARCHIVE_PATH}"
echo "  ${CHECKSUM_PATH}"
