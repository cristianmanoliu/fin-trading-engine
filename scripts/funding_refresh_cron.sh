#!/usr/bin/env bash
# funding_refresh_cron.sh — weekly funding-CSV refresh for VPS cron.
#
# Incrementally downloads Binance funding rates for deployed symbols,
# then validates that no CSV is stale (>24h since last entry). Sends
# Telegram alerts on success/failure.
#
# Designed for VPS cron (Sunday 03:00 UTC). Engines load funding CSVs
# at startup; this script does NOT restart them. Fresh data is picked
# up on the next redeploy.
#
# Usage:
#   bash scripts/funding_refresh_cron.sh            # normal
#   DRY_RUN=1 bash scripts/funding_refresh_cron.sh  # print, don't send Telegram
#
# Exit codes:
#   0  REFRESH_OK     — all deployed symbols refreshed and fresh
#   1  REFRESH_FAIL   — download_funding.sh failed
#   2  REFRESH_STALE  — download succeeded but staleness persists (API gap?)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# shellcheck source=lib/notify.sh
source "${SCRIPT_DIR}/lib/notify.sh"
# shellcheck source=lib/symbols.sh
source "${SCRIPT_DIR}/lib/symbols.sh"

DEPLOYED=$(get_symbols deployed)
SYMBOL_COUNT=$(echo "$DEPLOYED" | wc -w | tr -d ' ')
FUNDING_DIR="${ROOT}/data/funding"
NOW_S=$(date +%s)
STALE_THRESHOLD_S=$((24 * 3600))

echo "── funding_refresh_cron: ${SYMBOL_COUNT} deployed symbols ──"

# ── Download ────────────────────────────────────────────────────────────────

if ! SYMBOLS="$DEPLOYED" INCREMENTAL=1 bash "${SCRIPT_DIR}/download_funding.sh" 2>&1; then
    notify_telegram CRITICAL "funding_refresh FAILED" \
        "INCREMENTAL download_funding.sh exited non-zero for deployed symbols.
Check /var/log/paper-live/funding_refresh.log for details.
Manual fix: bash scripts/download_funding.sh"
    exit 1
fi

# ── Post-refresh staleness validation ───────────────────────────────────────

stale_syms=()
oldest_age_h=0

for sym in $DEPLOYED; do
    csv="${FUNDING_DIR}/${sym}.csv"
    if [[ ! -f "$csv" ]]; then
        stale_syms+=("${sym}(missing)")
        continue
    fi

    last_ms=$(awk -F',' 'NR>1 && $1+0>0 {v=$1} END{print v+0}' "$csv")
    if [[ "$last_ms" -eq 0 ]]; then
        stale_syms+=("${sym}(empty)")
        continue
    fi

    last_s=$((last_ms / 1000))
    age_s=$((NOW_S - last_s))
    age_h=$((age_s / 3600))

    if [[ "$age_s" -gt "$STALE_THRESHOLD_S" ]]; then
        stale_syms+=("${sym}(${age_h}h)")
    fi

    if [[ "$age_h" -gt "$oldest_age_h" ]]; then
        oldest_age_h=$age_h
    fi
done

if [[ ${#stale_syms[@]} -gt 0 ]]; then
    stale_list=$(printf '%s ' "${stale_syms[@]}")
    echo "STALE after refresh: ${stale_list}"
    notify_telegram WARN "funding_refresh: ${#stale_syms[@]} symbols still stale" \
        "Staleness persists after incremental refresh (threshold: 24h).
Stale: ${stale_list}
Possible causes: Binance API rate-limit, symbol delisted, network issue.
Manual check: tail data/funding/{SYMBOL}.csv"
    exit 2
fi

# ── Success ─────────────────────────────────────────────────────────────────

echo ""
echo "════════════════════════════════════════════════════════════════════════"
echo "  funding_refresh OK — ${SYMBOL_COUNT} symbols fresh (oldest: ${oldest_age_h}h)"
echo "════════════════════════════════════════════════════════════════════════"

notify_telegram INFO "funding_refresh OK" \
    "${SYMBOL_COUNT} deployed symbols refreshed (oldest entry: ${oldest_age_h}h ago).
Engines pick up new data on next restart/redeploy."

exit 0
