#!/usr/bin/env bash
# Mechanical applicator for shadow_promotion_decision_rule_2026-05-27.md
# Run when ANY shadow first hits n>=63. Computes all 9 gates and prints
# PASS/FAIL per gate. Writes results/shadow_promotion_eval_<DATE>.md as
# stub for human completion.
#
# Reads forward-paper journals from VPS. Does NOT make any deploy changes.
#
# Usage:
#   ./scripts/shadow_promotion_eval.sh                # eval all shadows currently >=63 closes
#   ./scripts/shadow_promotion_eval.sh alt5-15-336    # specific cohort
#
# Cohort name list: alt5-15-336 alt5-15-504 bb20 alt5-21-504 alt7-14-504 alt10-30-504 alt12-26-504 alt21-50-504

set -euo pipefail

VPS="${VPS:-root@178.105.24.230}"
DATE="$(date -u +%Y-%m-%d)"
COHORTS="${1:-alt5-15-336 alt5-15-504 bb20 alt5-21-504 alt7-14-504 alt10-30-504 alt12-26-504 alt21-50-504}"
EVAL_DOC="results/shadow_promotion_eval_${DATE}.md"
POWER_FLOOR=63
P_VALUE_BAR=0.001
EFFECT_MULT=1.5
ANTI_CLUSTER_MULT=0.4

echo "=== Shadow promotion eval — $DATE ==="
echo "Power floor: $POWER_FLOOR | p-bar: $P_VALUE_BAR | effect: ${EFFECT_MULT}x | anti-cluster: ${ANTI_CLUSTER_MULT}x"
echo

