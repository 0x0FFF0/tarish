#!/bin/bash
set -e

REPO="0x0FFF0/tarish"
INSTALL_DIR="/usr/local/bin"

# Detect OS and arch
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
[[ "$ARCH" == "x86_64" ]] && ARCH="amd64"
[[ "$ARCH" == "aarch64" || "$ARCH" == "arm64" ]] && ARCH="arm64"
[[ "$OS" == "darwin" ]] && OS="macos"

BINARY="tarish_${OS}_${ARCH}"
LATEST=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep tag_name | cut -d'"' -f4)
URL="https://github.com/${REPO}/releases/download/${LATEST}/${BINARY}"

echo "Installing tarish ${LATEST} (${OS}/${ARCH})..."
curl -fsSL "$URL" -o /tmp/tarish
chmod +x /tmp/tarish
mv /tmp/tarish "${INSTALL_DIR}/tarish"
echo "Installed to ${INSTALL_DIR}/tarish"
echo "Run: sudo tarish install"

