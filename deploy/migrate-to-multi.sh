#!/usr/bin/env bash
# migrate-to-multi.sh — switch VPS from 16 paper-live@*.service units to
# the single paper-live-multi.service.
#
# Idempotent: safe to re-run. Verifies each step.
#
# Usage: ./deploy/migrate-to-multi.sh [HOST]
#   HOST defaults to root@178.105.24.230
set -euo pipefail

HOST="${1:-root@178.105.24.230}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"

echo "=== 1. Sync code + new unit file to $HOST ==="
rsync -avz --delete \
  --exclude='data/' --exclude='results/' --exclude='logs/' --exclude='.git/' \
  "$ROOT/" "$HOST:/opt/trading-engine/"
rsync -avz "$ROOT/data/funding/" "$HOST:/opt/trading-engine/data/funding/"

echo ""
echo "=== 2. Rebuild engine binary on VPS ==="
ssh "$HOST" 'cd /opt/trading-engine && go build -o bin/engine ./cmd/engine/'

echo ""
echo "=== 3. Stop old per-symbol units + timers ==="
ssh "$HOST" '
  systemctl stop "paper-live@*.service" || true
  systemctl disable "paper-live@*.service" 2>/dev/null || true
  systemctl stop paper-live-watchdog.timer paper-live-digest.timer || true
'

echo ""
echo "=== 4. Install multi unit ==="
ssh "$HOST" '
  cp /opt/trading-engine/deploy/systemd/paper-live-multi.service /etc/systemd/system/
  systemctl daemon-reload
  systemd-analyze verify /etc/systemd/system/paper-live-multi.service
'

echo ""
echo "=== 5. Start multi engine ==="
ssh "$HOST" 'systemctl enable --now paper-live-multi.service'

echo ""
echo "=== 6. Wait 90s for backfill + first ticks ==="
sleep 90

echo ""
echo "=== 7. Health check ==="
ssh "$HOST" '
  echo "--- service state ---"
  systemctl is-active paper-live-multi.service
  echo ""
  echo "--- recent log (last 30 lines) ---"
  tail -30 /var/log/paper-live/multi.log
  echo ""
  echo "--- ticks per symbol (heartbeat sample) ---"
  for s in ROSEUSDT MKRUSDT GRTUSDT 1INCHUSDT ADAUSDT KAVAUSDT 1000SHIBUSDT ENSUSDT XLMUSDT IMXUSDT ETCUSDT RUNEUSDT AVAXUSDT FTMUSDT DOTUSDT FILUSDT; do
    last_hb=$(grep "\"symbol\":\"$s\"" /var/log/paper-live/multi.log | grep "heartbeat" | tail -1)
    if [ -n "$last_hb" ]; then
      echo "  $s: $(echo $last_hb | grep -oE "\"ticks_since_last\":[0-9]+" | head -1)"
    else
      echo "  $s: NO HEARTBEAT YET"
    fi
  done
'

echo ""
echo "=== 8. Re-enable watchdog/digest timers (point them at multi log) ==="
ssh "$HOST" 'systemctl enable --now paper-live-watchdog.timer paper-live-digest.timer || true'

echo ""
echo "Migration complete."
