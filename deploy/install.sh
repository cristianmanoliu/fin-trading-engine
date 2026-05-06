#!/usr/bin/env bash
# Run on the VPS after sync.sh. Installs systemd units, logrotate, and creates
# required directories. Prompts for Telegram credentials if not already set.
set -euo pipefail

if [[ "$(uname)" != "Linux" ]]; then
    echo "Error: run this on the VPS, not locally." >&2
    echo "  ssh root@<VPS_IP> 'bash -s' < ./deploy/install.sh" >&2
    exit 1
fi

ROOT="/opt/trading-engine"
ENV_FILE="/etc/paper-live/env"

# ── Directories ───────────────────────────────────────────────────────────────
mkdir -p /var/log/paper-live/journal /var/lib/paper-live /etc/paper-live
chown -R paperlive:paperlive /var/log/paper-live /var/lib/paper-live

# ── Telegram credentials ──────────────────────────────────────────────────────
if [[ ! -f "$ENV_FILE" ]]; then
    cat > "$ENV_FILE" <<'EOF'
TELEGRAM_BOT_TOKEN=8549573378:AAFMPhyi9v1Yfk0fdXSx0SGqopjCAeOZST4
TELEGRAM_CHAT_ID=6462142964
EOF
    chmod 600 "$ENV_FILE"
    chown paperlive:paperlive "$ENV_FILE"
    echo "→ Credentials saved to ${ENV_FILE}"
else
    echo "→ ${ENV_FILE} already exists — skipping"
fi

# ── systemd units ─────────────────────────────────────────────────────────────
cp "${ROOT}/deploy/systemd/"* /etc/systemd/system/
chmod 644 /etc/systemd/system/paper-live*
systemctl daemon-reload

systemctl enable \
    paper-live@ethusdt \
    paper-live@runeusdt \
    paper-live@linkusdt \
    paper-live@xlmusdt \
    paper-live@ldousdt \
    paper-live@solusdt \
    paper-live@apeusdt \
    paper-live@enjusdt \
    paper-live@opusdt \
    paper-live@zilusdt \
    paper-live@btcusdt \
    paper-live@tiausdt \
    paper-live.target \
    paper-live-watchdog.timer \
    paper-live-digest.timer

# ── logrotate ─────────────────────────────────────────────────────────────────
cp "${ROOT}/deploy/logrotate.d/paper-live" /etc/logrotate.d/paper-live

# ── script permissions ────────────────────────────────────────────────────────
chmod +x "${ROOT}/scripts/"*.sh

echo ""
echo "✓ Install complete."
echo ""
echo "Pre-flight: start BTC only first:"
echo "  systemctl start paper-live@btcusdt"
echo "  journalctl -u paper-live@btcusdt -f"
