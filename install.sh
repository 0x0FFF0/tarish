#!/bin/bash
set -e

BASE_URL="https://file.aooo.nl/tarish"
INSTALL_DIR="/usr/local/bin"

# Detect OS and arch
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
[[ "$ARCH" == "x86_64" ]] && ARCH="amd64"
[[ "$ARCH" == "aarch64" || "$ARCH" == "arm64" ]] && ARCH="arm64"
[[ "$OS" == "darwin" ]] && OS="macos"

BINARY="tarish_${OS}_${ARCH}"
URL="${BASE_URL}/dist/${BINARY}"

echo "Installing tarish (${OS}/${ARCH})..."
curl -fsSL "$URL" -o /tmp/tarish
chmod +x /tmp/tarish
mv /tmp/tarish "${INSTALL_DIR}/tarish"

echo ""
echo "Installed tarish to ${INSTALL_DIR}/tarish"
echo ""
echo "Run: sudo tarish install"
