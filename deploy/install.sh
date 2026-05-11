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
# SD-2 (security): credentials previously hardcoded in this script were a
# real source-control credential leak — anyone with repo access could
# intercept CRITICAL/WARN alerts or send fake "STOP THE PROTOCOL" /
# "PROMOTION READY" messages. Now: TELEGRAM_BOT_TOKEN + TELEGRAM_CHAT_ID
# are REQUIRED env vars at install time. The bootstrapped values are
# never written to source.
#
# Operator workflow on first install:
#   ssh root@<VPS> "TELEGRAM_BOT_TOKEN=... TELEGRAM_CHAT_ID=... bash -s" < ./deploy/install.sh
#
# On subsequent installs the existing $ENV_FILE is preserved (no env vars
# needed; this branch is skipped).
if [[ ! -f "$ENV_FILE" ]]; then
    if [[ -z "${TELEGRAM_BOT_TOKEN:-}" ]] || [[ -z "${TELEGRAM_CHAT_ID:-}" ]]; then
        echo "ERROR: $ENV_FILE does not exist and TELEGRAM_BOT_TOKEN / TELEGRAM_CHAT_ID env vars are not set." >&2
        echo "       Provide them on the install command:" >&2
        echo "         ssh root@<VPS> \"TELEGRAM_BOT_TOKEN=... TELEGRAM_CHAT_ID=... bash -s\" < ./deploy/install.sh" >&2
        echo "       Or set them in your shell before running install.sh." >&2
        exit 1
    fi
    cat > "$ENV_FILE" <<EOF
TELEGRAM_BOT_TOKEN=${TELEGRAM_BOT_TOKEN}
TELEGRAM_CHAT_ID=${TELEGRAM_CHAT_ID}
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

# SD-3: previously hardcoded a 12-engine list that drifted from CLAUDE.md
# deployed-16 (single source of truth: configs/symbols.yaml). Fresh-VPS
# install would enable the wrong set; 4 engines would be missing on first
# boot. Now: read from configs/symbols.yaml via the shared helper, same
# pattern as scripts/post_deploy_check.sh + scripts/forward_paper_status.sh.
# shellcheck source=../scripts/lib/symbols.sh
source "${ROOT}/scripts/lib/symbols.sh"
DEPLOYED_UNITS=$(get_symbols deployed lower | sed 's/[^ ]*/paper-live@&/g')
# shellcheck disable=SC2086
# DEPLOYED_UNITS is intentionally word-split so each unit becomes a separate
# argument to systemctl enable — quoting would pass the whole string as one.
systemctl enable \
    $DEPLOYED_UNITS \
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
