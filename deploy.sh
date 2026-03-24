#!/bin/bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEFAULT_TARGET="nas:/vol1/1000/File/tarish"
TARGET="${TARISH_DEPLOY_TARGET:-$DEFAULT_TARGET}"
BUILD_FIRST=1
DRY_RUN=0

usage() {
    cat <<EOF
Usage: ./deploy.sh [options] [target]

Build release artifacts and sync the client-facing release files:
  - dist/
  - version
  - install.sh

Options:
  --no-build        Sync existing release artifacts without rebuilding
  --dry-run, -n     Show what would be uploaded without changing the target
  --target <path>   Override the rsync target
  -h, --help        Show this help

Environment:
  TARISH_DEPLOY_TARGET

Default target:
  ${DEFAULT_TARGET}
EOF
}

while [ $# -gt 0 ]; do
    case "$1" in
        --no-build)
            BUILD_FIRST=0
            ;;
        --dry-run|-n)
            DRY_RUN=1
            ;;
        --target)
            shift
            if [ $# -eq 0 ]; then
                echo "Error: --target requires a value" >&2
                exit 1
            fi
            TARGET="$1"
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        -*)
            echo "Error: unknown option '$1'" >&2
            usage >&2
            exit 1
            ;;
        *)
            TARGET="$1"
            ;;
    esac
    shift
done

TARGET="${TARGET%/}"

if [ "$BUILD_FIRST" -eq 1 ]; then
    "$SCRIPT_DIR/build.sh"
fi

if [ ! -d "$SCRIPT_DIR/dist" ]; then
    echo "Error: missing dist/ directory. Run ./build.sh first." >&2
    exit 1
fi

if [ ! -f "$SCRIPT_DIR/version" ]; then
    echo "Error: missing version file. Run ./build.sh first." >&2
    exit 1
fi

if [ ! -f "$SCRIPT_DIR/install.sh" ]; then
    echo "Error: missing install.sh" >&2
    exit 1
fi

VERSION="$(tr -d '\r\n' < "$SCRIPT_DIR/version")"
if [ -z "$VERSION" ]; then
    echo "Error: version file is empty" >&2
    exit 1
fi

if git -C "$SCRIPT_DIR" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    if ! git -C "$SCRIPT_DIR" diff --quiet --ignore-submodules HEAD -- 2>/dev/null; then
        echo "Warning: working tree has uncommitted changes; build version may include -dirty" >&2
    fi
fi

RSYNC_ARGS=(
    -avz
    --progress
    --human-readable
    --mkpath
)

if [ "$DRY_RUN" -eq 1 ]; then
    RSYNC_ARGS+=(
        --dry-run
        --itemize-changes
    )
fi

echo "Deploy target: ${TARGET}"
echo "Version: ${VERSION}"
if [[ "$VERSION" == *-dirty* ]]; then
    echo "Warning: version contains -dirty. Use ./release.sh --version vX.Y.Z for an official release." >&2
fi
echo ""
echo "Syncing dist/ ..."
rsync "${RSYNC_ARGS[@]}" \
    --delete \
    --exclude '.DS_Store' \
    "$SCRIPT_DIR/dist/" \
    "${TARGET}/dist/"

echo ""
echo "Syncing release metadata ..."
rsync "${RSYNC_ARGS[@]}" \
    "$SCRIPT_DIR/version" \
    "$SCRIPT_DIR/install.sh" \
    "${TARGET}/"

echo ""
if [ "$DRY_RUN" -eq 1 ]; then
    echo "Dry run complete."
else
    echo "Deploy complete."
fi
