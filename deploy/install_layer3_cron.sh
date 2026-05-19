#!/usr/bin/env bash
# install_layer3_cron.sh — install the weekly Layer 3 verdict cron on VPS.
#
# Idempotent: removes any existing layer3_cron entry before adding.
# Reads TELEGRAM env from /etc/paper-live/env (same as other operational
# crons). Cadence: Sunday 10:00 UTC weekly — Layer 3 needs ≥7d window so
# running more often is wasteful and would inflate Telegram noise.
# Aligned with the existing weekly cadence discipline (funding_refresh
# Sun 03:00, weekly_audit Sun 09:00 local, daily_digest 09:00 daily).
#
# Per real_money_executor_architecture_decision_rule_2026-05-08.md, this
# is the passive monitor for Layer 3 — operator only sees Telegram on
# state CHANGES (PASS first-time, FAIL, recovery). Silent during the
# initial 7d INSUFFICIENT_DURATION window.
#
# Usage (from local machine):
#   ssh root@178.105.24.230 'bash /opt/trading-engine/deploy/install_layer3_cron.sh'
set -euo pipefail

CRON_LINE='0 10 * * 0 . /etc/paper-live/env && /opt/trading-engine/scripts/layer3_cron.sh --quiet >> /var/log/paper-live/layer3_cron.log 2>&1'

# Replace any existing layer3_cron entry idempotently.
( crontab -l 2>/dev/null | grep -v 'layer3_cron' || true ; echo "$CRON_LINE" ) | crontab -

# Ensure the log file + parent dirs exist with operator-owned perms so the
# first cron firing doesn't trip permission errors.
install -d -m 755 -o paperlive -g paperlive /var/log/paper-live/layer3_runs 2>/dev/null || \
    mkdir -p /var/log/paper-live/layer3_runs
touch /var/log/paper-live/layer3_cron.log
chown paperlive:paperlive /var/log/paper-live/layer3_cron.log 2>/dev/null || true

echo "layer3_cron installed: Sunday 10:00 UTC weekly"
echo "  Log:     /var/log/paper-live/layer3_cron.log"
echo "  Runs:    /var/log/paper-live/layer3_runs/"
echo "  History: /var/log/paper-live/layer3_history.jsonl"
echo "  Verify:  crontab -l | grep layer3_cron"
