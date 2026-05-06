#!/usr/bin/env bash
# Runs daily at 23:00 UTC via paper-live-digest.timer.
# Reads logs/journal/*.jsonl, computes last-24h per-symbol stats, sends a Telegram message.
# Requires jq.
set -euo pipefail

ENV_FILE="/etc/paper-live/env"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
JOURNAL_DIR="${PAPER_LIVE_JOURNAL_DIR:-${ROOT}/logs/journal}"

# ── Load credentials ──────────────────────────────────────────────────────────
if [[ -f "$ENV_FILE" ]]; then
    # shellcheck source=/dev/null
    source "$ENV_FILE"
fi
BOT_TOKEN="${TELEGRAM_BOT_TOKEN:-}"
CHAT_ID="${TELEGRAM_CHAT_ID:-}"

if [[ -z "$BOT_TOKEN" || -z "$CHAT_ID" ]]; then
    echo "TELEGRAM_BOT_TOKEN / TELEGRAM_CHAT_ID not set — digest suppressed" >&2
    exit 0
fi

if ! command -v jq &>/dev/null; then
    echo "jq not found — install with: apt install jq" >&2
    exit 1
fi

# ── Time window: last 24h ─────────────────────────────────────────────────────
NOW_UTC=$(date -u +%s)
WINDOW_START=$(( NOW_UTC - 86400 ))
TODAY=$(date -u +%Y-%m-%d)

# ── Per-symbol stats ──────────────────────────────────────────────────────────
total_trades=0
total_wins=0
total_pnl_cents=0  # track in cents to avoid float issues

lines=""

shopt -s nullglob
for journal in "${JOURNAL_DIR}"/*.jsonl; do
    symbol=$(basename "$journal" | sed 's/-[0-9]*-[0-9]*.jsonl//')

    # Extract close events in the last 24h; compute: trade count, wins, total pnl_usd.
    stats=$(jq -r --argjson since "$WINDOW_START" '
        select(.event == "close") |
        select(
            (.ts // .timestamp // "") |
            if . != "" then (strptime("%Y-%m-%dT%H:%M:%SZ") | mktime) >= $since else false end
        ) |
        [
            if .outcome == "TARGET" then 1 else 0 end,
            (.pnl_usd // 0)
        ] | @csv
    ' "$journal" 2>/dev/null || true)

    sym_trades=0
    sym_wins=0
    sym_pnl=0

    if [[ -n "$stats" ]]; then
        while IFS=, read -r win pnl; do
            sym_trades=$(( sym_trades + 1 ))
            sym_wins=$(( sym_wins + win ))
            # Use awk for float addition
            sym_pnl=$(awk "BEGIN{printf \"%.2f\", $sym_pnl + $pnl}")
        done <<< "$stats"
    fi

    total_trades=$(( total_trades + sym_trades ))
    total_wins=$(( total_wins + sym_wins ))
    total_pnl=$(awk "BEGIN{printf \"%.2f\", ${total_pnl_cents:-0} + $sym_pnl}" || echo "$sym_pnl")
    total_pnl_cents="$total_pnl"

    if (( sym_trades > 0 )); then
        win_pct=$(( sym_wins * 100 / sym_trades ))
        pnl_fmt=$(awk "BEGIN{printf \"%+.0f\", $sym_pnl}")
        lines="${lines}$(printf '%-10s %2d trades  %3d%%  \$%s\n' "$symbol" "$sym_trades" "$win_pct" "$pnl_fmt")\n"
    else
        lines="${lines}$(printf '%-10s  0 trades    —    —\n' "$symbol")\n"
    fi
done

# ── WS gap summary from logs ──────────────────────────────────────────────────
LOG_DIR="/var/log/paper-live"
gap_count=0
gap_avg_secs=0
if [[ -d "$LOG_DIR" ]]; then
    gap_data=$(grep -h '"ws gap closed"' "${LOG_DIR}"/*.log 2>/dev/null \
        | jq -r '.duration_s // empty' 2>/dev/null \
        | awk '{n++; s+=$1} END{if(n>0) print n, s/n; else print 0, 0}' || echo "0 0")
    gap_count=$(echo "$gap_data" | awk '{print $1}')
    gap_avg_secs=$(echo "$gap_data" | awk '{printf "%.0f", $2}')
fi

# ── Build message ─────────────────────────────────────────────────────────────
if (( total_trades > 0 )); then
    overall_win_pct=$(( total_wins * 100 / total_trades ))
else
    overall_win_pct=0
fi
total_pnl_fmt=$(awk "BEGIN{printf \"%+.0f\", ${total_pnl_cents:-0}}")

msg="📊 *Daily digest — ${TODAY}*
\`\`\`
$(printf '%b' "$lines")
TOTAL      ${total_trades} trades  ${overall_win_pct}%  \$${total_pnl_fmt}
WS gaps:   ${gap_count} gaps (avg ${gap_avg_secs}s)
\`\`\`"

# ── Send ──────────────────────────────────────────────────────────────────────
curl -sS -X POST \
    "https://api.telegram.org/bot${BOT_TOKEN}/sendMessage" \
    -H "Content-Type: application/json" \
    -d "$(jq -n \
        --arg chat_id "$CHAT_ID" \
        --arg text "$msg" \
        '{chat_id: $chat_id, text: $text, parse_mode: "Markdown"}')" \
    > /dev/null