# Fetch live cohort metrics
echo "--- LIVE ---"
LIVE_DATA=$(ssh "$VPS" 'cat /var/log/paper-live/journal/*-2026-*.jsonl 2>/dev/null' \
  | python3 -c '
import json, sys, collections
n_close = 0
pnl_sum = 0.0
wins = 0
per_day = collections.defaultdict(float)
for line in sys.stdin:
    try:
        ev = json.loads(line)
    except Exception:
        continue
    if ev.get("event") != "close":
        continue
    n_close += 1
    p = float(ev.get("pnl_usd", 0))
    pnl_sum += p
    if p > 0:
        wins += 1
    day = ev["ts"][:10]
    per_day[day] += p

# Robust = remove 2 best calendar days
best_2_days = sorted(per_day.values(), reverse=True)[:2]
pnl_robust = pnl_sum - sum(best_2_days)
print(f"n_close={n_close}")
print(f"pnl_raw={pnl_sum:.2f}")
print(f"pnl_robust={pnl_robust:.2f}")
print(f"wins={wins}")
print(f"wr={(wins/n_close*100) if n_close else 0:.2f}")
')
echo "$LIVE_DATA"
LIVE_N=$(echo "$LIVE_DATA" | awk -F= '/^n_close=/{print $2}')
LIVE_PNL=$(echo "$LIVE_DATA" | awk -F= '/^pnl_raw=/{print $2}')
LIVE_WR=$(echo "$LIVE_DATA" | awk -F= '/^wr=/{print $2}')
LIVE_WINS=$(echo "$LIVE_DATA" | awk -F= '/^wins=/{print $2}')
echo

# Each shadow
for cohort in $COHORTS; do
  echo "--- $cohort ---"
  SHADOW_DATA=$(ssh "$VPS" "cat /var/log/paper-live/journal/shadow/$cohort/*-2026-*.jsonl 2>/dev/null" \
    | python3 -c '
import json, sys, collections
n_close = 0
pnl_sum = 0.0
wins = 0
per_day = collections.defaultdict(float)
for line in sys.stdin:
    try:
        ev = json.loads(line)
    except Exception:
        continue
    if ev.get("event") != "close":
        continue
    n_close += 1
    p = float(ev.get("pnl_usd", 0))
    pnl_sum += p
    if p > 0:
        wins += 1
    day = ev["ts"][:10]
    per_day[day] += p

best_2_days = sorted(per_day.values(), reverse=True)[:2]
pnl_robust = pnl_sum - sum(best_2_days)
print(f"n_close={n_close}")
print(f"pnl_raw={pnl_sum:.2f}")
print(f"pnl_robust={pnl_robust:.2f}")
print(f"wins={wins}")
print(f"wr={(wins/n_close*100) if n_close else 0:.2f}")
print(f"best_2_days_pnl={sum(best_2_days):.2f}")
')
  echo "$SHADOW_DATA"
  S_N=$(echo "$SHADOW_DATA" | awk -F= '/^n_close=/{print $2}')
  S_PNL_RAW=$(echo "$SHADOW_DATA" | awk -F= '/^pnl_raw=/{print $2}')
  S_PNL_ROBUST=$(echo "$SHADOW_DATA" | awk -F= '/^pnl_robust=/{print $2}')
  S_WR=$(echo "$SHADOW_DATA" | awk -F= '/^wr=/{print $2}')
  S_WINS=$(echo "$SHADOW_DATA" | awk -F= '/^wins=/{print $2}')

  # Compute gates via python (one call, structured output)
  python3 <<PYEOF
import math

# Inputs
live_n = $LIVE_N
live_wr = $LIVE_WR
live_pnl = $LIVE_PNL
live_wins = $LIVE_WINS
s_n = $S_N
s_wr = $S_WR
s_pnl_raw = $S_PNL_RAW
s_pnl_robust = $S_PNL_ROBUST
s_wins = $S_WINS

# Gate 1: shadow n >= power floor
g1 = s_n >= $POWER_FLOOR
print(f"  Gate 1 (shadow n>={$POWER_FLOOR}):     {'PASS' if g1 else 'FAIL'}  (n={s_n})")

# Gate 2: live n >= power floor
g2 = live_n >= $POWER_FLOOR
print(f"  Gate 2 (live n>={$POWER_FLOOR}):       {'PASS' if g2 else 'FAIL'}  (n={live_n})")

# Gate 3: live PnL trajectory documented — manual check; print value
print(f"  Gate 3 (live PnL documented): MANUAL  (live cumulative_pnl=\${live_pnl:+.2f})")

# Gate 4: two-proportion z-test WR (live vs shadow), p < 0.001 Bonferroni
if live_n > 0 and s_n > 0:
    p1 = live_wins / live_n
    p2 = s_wins / s_n
    p_pool = (live_wins + s_wins) / (live_n + s_n)
    se = math.sqrt(p_pool * (1 - p_pool) * (1/live_n + 1/s_n))
    z = (p2 - p1) / se if se > 0 else 0.0
    # Two-tailed p approx via normal CDF
    from math import erf, sqrt
    pval = 2 * (1 - 0.5 * (1 + erf(abs(z)/sqrt(2))))
    g4 = pval < $P_VALUE_BAR
    print(f"  Gate 4 (z-test p<{$P_VALUE_BAR}):      {'PASS' if g4 else 'FAIL'}  (z={z:+.2f}, p={pval:.4f})")
else:
    print(f"  Gate 4 (z-test p<{$P_VALUE_BAR}):      SKIP  (n=0)")

# Gate 5: shadow PnL >= 1.5 * |live PnL| OR (shadow >= +20k AND live <= 0)
g5a = s_pnl_raw >= $EFFECT_MULT * abs(live_pnl)
g5b = (s_pnl_raw >= 20000) and (live_pnl <= 0)
g5 = g5a or g5b
print(f"  Gate 5 (effect size):         {'PASS' if g5 else 'FAIL'}  (shadow=\${s_pnl_raw:+.2f} live=\${live_pnl:+.2f} ratio={s_pnl_raw/abs(live_pnl) if live_pnl != 0 else float('inf'):.2f}x)")

# Gate 6: mechanism — MANUAL (cannot auto-compute shared-day analysis cheaply)
print(f"  Gate 6 (mechanism):           MANUAL  (run shared-day shared-symbol analysis)")

# Gate 7: cost-stack honest — MANUAL (compute from fee_usd / slip_usd / notional_usd)
print(f"  Gate 7 (cost stack):          MANUAL  (compute realized bp from journal)")

# Gate 8: shadow-vs-shadow ambiguity — applies after multi-cohort eval
print(f"  Gate 8 (shadow-vs-shadow):    MANUAL  (compare to other cohorts hitting all 1-7)")

# Gate 9: anti-cluster
g9a = s_pnl_robust > 0
g9b = s_pnl_robust > $ANTI_CLUSTER_MULT * s_pnl_raw if s_pnl_raw > 0 else False
g9 = g9a and g9b
ratio = s_pnl_robust / s_pnl_raw if s_pnl_raw != 0 else float('nan')
print(f"  Gate 9 (anti-cluster):        {'PASS' if g9 else 'FAIL'}  (robust=\${s_pnl_robust:+.2f} raw=\${s_pnl_raw:+.2f} ratio={ratio:.2f})")
PYEOF
  echo
done

echo "=== Eval stub doc to write ==="
echo "Path: $EVAL_DOC"
echo "Required sections (per Phase A of rule):"
echo "  1. Per-gate evaluation with PASS/FAIL"
echo "  2. p-values for Gate 4"
echo "  3. Effect-size numbers for Gate 5"
echo "  4. Mechanism analysis for Gate 6 (shared-day shared-symbol)"
echo "  5. Cost-stack realized bp for Gate 7"
echo "  6. Anti-cluster numbers for Gate 9 (already computed above)"
echo "  7. Snapshot reference: snapshot/cohort-snapshot-$DATE.txt"
echo "  8. Decision: PROMOTE <cohort> | NO-PROMOTE | DEFER (with rationale)"
