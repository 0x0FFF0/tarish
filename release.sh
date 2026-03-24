#!/bin/bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEFAULT_TARGET="nas:/vol1/1000/File/tarish"
TARGET="${TARISH_DEPLOY_TARGET:-$DEFAULT_TARGET}"
RELEASE_VERSION=""

usage() {
    cat <<EOF
Usage: ./release.sh --version <vX.Y.Z> [options]

Create an official release build and deploy it.

Requirements:
  - git working tree must be clean
  - release version must be provided explicitly

Options:
  --version <value>   Release version to embed and publish
  --target <path>     Override the rsync target
  -h, --help          Show this help

Environment:
  TARISH_DEPLOY_TARGET

Default target:
  ${DEFAULT_TARGET}
EOF
}

validate_version() {
    local value="$1"

    if [[ ! "$value" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([-.][0-9A-Za-z.-]+)?$ ]]; then
        echo "Error: release version must look like v1.2.3 or v1.2.3-rc.1" >&2
        exit 1
    fi
}

ensure_clean_git() {
    if ! git -C "$SCRIPT_DIR" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
        echo "Error: official release requires a git worktree" >&2
        exit 1
    fi

    local status
    status="$(git -C "$SCRIPT_DIR" status --porcelain --untracked-files=normal)"
    if [ -n "$status" ]; then
        echo "Error: official release requires a clean git worktree" >&2
        echo "" >&2
        echo "$status" >&2
        echo "" >&2
        echo "Commit or stash changes before releasing." >&2
        exit 1
    fi
}

while [ $# -gt 0 ]; do
    case "$1" in
        --version)
            shift
            if [ $# -eq 0 ]; then
                echo "Error: --version requires a value" >&2
                exit 1
            fi
            RELEASE_VERSION="$1"
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
        *)
            echo "Error: unknown option '$1'" >&2
            usage >&2
            exit 1
            ;;
    esac
    shift
done

if [ -z "$RELEASE_VERSION" ]; then
    echo "Error: --version is required" >&2
    usage >&2
    exit 1
fi

validate_version "$RELEASE_VERSION"
ensure_clean_git

echo "Preparing official release ${RELEASE_VERSION}"
echo "Deploy target: ${TARGET}"
echo ""

VERSION="$RELEASE_VERSION" "$SCRIPT_DIR/build.sh"
"$SCRIPT_DIR/deploy.sh" --no-build --target "$TARGET"

