#!/bin/bash

# Tarish Build Script
# Cross-compiles tarish for multiple platforms

set -e

# Configuration
VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo "dev")}"
BUILD_DIR="dist"
BINARY_NAME="tarish"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}Building tarish version ${VERSION}${NC}"
echo ""

# Create build directory
mkdir -p "${BUILD_DIR}"

# Build targets
TARGETS=(
    "darwin/arm64"   # macOS Apple Silicon
    "darwin/amd64"   # macOS Intel
    "linux/amd64"    # Linux x86_64
    "linux/arm64"    # Linux ARM64
)

# Build each target
for target in "${TARGETS[@]}"; do
    IFS='/' read -r GOOS GOARCH <<< "$target"
    
    # Determine output name
    OS_NAME="${GOOS}"
    if [ "${GOOS}" = "darwin" ]; then
        OS_NAME="macos"
    fi
    
    OUTPUT="${BUILD_DIR}/${BINARY_NAME}_${OS_NAME}_${GOARCH}"
    
    echo -e "${YELLOW}Building for ${GOOS}/${GOARCH}...${NC}"
    
    # Build with version embedded
    CGO_ENABLED=0 GOOS="${GOOS}" GOARCH="${GOARCH}" go build \
        -ldflags="-s -w -X main.Version=${VERSION}" \
        -o "${OUTPUT}" \
        .
    
    if [ $? -eq 0 ]; then
        # Get file size
        SIZE=$(ls -lh "${OUTPUT}" | awk '{print $5}')
        echo -e "${GREEN}  ✓ Built ${OUTPUT} (${SIZE})${NC}"
    else
        echo -e "${RED}  ✗ Failed to build for ${GOOS}/${GOARCH}${NC}"
        exit 1
    fi
done

echo ""
echo -e "${GREEN}Build complete!${NC}"
echo ""
echo "Binaries are in the ${BUILD_DIR}/ directory:"
ls -lh "${BUILD_DIR}/"

# Create local binary for current platform
CURRENT_OS=$(uname -s | tr '[:upper:]' '[:lower:]')
CURRENT_ARCH=$(uname -m)
if [ "$CURRENT_ARCH" = "x86_64" ]; then CURRENT_ARCH="amd64"; fi
if [ "$CURRENT_ARCH" = "aarch64" ] || [ "$CURRENT_ARCH" = "arm64" ]; then CURRENT_ARCH="arm64"; fi
if [ "$CURRENT_OS" = "darwin" ]; then CURRENT_OS="macos"; fi

LOCAL_BINARY="${BUILD_DIR}/${BINARY_NAME}_${CURRENT_OS}_${CURRENT_ARCH}"
if [ -f "$LOCAL_BINARY" ]; then
    cp "$LOCAL_BINARY" "${BINARY_NAME}"
    chmod +x "${BINARY_NAME}"
    echo ""
    echo -e "${GREEN}Created local binary: ./${BINARY_NAME}${NC}"
fi

# Create release archives if requested
if [ "$1" = "--release" ]; then
    echo ""
    echo -e "${YELLOW}Creating release archives...${NC}"
    
    for target in "${TARGETS[@]}"; do
        IFS='/' read -r GOOS GOARCH <<< "$target"
        
        OS_NAME="${GOOS}"
        if [ "${GOOS}" = "darwin" ]; then
            OS_NAME="macos"
        fi
        
        BINARY="${BUILD_DIR}/${BINARY_NAME}_${OS_NAME}_${GOARCH}"
        ARCHIVE="${BUILD_DIR}/${BINARY_NAME}_${VERSION}_${OS_NAME}_${GOARCH}.tar.gz"
        
        # Create temp directory for archive contents
        TEMP_DIR=$(mktemp -d)
        mkdir -p "${TEMP_DIR}/tarish"
        
        # Copy binary
        cp "${BINARY}" "${TEMP_DIR}/tarish/${BINARY_NAME}"
        
        # Copy bin/ directory if exists
        if [ -d "bin" ]; then
            cp -r bin "${TEMP_DIR}/tarish/"
        fi
        
        # Copy configs/ directory if exists
        if [ -d "configs" ]; then
            cp -r configs "${TEMP_DIR}/tarish/"
        fi
        
        # Create archive
        tar -czf "${ARCHIVE}" -C "${TEMP_DIR}" tarish
        
        # Clean up
        rm -rf "${TEMP_DIR}"
        
        echo -e "${GREEN}  ✓ Created ${ARCHIVE}${NC}"
    done
    
    echo ""
    echo -e "${GREEN}Release archives created!${NC}"
fi

