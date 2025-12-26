#!/bin/bash
set -e

BASE_URL="https://file.aooo.nl/tarish"

# Detect OS and arch
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
[[ "$ARCH" == "x86_64" ]] && ARCH="amd64"
[[ "$ARCH" == "aarch64" || "$ARCH" == "arm64" ]] && ARCH="arm64"
[[ "$OS" == "darwin" ]] && OS="macos"

BINARY="tarish_${OS}_${ARCH}"
URL="${BASE_URL}/dist/${BINARY}"

# Determine install location
if [ "$(id -u)" -eq 0 ]; then
    INSTALL_DIR="/usr/local/bin"
    SUDO=""
else
    INSTALL_DIR="$HOME/.local/bin"
    SUDO=""
    mkdir -p "$INSTALL_DIR"
fi

echo "Installing tarish (${OS}/${ARCH}) to ${INSTALL_DIR}..."

# Download to temp file first
TMP_FILE=$(mktemp)
curl -fsSL "$URL" -o "$TMP_FILE"
chmod +x "$TMP_FILE"

# Move to install directory
mv "$TMP_FILE" "${INSTALL_DIR}/tarish"

echo ""
echo "Installed tarish to ${INSTALL_DIR}/tarish"
echo ""

# Check PATH
if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
    SHELL_NAME=$(basename "$SHELL")
    PROFILE="~/.bashrc"
    if [ "$SHELL_NAME" = "zsh" ]; then
        PROFILE="~/.zshrc"
    fi

    echo -e "\033[33mWarning: ${INSTALL_DIR} is not in your PATH.\033[0m"
    echo "To use 'tarish' command, run:"
    echo ""
    echo "  echo 'export PATH=\"\$PATH:${INSTALL_DIR}\"' >> ${PROFILE}"
    echo "  source ${PROFILE}"
    echo ""
fi

echo "Run: tarish install"
