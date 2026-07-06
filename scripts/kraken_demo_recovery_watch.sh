#!/usr/bin/env bash
# Polls the Kraken demo-futures public REST gateway (down since 2026-07-04)
# and sends a ONE-SHOT Telegram when it recovers, then disarms via marker
# file. Cron-installed on the VPS every 15 min; safe to run anywhere.
#
#   */15 * * * * /opt/trading-engine/scripts/kraken_demo_recovery_watch.sh
#
# Recovery means the PUBLIC instruments endpoint answers 200 — auth is NOT
# tested here; the Telegram tells the operator to run
# scripts/kraken_demo_smoke.py for the authenticated SMOKE PASS.
#
# DRY_RUN=1 prints instead of sending. Delete the marker to re-arm.
set -u

DEMO_URL="${KRAKEN_WATCH_URL:-https://demo-futures.kraken.com/derivatives/api/v3/instruments}"
MARKER="${KRAKEN_WATCH_MARKER:-/var/log/paper-live/kraken_demo_recovered.marker}"
LOG="${KRAKEN_WATCH_LOG:-/var/log/paper-live/kraken_demo_watch.log}"
ENV_FILE="/etc/paper-live/env"

[[ -f "$MARKER" ]] && exit 0

if [[ -f "$ENV_FILE" ]]; then
    # shellcheck source=/dev/null
    source "$ENV_FILE"
fi
BOT_TOKEN="${TELEGRAM_BOT_TOKEN:-}"
CHAT_ID="${TELEGRAM_CHAT_ID:-}"

code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 10 "$DEMO_URL" || echo "curl-fail")
ts=$(date -u +%Y-%m-%dT%H:%M:%SZ)
echo "$ts demo-instruments=$code" >> "$LOG"

[[ "$code" != "200" ]] && exit 0

msg="Kraken demo-futures REST gateway RECOVERED (public instruments 200 at $ts). Run: python3 scripts/kraken_demo_smoke.py — expect SMOKE PASS, then tick venue checklist step 7/8."

if [[ "${DRY_RUN:-0}" == "1" ]]; then
    echo "DRY_RUN: $msg"
    exit 0
fi

if [[ -n "$BOT_TOKEN" && -n "$CHAT_ID" ]]; then
    # Plain text on purpose — parse_mode=Markdown 400s silently ate a
    # CRITICAL gate-block alert on 2026-06-10 (see docs/findings/2026-06-11.md).
    if curl -sS -X POST "https://api.telegram.org/bot${BOT_TOKEN}/sendMessage" \
        --data-urlencode "chat_id=${CHAT_ID}" \
        --data-urlencode "text=${msg}" > /dev/null; then
        touch "$MARKER"
        echo "$ts RECOVERED — telegram sent, watch disarmed" >> "$LOG"
    else
        echo "$ts RECOVERED but telegram send FAILED — will retry next cron run" >> "$LOG"
    fi
else
    # No credentials (local run): report loudly, do NOT disarm.
    echo "$ts RECOVERED but no TELEGRAM env — not disarming" >> "$LOG"
    echo "RECOVERED: $msg"
fi
