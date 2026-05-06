#!/usr/bin/env bash
# Runs every 10 minutes via paper-live-watchdog.timer.
# Alerts on: dead process, stale heartbeat (>5m), zero heartbeats after 5min running.
# Anti-spam: suppresses duplicate alerts for the same condition for 1 hour.
# Always exits 0 — a watchdog failure must not itself cause noise.
set -uo pipefail

ENV_FILE="/etc/paper-live/env"
STATE_FILE="/var/lib/paper-live/alerted.state"
LOG_DIR="/var/log/paper-live"
# Deployed set last updated 2026-05-06: 16 train-only top-K on Strategy A
# (P4-Combined with --max-hold-hours 336). Selected via scripts/select_16_engines.py:
# deployed-32 → robustness gate (train_NET > 0 at slip=25bp drops ETH/LINK/VET/LDO)
# → top-16 by train_NET at slip=15bp. See CLAUDE.md ## Current state for context.
SYMBOLS=(roseusdt mkrusdt grtusdt 1inchusdt adausdt kavausdt 1000shibusdt ensusdt xlmusdt imxusdt etcusdt runeusdt avaxusdt ftmusdt dotusdt filusdt)

# ── Load credentials ──────────────────────────────────────────────────────────
if [[ -f "$ENV_FILE" ]]; then
    # shellcheck source=/dev/null
    source "$ENV_FILE"
fi
BOT_TOKEN="${TELEGRAM_BOT_TOKEN:-}"
CHAT_ID="${TELEGRAM_CHAT_ID:-}"

send_alert() {
    local msg="$1"
    if [[ -z "$BOT_TOKEN" || -z "$CHAT_ID" ]]; then
        return
    fi
    curl -sS -X POST \
        "https://api.telegram.org/bot${BOT_TOKEN}/sendMessage" \
        -H "Content-Type: application/json" \
        -d "{\"chat_id\":\"${CHAT_ID}\",\"text\":\"${msg}\",\"parse_mode\":\"Markdown\"}" \
        > /dev/null 2>&1 || true
}

# ── Anti-spam: suppress same condition for 1 hour ─────────────────────────────
should_alert() {
    local key="$1"
    local now
    now=$(date +%s)
    mkdir -p "$(dirname "$STATE_FILE")"
    touch "$STATE_FILE"
    local last
    last=$(grep -F "${key}=" "$STATE_FILE" 2>/dev/null | tail -1 | cut -d= -f2 || echo 0)
    if (( now - last < 3600 )); then
        return 1
    fi
    grep -vF "${key}=" "$STATE_FILE" > "${STATE_FILE}.tmp" 2>/dev/null || true
    echo "${key}=${now}" >> "${STATE_FILE}.tmp"
    mv "${STATE_FILE}.tmp" "$STATE_FILE"
    return 0
}

# ── Check each symbol ─────────────────────────────────────────────────────────
for symbol in "${SYMBOLS[@]}"; do
    service="paper-live@${symbol}.service"
    log="${LOG_DIR}/${symbol}.log"

    # ── Dead process check via systemd ───────────────────────────────────────
    active=$(systemctl is-active "$service" 2>/dev/null || echo "inactive")
    if [[ "$active" != "active" ]]; then
        key="dead_${symbol}"
        if should_alert "$key"; then
            send_alert "🔴 *Watchdog*: \`${symbol^^}\` process is *${active^^}*"
        fi
        continue
    fi

    [[ -f "$log" ]] || continue

    # ── Stale heartbeat check ─────────────────────────────────────────────────
    last_hb_line=$(grep '"msg":"heartbeat' "$log" 2>/dev/null | tail -1 || true)
    if [[ -n "$last_hb_line" ]]; then
        # last_tick_age is in nanoseconds in the JSON
        age_ns=$(echo "$last_hb_line" | grep -o '"last_tick_age":[0-9]*' | cut -d: -f2 || echo 0)
        age_secs=$(( age_ns / 1000000000 ))
        if (( age_secs > 300 )); then
            age_min=$(( age_secs / 60 ))
            key="stale_${symbol}"
            if should_alert "$key"; then
                send_alert "⚠️ *Watchdog*: \`${symbol^^}\` last tick *${age_min}m* ago (threshold: 5m)"
            fi
        fi
    fi

    # ── Zero heartbeats after 5 min running ──────────────────────────────────
    hb_count=$(grep -c '"heartbeat"' "$log" 2>/dev/null || echo 0)
    if [[ "$hb_count" == "0" ]]; then
        start_ts=$(systemctl show "$service" --property=ActiveEnterTimestamp --value 2>/dev/null || echo "")
        if [[ -n "$start_ts" ]]; then
            start_epoch=$(date -d "$start_ts" +%s 2>/dev/null || echo 0)
            now=$(date +%s)
            running_secs=$(( now - start_epoch ))
            if (( running_secs > 300 )); then
                key="nohb_${symbol}"
                if should_alert "$key"; then
                    send_alert "⚠️ *Watchdog*: \`${symbol^^}\` 0 heartbeats after ${running_secs}s running"
                fi
            fi
        fi
    fi
done

exit 0
