# Telegram/systemd env fix — prod-activation runbook (2026-05-30)

## The bug

`/etc/paper-live/env` was written with `export KEY=VAL` (commit `d6d6061`, to make cron-sourced
children inherit the vars). But systemd `EnvironmentFile=` parses `KEY=VAL` **literally** and rejects
`export KEY=VAL` — it logs `Ignoring invalid environment assignment 'export TELEGRAM_CHAT_ID=…'` and
drops the var. Consequence on all 16 live engines (and watchdog/digest *services*):

- `TELEGRAM_BOT_TOKEN`, `TELEGRAM_CHAT_ID` unset in the engine env → `pkg/notify` `FromEnv` → **engine
  Telegram is a silent no-op** (startup/shutdown/CRITICAL alerts never send).
- `PAPER_LIVE_SIGNAL_CONTEXT_DIR` unset → C2 signal-context sidecars not written (if relied on via env).

Cron paths (`. env && …`) were unaffected (shell honours `export`) — which is why daily_digest/funding
Telegram kept working and the bug stayed hidden.

## The fix (repo — DONE, commit `6805add`)

- `deploy/install.sh`: writes **plain `KEY=VALUE`** (systemd-valid). `kill_switch.sh` already uses
  `set -a`, unaffected.
- `deploy/install_{funding,layer3,daily_digest}_cron.sh`: crontab lines now
  `set -a && . /etc/paper-live/env && set +a && <cmd>` so cron children still inherit. **Backward-
  compatible**: the new crontab works with both the old (export) and new (plain) env file.

This corrects **fresh installs**. The existing VPS env file is untouched by a repo change → activation
below is required to fix the running cohort.

## Prod activation (existing VPS — NOT done; do at a maintenance window)

Ordered so cron alerting is never broken mid-way (new crontab first — it handles both env formats —
then strip `export`, then restart). Never read/echo the env file (token).

```bash
# 0. Sync the fixed repo to VPS (sync.sh protects VPS funding CSVs; does NOT restart engines)
./deploy/sync.sh

# 1. Reinstall the 3 crons with the new `set -a` lines (works with current export env too)
ssh root@178.105.24.230 'bash /opt/trading-engine/deploy/install_funding_cron.sh \
  && bash /opt/trading-engine/deploy/install_layer3_cron.sh \
  && bash /opt/trading-engine/deploy/install_daily_digest_cron.sh'

# 2. Strip `export` from the live env (backup first; sed never prints values)
ssh root@178.105.24.230 'cp /etc/paper-live/env /etc/paper-live/env.bak.$(date +%Y%m%d) \
  && sed -i "s/^export //" /etc/paper-live/env'

# 3. Restart engines to load the now-valid env  ⚠️ DISRUPTIVE
#    Triggers journal-replay recovery + ~16 startup-grace Telegram warns that auto-clear.
#    Do deliberately, not mid-investigation.
ssh root@178.105.24.230 'systemctl restart "paper-live@*.service"'

# 4. Verify the rejection is gone (should print NOTHING)
ssh root@178.105.24.230 'journalctl -u "paper-live@*.service" --since "2 min ago" \
  | grep -i "Ignoring invalid environment"'
#    And confirm an engine startup Telegram actually arrived on your phone.
```

## Why activation is deferred

Paper-mode: the **decision-grade** paths are unaffected — the drift-kill wrapper runs via local launchd
(local env) and daily_digest/funding alerts run via cron (`. env`, works). Only *engine-originated*
alerts are down, which matters most at **real-money** promotion (months out). Restarting the 16-engine
cohort mid-forward-paper is an avoidable disruption (journal-replay + startup-grace flood), so the fix
is banked in the repo and activated at the next natural redeploy or a chosen window.

## Cross-references
- Commit `6805add` — the repo fix
- `deploy/install.sh`, `deploy/install_{funding,layer3,daily_digest}_cron.sh`
- `pkg/notify/telegram.go` `FromEnv` — the consumer that silently no-ops when env unset
- Regression origin: commit `d6d6061` (export-for-cron)
