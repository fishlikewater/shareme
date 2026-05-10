#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
BACKEND_DIR="${REPO_ROOT}/backend"
GO_CACHE_DIR="${REPO_ROOT}/.cache/go-build"
PLATFORM="${1:-linux/amd64}"
WAILS_BUILD_TAGS="${WAILS_BUILD_TAGS:-}"

cd "${BACKEND_DIR}"
WAILS_ARGS=(build -clean -platform "${PLATFORM}")
if [[ -n "${WAILS_BUILD_TAGS}" ]]; then
  WAILS_ARGS+=(-tags "${WAILS_BUILD_TAGS}")
fi

GOCACHE="${GO_CACHE_DIR}" GOTELEMETRY=off go run github.com/wailsapp/wails/v2/cmd/wails@v2.12.0 "${WAILS_ARGS[@]}"
