#!/usr/bin/env bash
# Verify IMXUSDT max-hold force-close fired as expected at 2026-05-30T00:00:00Z (alt5-15-336)
# and/or 2026-06-06T00:00:00Z (alt5-15-504). Run post-fire to confirm:
#   - close event present w/ outcome consistent with max-hold mechanism
#   - pnl_usd recorded
#   - engine still alive after close (no crash from time-stop branch)
#
# Baseline: results/snapshots/cohort_snapshot_2026-05-27_pre_imx_force_close.txt
#
# Usage:
#   ./scripts/verify_imx_force_close.sh             # checks both 336 + 504 cohorts
#   ./scripts/verify_imx_force_close.sh alt5-15-336 # specific cohort only

set -euo pipefail

VPS="${VPS:-root@178.105.24.230}"
COHORTS="${1:-alt5-15-336 alt5-15-504}"
EXPECTED_ENTRY="0.1851"
EXPECTED_OPEN_TS="2026-05-16T00:00:00Z"

echo "=== IMXUSDT force-close verification ==="
echo "VPS:       $VPS"
echo "Cohorts:   $COHORTS"
echo "Expected:  entry=$EXPECTED_ENTRY opened=$EXPECTED_OPEN_TS"
echo

for cohort in $COHORTS; do
  echo "--- $cohort ---"
  ssh "$VPS" "cat /var/log/paper-live/journal/shadow/$cohort/IMXUSDT-2026-*.jsonl 2>/dev/null" \
    | python3 -c '
import json, sys
opens = []
closes = []
for line in sys.stdin:
    try:
        ev = json.loads(line)
    except Exception:
        continue
    if ev.get("event") == "open" and ev.get("ts") == "'"$EXPECTED_OPEN_TS"'":
        opens.append(ev)
    elif ev.get("event") == "close":
        closes.append(ev)

if not opens:
    print(f"  NO MATCHING OPEN @ '"$EXPECTED_OPEN_TS"'")
    sys.exit(2)

# Find matching close (entry must match)
matched = None
for c in closes:
    if abs(c.get("entry", 0) - '"$EXPECTED_ENTRY"') < 1e-6:
        matched = c
        break

if not matched:
    print(f"  OPEN found ts={opens[0][\"ts\"]} entry={opens[0][\"entry\"]} — NO CLOSE YET (position still open)")
    sys.exit(0)

import datetime as dt
open_t = dt.datetime.fromisoformat(opens[0]["ts"].replace("Z","+00:00"))
close_t = dt.datetime.fromisoformat(matched["ts"].replace("Z","+00:00"))
hours = (close_t - open_t).total_seconds() / 3600
print(f"  CLOSED  entry={matched[\"entry\"]:.4f} exit={matched[\"exit\"]:.4f}")
print(f"          held={hours:.2f}h  pnl_usd={matched[\"pnl_usd\"]:+.2f}  outcome={matched.get(\"outcome\",\"?\")}")
print(f"          mfe_r={matched.get(\"mfe_r\",\"?\")} mae_r={matched.get(\"mae_r\",\"?\")} notional={matched.get(\"notional_usd\",\"?\")}")
# Sanity: max-hold cohort tag in name -> expect hours close to cap
cap = 336 if "336" in "'"$cohort"'" else 504 if "504" in "'"$cohort"'" else None
if cap and abs(hours - cap) > 1.0:
    print(f"  WARN    held={hours:.2f}h diverges from cap={cap}h by >1h — was this max-hold or a normal exit?")
else:
    print(f"  OK      hold matches cap (~{cap}h)")
'
  echo
done

echo "=== Engine liveness check ==="
ssh "$VPS" 'systemctl is-active paper-live@imxusdt.service'
echo
echo "Diff snapshot: results/snapshots/cohort_snapshot_2026-05-27_pre_imx_force_close.txt"
