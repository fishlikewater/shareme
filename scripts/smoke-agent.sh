#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
BINARY_PATH="${REPO_ROOT}/dist/message-share-agent"
SKIP_BUILD="${1:-}"

if [[ "${SKIP_BUILD}" != "--skip-build" ]]; then
  "${SCRIPT_DIR}/build-agent.sh"
fi

if [[ ! -f "${BINARY_PATH}" ]]; then
  echo "Agent binary not found: ${BINARY_PATH}" >&2
  exit 1
fi

SMOKE_ROOT="${REPO_ROOT}/.tmp/smoke-agent"
TIMESTAMP="$(date +%Y%m%d%H%M%S)"
RUN_DIR="${SMOKE_ROOT}/run-${TIMESTAMP}"
RUNTIME_DIR="${RUN_DIR}/runtime"
HOME_DIR="${RUN_DIR}/home"
BASE_PORT="$((52000 + RANDOM % 5000))"
AGENT_TCP_PORT="${BASE_PORT}"
ACCELERATED_DATA_PORT="$((BASE_PORT + 1))"
DISCOVERY_UDP_PORT="$((BASE_PORT + 2))"
LOCAL_HTTP_PORT="$((BASE_PORT + 3))"
BOOTSTRAP_URL="http://127.0.0.1:${LOCAL_HTTP_PORT}/api/bootstrap"
ROOT_URL="http://127.0.0.1:${LOCAL_HTTP_PORT}/"

mkdir -p "${RUNTIME_DIR}" "${HOME_DIR}"

cleanup() {
  if [[ -n "${AGENT_PID:-}" ]] && kill -0 "${AGENT_PID}" >/dev/null 2>&1; then
    kill "${AGENT_PID}" >/dev/null 2>&1 || true
    wait "${AGENT_PID}" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

USERPROFILE="${HOME_DIR}" \
HOME="${HOME_DIR}" \
MESSAGE_SHARE_DATA_DIR="${RUNTIME_DIR}" \
MESSAGE_SHARE_AGENT_TCP_PORT="${AGENT_TCP_PORT}" \
MESSAGE_SHARE_LOCAL_HTTP_PORT="${LOCAL_HTTP_PORT}" \
MESSAGE_SHARE_ACCELERATED_DATA_PORT="${ACCELERATED_DATA_PORT}" \
MESSAGE_SHARE_DISCOVERY_UDP_PORT="${DISCOVERY_UDP_PORT}" \
MESSAGE_SHARE_DISCOVERY_LISTEN_ADDR="127.0.0.1:${DISCOVERY_UDP_PORT}" \
MESSAGE_SHARE_DISCOVERY_BROADCAST_ADDR="127.0.0.1:${DISCOVERY_UDP_PORT}" \
"${BINARY_PATH}" >"${RUN_DIR}/agent.log" 2>&1 &
AGENT_PID=$!

CONFIG_PATH="${RUNTIME_DIR}/config.json"
DEADLINE=$((SECONDS + 12))
while (( SECONDS < DEADLINE )); do
  if [[ -f "${CONFIG_PATH}" ]] && curl -fsS "${BOOTSTRAP_URL}" >/dev/null 2>&1 && curl -fsS "${ROOT_URL}" >/dev/null 2>&1; then
    sleep 1
    if kill -0 "${AGENT_PID}" >/dev/null 2>&1; then
      echo "Agent smoke passed: localhost Web UI and bootstrap API are reachable."
      exit 0
    fi
    echo "Agent exited during stability window." >&2
    exit 1
  fi

  if ! kill -0 "${AGENT_PID}" >/dev/null 2>&1; then
    echo "Agent exited before smoke completed." >&2
    cat "${RUN_DIR}/agent.log" >&2 || true
    exit 1
  fi

  sleep 0.25
done

echo "Agent smoke timed out." >&2
cat "${RUN_DIR}/agent.log" >&2 || true
exit 1
