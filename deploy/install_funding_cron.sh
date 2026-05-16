#!/usr/bin/env bash
# install_funding_cron.sh — install the weekly funding-CSV refresh cron on VPS.
#
# Idempotent: removes any existing funding_refresh entry before adding.
# Reads TELEGRAM env from /etc/paper-live/env (same as engines).
#
# Usage (from local machine):
#   ssh root@178.105.24.230 'bash /opt/trading-engine/deploy/install_funding_cron.sh'
set -euo pipefail

CRON_LINE='0 3 * * 0 . /etc/paper-live/env && /opt/trading-engine/scripts/funding_refresh_cron.sh >> /var/log/paper-live/funding_refresh.log 2>&1'

( crontab -l 2>/dev/null | grep -v 'funding_refresh' || true ; echo "$CRON_LINE" ) | crontab -

echo "funding_refresh cron installed: Sunday 03:00 UTC weekly"
echo "  Log: /var/log/paper-live/funding_refresh.log"
echo "  Verify: crontab -l | grep funding_refresh"
