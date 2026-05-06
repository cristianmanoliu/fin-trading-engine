#!/usr/bin/env bash
set -euo pipefail

# Stop all paper-live engine processes gracefully (SIGTERM, then SIGKILL after 10s).
#
# Usage:
#   ./scripts/paper_live_stop.sh

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PID_DIR="${ROOT}/logs/pids"

if [[ ! -d "${PID_DIR}" ]]; then
  echo "No PID directory found at ${PID_DIR}. Nothing to stop."
  exit 0
fi

shopt -s nullglob
pid_files=("${PID_DIR}"/*.pid)
if [[ ${#pid_files[@]} -eq 0 ]]; then
  echo "No PID files found. Nothing to stop."
  exit 0
fi

for pid_file in "${pid_files[@]}"; do
  symbol=$(basename "${pid_file}" .pid)
  pid=$(cat "${pid_file}")

  if ! kill -0 "${pid}" 2>/dev/null; then
    echo "  DEAD   ${symbol} (pid ${pid}, removing stale PID file)"
    rm -f "${pid_file}"
    continue
  fi

  echo -n "  STOP   ${symbol} (pid ${pid})..."
  kill -TERM "${pid}"

  # Wait up to 10s for graceful shutdown.
  for i in $(seq 1 10); do
    if ! kill -0 "${pid}" 2>/dev/null; then
      break
    fi
    sleep 1
  done

  if kill -0 "${pid}" 2>/dev/null; then
    echo " (SIGKILL)"
    kill -KILL "${pid}" 2>/dev/null || true
  else
    echo " done"
  fi

  rm -f "${pid_file}"
done
