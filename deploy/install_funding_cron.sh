#!/usr/bin/env bash
# install_funding_cron.sh — install the weekly funding-CSV refresh cron on VPS.
#
# Idempotent: removes any existing funding_refresh entry before adding.
# Reads TELEGRAM env from /etc/paper-live/env (same as engines).
#
# Usage (from local machine):
#   ssh root@178.105.24.230 'bash /opt/trading-engine/deploy/install_funding_cron.sh'
set -euo pipefail

# set -a/+a auto-exports the (now plain, no-`export`) env so the cron's child
# inherits TELEGRAM_*. The env file dropped `export` for systemd EnvironmentFile
# (see deploy/install.sh); don't revert to bare `. env &&` or alerts go silent.
CRON_LINE='0 3 * * 0 set -a && . /etc/paper-live/env && set +a && /opt/trading-engine/scripts/funding_refresh_cron.sh >> /var/log/paper-live/funding_refresh.log 2>&1'

( crontab -l 2>/dev/null | grep -v 'funding_refresh' || true ; echo "$CRON_LINE" ) | crontab -

echo "funding_refresh cron installed: Sunday 03:00 UTC weekly"
echo "  Log: /var/log/paper-live/funding_refresh.log"
echo "  Verify: crontab -l | grep funding_refresh"
