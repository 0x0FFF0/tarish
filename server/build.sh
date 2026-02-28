#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
WEB_DIR="$SCRIPT_DIR/../web"

echo "=== Building tarish-server ==="

# Build frontend
echo "Building frontend..."
cd "$WEB_DIR"
npm run build

# Copy dist into server for go:embed
echo "Copying frontend build..."
rm -rf "$SCRIPT_DIR/web/dist"
mkdir -p "$SCRIPT_DIR/web"
cp -r "$WEB_DIR/dist" "$SCRIPT_DIR/web/"

# Build Go binary
echo "Building server binary..."
cd "$SCRIPT_DIR"
go build -o tarish-server .

echo ""
echo "Build complete: $SCRIPT_DIR/tarish-server"
echo ""
echo "Run with:"
echo "  ./tarish-server --proxy-url http://127.0.0.1:8080 --proxy-api-token <token> --agent-key <key>"
