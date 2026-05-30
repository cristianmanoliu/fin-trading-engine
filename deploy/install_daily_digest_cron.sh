#!/usr/bin/env bash
# install_daily_digest_cron.sh — install the daily digest cron on VPS.
#
# Idempotent: removes any existing daily_digest entry before adding.
# Reads TELEGRAM env from /etc/paper-live/env (same as engines).
#
# Usage (from local machine):
#   ssh root@178.105.24.230 'bash /opt/trading-engine/deploy/install_daily_digest_cron.sh'
set -euo pipefail

# set -a/+a auto-exports the (now plain, no-`export`) env so the cron's child
# inherits TELEGRAM_*. The env file dropped `export` for systemd EnvironmentFile
# (see deploy/install.sh); don't revert to bare `. env &&` or alerts go silent.
CRON_LINE='0 9 * * * set -a && . /etc/paper-live/env && set +a && /opt/trading-engine/scripts/daily_digest.sh >> /var/log/paper-live/daily_digest.log 2>&1'

( crontab -l 2>/dev/null | grep -v 'daily_digest' || true ; echo "$CRON_LINE" ) | crontab -

echo "daily_digest cron installed: 09:00 UTC daily"
echo "  Log: /var/log/paper-live/daily_digest.log"
echo "  Verify: crontab -l | grep daily_digest"
