#!/usr/bin/env sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
INSTALL_DIR=${TARISH_INSTALL_DIR:-}

detect_os() {
    os=$(uname -s | tr '[:upper:]' '[:lower:]')
    case "$os" in
        darwin)
            echo "macos"
            ;;
        linux)
            echo "linux"
            ;;
        *)
            echo "unsupported"
            ;;
    esac
}

detect_arch() {
    arch=$(uname -m)
    case "$arch" in
        x86_64|amd64)
            echo "amd64"
            ;;
        arm64|aarch64)
            echo "arm64"
            ;;
        *)
            echo "unsupported"
            ;;
    esac
}

OS=$(detect_os)
ARCH=$(detect_arch)

if [ "$OS" = "unsupported" ] || [ "$ARCH" = "unsupported" ]; then
    echo "Error: unsupported platform $(uname -s)/$(uname -m)" >&2
    exit 1
fi

BINARY_SOURCE="${SCRIPT_DIR}/binaries/tarish_${OS}_${ARCH}"

if [ ! -f "$BINARY_SOURCE" ]; then
    echo "Error: missing packaged binary ${BINARY_SOURCE}" >&2
    exit 1
fi

if [ -z "$INSTALL_DIR" ]; then
    if [ "$(id -u)" -eq 0 ]; then
        INSTALL_DIR="/usr/local/bin"
    else
        INSTALL_DIR="${HOME}/.local/bin"
    fi
fi

mkdir -p "$INSTALL_DIR"
TARGET_PATH="${INSTALL_DIR}/tarish"

echo "Installing tarish (${OS}/${ARCH}) to ${TARGET_PATH}"
install -m 0755 "$BINARY_SOURCE" "$TARGET_PATH"

echo ""
echo "Installed tarish to ${TARGET_PATH}"

case ":${PATH}:" in
    *:"${INSTALL_DIR}":*)
        ;;
    *)
        SHELL_NAME=$(basename "${SHELL:-sh}")
        PROFILE="~/.bashrc"
        if [ "$SHELL_NAME" = "zsh" ]; then
            PROFILE="~/.zshrc"
        fi

        echo ""
        echo "Warning: ${INSTALL_DIR} is not in your PATH."
        echo "To use tarish directly, run:"
        echo "  echo 'export PATH=\"\$PATH:${INSTALL_DIR}\"' >> ${PROFILE}"
        echo "  . ${PROFILE}"
        ;;
esac

echo ""
echo "Next step: run 'tarish install' to extract bundled mining assets."
