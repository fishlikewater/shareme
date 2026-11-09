#!/usr/bin/env bash
set -euo pipefail

OUTPUT="${1:-}"
GOOS="${GOOS:-$(go env GOOS)}"
GOARCH="${GOARCH:-$(go env GOARCH)}"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

if [ -z "$OUTPUT" ]; then
    ext=""
    [ "$GOOS" = "windows" ] && ext=".exe"
    OUTPUT="$PROJECT_DIR/dist/shareme-agent${ext}"
fi

echo "=== Building shareme-agent for $GOOS/$GOARCH -> $OUTPUT ==="

cd "$PROJECT_DIR/backend"

export GOOS="$GOOS"
export GOARCH="$GOARCH"
export GOTELEMETRY=off
export CGO_ENABLED=0

go build -o "$OUTPUT" ./cmd/shareme-agent

