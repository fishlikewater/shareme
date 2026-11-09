#!/usr/bin/env bash
set -euo pipefail

PLATFORM="${1:-linux/amd64}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

echo "=== Building shareme desktop for $PLATFORM ==="

cd "$PROJECT_DIR/backend"

go run github.com/wailsapp/wails/v2/cmd/wails@v2.12.0 build -platform "$PLATFORM"

echo "=== Build complete: backend/build/bin/ ==="

