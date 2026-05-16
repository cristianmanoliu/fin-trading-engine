#!/usr/bin/env bash
# install_daily_digest_cron.sh — install the daily digest cron on VPS.
#
# Idempotent: removes any existing daily_digest entry before adding.
# Reads TELEGRAM env from /etc/paper-live/env (same as engines).
#
# Usage (from local machine):
#   ssh root@178.105.24.230 'bash /opt/trading-engine/deploy/install_daily_digest_cron.sh'
set -euo pipefail

CRON_LINE='0 9 * * * . /etc/paper-live/env && /opt/trading-engine/scripts/daily_digest.sh >> /var/log/paper-live/daily_digest.log 2>&1'

( crontab -l 2>/dev/null | grep -v 'daily_digest' || true ; echo "$CRON_LINE" ) | crontab -

echo "daily_digest cron installed: 09:00 UTC daily"
echo "  Log: /var/log/paper-live/daily_digest.log"
echo "  Verify: crontab -l | grep daily_digest"
