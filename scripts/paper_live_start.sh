#!/usr/bin/env bash
set -euo pipefail

# Start paper-live engines for all 8 symbols.
# Builds the binary once, then launches one background process per symbol.
# Logs go to logs/{SYMBOL}.log; PIDs go to logs/pids/{SYMBOL}.pid.
# Trade journals go to logs/journal/{SYMBOL}-YYYY-MM.jsonl (via PAPER_LIVE_JOURNAL_DIR env).
#
# Usage:
#   ./scripts/paper_live_start.sh ["BTCUSDT ETHUSDT ..."]

SYMBOLS="${1:-BTCUSDT ETHUSDT BNBUSDT SOLUSDT XRPUSDT LINKUSDT LTCUSDT DOGEUSDT}"

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="${ROOT}/bin/engine"
LOG_DIR="${ROOT}/logs"
PID_DIR="${LOG_DIR}/pids"
JOURNAL_DIR="${LOG_DIR}/journal"

mkdir -p "${LOG_DIR}" "${PID_DIR}" "${JOURNAL_DIR}"

echo "→ Building engine binary..."
(cd "${ROOT}" && go build -o "${BIN}" ./cmd/engine)
echo "  Built: ${BIN}"

for symbol in ${SYMBOLS}; do
  cfg_lower=$(echo "${symbol}" | tr '[:upper:]' '[:lower:]')
  cfg="${ROOT}/configs/${cfg_lower}.yaml"
  log="${LOG_DIR}/${symbol}.log"
  pid_file="${PID_DIR}/${symbol}.pid"

  if [[ -f "${pid_file}" ]]; then
    existing_pid=$(cat "${pid_file}")
    if kill -0 "${existing_pid}" 2>/dev/null; then
      echo "  SKIP ${symbol}: already running (pid ${existing_pid})"
      continue
    fi
    rm -f "${pid_file}"
  fi

  if [[ ! -f "${cfg}" ]]; then
    echo "  WARN ${symbol}: config not found at ${cfg}, skipping"
    continue
  fi

  PAPER_LIVE_JOURNAL_DIR="${JOURNAL_DIR}" \
    nohup "${BIN}" --config "${cfg}" >> "${log}" 2>&1 &
  echo $! > "${pid_file}"
  echo "  START ${symbol}: pid $(cat "${pid_file}"), log ${log}"
done

echo ""
echo "All done. Run './scripts/paper_live_status.sh' to check liveness."
