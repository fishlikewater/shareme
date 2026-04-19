#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
BACKEND_DIR="${REPO_ROOT}/backend"
SMOKE_ROOT="${REPO_ROOT}/.tmp/smoke-desktop"
PLATFORM="${1:-}"
TIMEOUT_SECONDS="${SMOKE_TIMEOUT_SECONDS:-12}"
STABILITY_MILLISECONDS="${SMOKE_STABILITY_MILLISECONDS:-1000}"

detect_binary() {
  case "$(uname -s)" in
    Darwin)
      echo "${BACKEND_DIR}/build/bin/message-share.app/Contents/MacOS/message-share"
      ;;
    *)
      echo "${BACKEND_DIR}/build/bin/message-share"
      ;;
  esac
}

if [[ -z "${PLATFORM}" ]]; then
  case "$(uname -s)" in
    Darwin)
      PLATFORM="darwin/universal"
      ;;
    *)
      PLATFORM="linux/amd64"
      ;;
  esac
fi

if [[ ! -x "$(detect_binary)" ]]; then
  "${SCRIPT_DIR}/build-desktop.sh" "${PLATFORM}"
fi

RUN_DIR="${SMOKE_ROOT}/run-$(date +%Y%m%d%H%M%S)"
RUNTIME_DIR="${RUN_DIR}/runtime"
HOME_DIR="${RUN_DIR}/home"
UI_READY_MARKER="${RUN_DIR}/ui-ready.marker"
BASE_PORT="$((52000 + (RANDOM % 5000)))"
AGENT_TCP_PORT="${BASE_PORT}"
ACCELERATED_DATA_PORT="$((BASE_PORT + 1))"
DISCOVERY_UDP_PORT="$((BASE_PORT + 2))"
mkdir -p "${RUNTIME_DIR}" "${HOME_DIR}"

BINARY_PATH="$(detect_binary)"
(cd "${RUN_DIR}" && HOME="${HOME_DIR}" MESSAGE_SHARE_DATA_DIR="${RUNTIME_DIR}" MESSAGE_SHARE_UI_READY_MARKER="${UI_READY_MARKER}" MESSAGE_SHARE_AGENT_TCP_PORT="${AGENT_TCP_PORT}" MESSAGE_SHARE_ACCELERATED_DATA_PORT="${ACCELERATED_DATA_PORT}" MESSAGE_SHARE_DISCOVERY_UDP_PORT="${DISCOVERY_UDP_PORT}" "${BINARY_PATH}") &
APP_PID=$!
APP_EXIT=""

cleanup() {
  if kill -0 "${APP_PID}" >/dev/null 2>&1; then
    kill "${APP_PID}" >/dev/null 2>&1 || true
    wait "${APP_PID}" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

RUNTIME_READY=0
UI_READY=0
for ((tick=0; tick<TIMEOUT_SECONDS*4; tick++)); do
  if [[ -f "${RUNTIME_DIR}/config.json" ]]; then
    RUNTIME_READY=1
  fi
  if [[ -f "${UI_READY_MARKER}" ]]; then
    UI_READY=1
  fi
  if [[ "${RUNTIME_READY}" -eq 1 && "${UI_READY}" -eq 1 ]]; then
    break
  fi

  if ! kill -0 "${APP_PID}" >/dev/null 2>&1; then
    if wait "${APP_PID}"; then
      APP_EXIT=0
    else
      APP_EXIT=$?
    fi
    break
  fi

  sleep 0.25
done

if [[ "${RUNTIME_READY}" -eq 0 && -f "${RUNTIME_DIR}/config.json" ]]; then
  RUNTIME_READY=1
fi
if [[ "${UI_READY}" -eq 0 && -f "${UI_READY_MARKER}" ]]; then
  UI_READY=1
fi

if [[ "${RUNTIME_READY}" -eq 0 ]]; then
  if [[ -n "${APP_EXIT}" ]]; then
    echo "Desktop app exited before runtime dir initialization, exit code ${APP_EXIT}" >&2
    exit 1
  fi
  echo "Desktop app did not initialize runtime dir within ${TIMEOUT_SECONDS}s: ${RUNTIME_DIR}/config.json" >&2
  exit 1
fi
if [[ "${UI_READY}" -eq 0 ]]; then
  if [[ -n "${APP_EXIT}" ]]; then
    echo "Desktop app exited before main UI reported ready, exit code ${APP_EXIT}" >&2
    exit 1
  fi
  echo "Desktop app did not report main UI ready within ${TIMEOUT_SECONDS}s: ${UI_READY_MARKER}" >&2
  exit 1
fi

AGENT_MARKER_PORT="$(grep '^agentTcpPort=' "${UI_READY_MARKER}" | cut -d= -f2-)"
ACCELERATED_MARKER_PORT="$(grep '^acceleratedDataPort=' "${UI_READY_MARKER}" | cut -d= -f2-)"
DISCOVERY_MARKER_PORT="$(grep '^discoveryUdpPort=' "${UI_READY_MARKER}" | cut -d= -f2-)"

if [[ "${AGENT_MARKER_PORT}" != "${AGENT_TCP_PORT}" ]]; then
  echo "Desktop app did not honor smoke agent port override: expected ${AGENT_TCP_PORT}, got ${AGENT_MARKER_PORT}" >&2
  exit 1
fi
if [[ "${ACCELERATED_MARKER_PORT}" != "${ACCELERATED_DATA_PORT}" ]]; then
  echo "Desktop app did not honor smoke accelerated port override: expected ${ACCELERATED_DATA_PORT}, got ${ACCELERATED_MARKER_PORT}" >&2
  exit 1
fi
if [[ "${DISCOVERY_MARKER_PORT}" != "${DISCOVERY_UDP_PORT}" ]]; then
  echo "Desktop app did not honor smoke discovery port override: expected ${DISCOVERY_UDP_PORT}, got ${DISCOVERY_MARKER_PORT}" >&2
  exit 1
fi

sleep "$(awk "BEGIN { printf \"%.3f\", ${STABILITY_MILLISECONDS} / 1000 }")"
if ! kill -0 "${APP_PID}" >/dev/null 2>&1; then
  if wait "${APP_PID}"; then
    APP_EXIT=0
  else
    APP_EXIT=$?
  fi
  echo "Desktop app exited during post-ready stability window, exit code ${APP_EXIT}" >&2
  exit 1
fi

if [[ -n "${APP_EXIT}" ]]; then
  if [[ "${APP_EXIT}" -ne 0 ]]; then
    echo "Desktop app exited with failure during smoke test, exit code ${APP_EXIT}" >&2
    exit 1
  fi
  echo "Desktop smoke passed: runtime dir initialized and main UI reported ready before app exited cleanly."
else
  echo "Desktop smoke passed: app started, runtime dir initialized, and main UI reported ready."
fi
