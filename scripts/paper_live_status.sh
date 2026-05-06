#!/usr/bin/env bash
set -euo pipefail

# Show liveness status of all paper-live engine processes.
# For each symbol: alive/dead, recent heartbeat count, last trade time, recent gap warnings.
#
# Usage:
#   ./scripts/paper_live_status.sh

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PID_DIR="${ROOT}/logs/pids"
LOG_DIR="${ROOT}/logs"

printf "%-12s  %-6s  %-8s  %-22s  %-22s  %s\n" \
  "SYMBOL" "STATUS" "HB/100" "LAST_TRADE" "LAST_TICK_AGE" "GAP_WARNINGS"
printf "%-12s  %-6s  %-8s  %-22s  %-22s  %s\n" \
  "------" "------" "------" "----------" "-------------" "------------"

shopt -s nullglob
pid_files=("${PID_DIR}"/*.pid)
if [[ ${#pid_files[@]} -eq 0 ]]; then
  echo "(no PID files found — run paper_live_start.sh first)"
  exit 0
fi

for pid_file in "${pid_files[@]}"; do
  symbol=$(basename "${pid_file}" .pid)
  pid=$(cat "${pid_file}")
  log="${LOG_DIR}/${symbol}.log"

  if kill -0 "${pid}" 2>/dev/null; then
    status="ALIVE"
  else
    status="DEAD"
  fi

  if [[ ! -f "${log}" ]]; then
    printf "%-12s  %-6s  (no log)\n" "${symbol}" "${status}"
    continue
  fi

  hb_count=$(tail -100 "${log}" | grep -c '"heartbeat"' || true)

  last_trade=$(grep -o '"position closed"' "${log}" 2>/dev/null | tail -1 || true)
  if [[ -n "${last_trade}" ]]; then
    last_trade_ts=$(grep '"position closed"' "${log}" | tail -1 | python3 -c "import sys,json; l=json.loads(sys.stdin.read().strip()); print(l.get('time','?'))" 2>/dev/null || echo "?")
  else
    last_trade_ts="(none)"
  fi

  # Extract last_tick_age from the most recent heartbeat line.
  last_hb=$(grep '"heartbeat"' "${log}" 2>/dev/null | tail -1 || true)
  if [[ -n "${last_hb}" ]]; then
    tick_age=$(echo "${last_hb}" | python3 -c "import sys,json; l=json.loads(sys.stdin.read().strip()); print(l.get('last_tick_age','?'))" 2>/dev/null || echo "?")
  else
    tick_age="(no heartbeat)"
  fi

  gap_count=$(grep -c '"ws gap closed"' "${log}" 2>/dev/null || true)

  printf "%-12s  %-6s  %-8s  %-22s  %-22s  %s\n" \
    "${symbol}" "${status}" "${hb_count}" "${last_trade_ts}" "${tick_age}" "${gap_count} gaps"
done
