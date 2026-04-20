#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
BACKEND_DIR="${REPO_ROOT}/backend"
FRONTEND_DIR="${REPO_ROOT}/frontend"
AGENT_FRONTEND_DIR="${BACKEND_DIR}/cmd/message-share-agent/frontend"
AGENT_FRONTEND_DIST_DIR="${AGENT_FRONTEND_DIR}/dist"
SOURCE_FRONTEND_DIST_DIR="${BACKEND_DIR}/frontend/dist"
GO_CACHE_DIR="${REPO_ROOT}/.cache/go-build"
NPM_CACHE_DIR="${REPO_ROOT}/.cache/npm"
OUTPUT="${1:-${REPO_ROOT}/dist/message-share-agent}"

cd "${FRONTEND_DIR}"
if [[ ! -d "${FRONTEND_DIR}/node_modules" ]]; then
  npm_config_cache="${NPM_CACHE_DIR}" npm ci
fi
npm_config_cache="${NPM_CACHE_DIR}" npm run build

rm -rf "${AGENT_FRONTEND_DIST_DIR}"
mkdir -p "${AGENT_FRONTEND_DIST_DIR}"
cp -R "${SOURCE_FRONTEND_DIST_DIR}/." "${AGENT_FRONTEND_DIST_DIR}/"

mkdir -p "$(dirname "${OUTPUT}")"
cd "${BACKEND_DIR}"
GOCACHE="${GO_CACHE_DIR}" GOTELEMETRY=off go build -o "${OUTPUT}" ./cmd/message-share-agent
