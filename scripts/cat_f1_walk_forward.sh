#!/usr/bin/env bash
# cat_f1_walk_forward.sh — 6-window walk-forward for the Cat F1 funding-cross
# standalone signal. Pre-registered 2026-05-07 — see
# results/cat_f1_funding_cross_decision_rule_2026-05-07.md.
#
# Windows (locked, identical to deployed-candidate walk-forward):
#   W-2: 2020-05 → 2021-04
#   W-1: 2021-05 → 2022-04
#   W0:  2022-05 → 2023-04
#   W1:  2023-05 → 2024-04
#   W2:  2024-05 → 2025-04
#   W3:  2025-05 → 2026-04   (true OOS)
#
# Decision rule (locked):
#   SHADOW DEPLOY: ≥4/6 windows positive AND mean ≥ $50k/yr AND r < 0.5 with deployed
#   WALK-FORWARD CANDIDATE: ≥3/6 windows positive AND mean > 0
#   REJECT: otherwise
#
# Output: results/cat_f1_walk_forward_<date>.csv (per-window NET + decision)
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DATE_TAG="${DATE_TAG:-$(date +%F)}"
OUT="${OUT:-${ROOT}/results/cat_f1_walk_forward_${DATE_TAG}.csv}"

# Locked walk-forward windows.
WINDOWS=(
    "W-2  2020 5  2021 4"
    "W-1  2021 5  2022 4"
    "W0   2022 5  2023 4"
    "W1   2023 5  2024 4"
    "W2   2024 5  2025 4"
    "W3   2025 5  2026 4"
)

echo "→ Cat F1 walk-forward: 6 windows × deployed-16 × threshold=30bp/day"
echo "→ Decision rule LOCKED — see results/cat_f1_funding_cross_decision_rule_2026-05-07.md"
echo ""

echo "window,start,end,n_symbols,n_profitable,total_trades,total_wins,total_net,annual_net,long_net,short_net" > "$OUT"

declare -a window_nets
for win in "${WINDOWS[@]}"; do
    read -r label sy sm ey em <<< "$win"
    echo "→ $label  ($sy-$(printf '%02d' $sm) → $ey-$(printf '%02d' $em))"
    win_csv="$(mktemp /tmp/f1-wf-${label}.XXXXXXXX)"

    OUT="$win_csv" \
    DATE_TAG="${DATE_TAG}-${label}" \
    START_YEAR="$sy" START_MONTH="$sm" \
    END_YEAR="$ey" END_YEAR_MONTH="$em" \
    bash "${ROOT}/scripts/cat_f1_run.sh" > /dev/null 2>&1

    # Aggregate per-window.
    python3 - "$win_csv" "$label" "$sy-$(printf '%02d' $sm)" "$ey-$(printf '%02d' $em)" >> "$OUT" <<'EOF'
import csv, sys
rows = list(csv.DictReader(open(sys.argv[1])))
n = sum(1 for r in rows if r["net_usd"])
n_prof = sum(1 for r in rows if float(r.get("net_usd","0") or 0) > 0)
total = sum(float(r.get("net_usd","0") or 0) for r in rows)
trades = sum(int(r.get("trades","0") or 0) for r in rows)
wins = sum(int(r.get("wins","0") or 0) for r in rows)
long_net = sum(float(r.get("long_net","0") or 0) for r in rows)
short_net = sum(float(r.get("short_net","0") or 0) for r in rows)
# Each window is exactly 12 months → annualize by 1.
print(f"{sys.argv[2]},{sys.argv[3]},{sys.argv[4]},{n},{n_prof},{trades},{wins},{total:.2f},{total:.2f},{long_net:.2f},{short_net:.2f}")
EOF

    rm -f "$win_csv"
    last_line=$(tail -1 "$OUT")
    win_net=$(echo "$last_line" | cut -d, -f8)
    win_prof=$(echo "$last_line" | cut -d, -f5)
    win_n=$(echo "$last_line" | cut -d, -f4)
    win_trades=$(echo "$last_line" | cut -d, -f6)
    printf "    NET=\$%s  trades=%s  profitable=%s/%s\n" "$win_net" "$win_trades" "$win_prof" "$win_n"
    window_nets+=("$win_net")
done

echo ""
echo "→ wrote $OUT"
echo ""

# Apply pre-registered decision rule.
python3 - "$OUT" <<'EOF'
import csv, sys, math
from pathlib import Path

rows = list(csv.DictReader(open(sys.argv[1])))
nets = [float(r["total_net"]) for r in rows]
n_pos = sum(1 for n in nets if n > 0)
mean_net = sum(nets) / len(nets) if nets else 0
mean_annual = mean_net  # each window is 12 months

print("="*80)
print("CAT F1 — WALK-FORWARD VERDICT (pre-registered decision rule applied)")
print("="*80)
print()
print(f"{'Window':<6}  {'Range':<22}  {'Trades':>7}  {'NET $':>14}")
print(f"{'-'*6}  {'-'*22}  {'-'*7}  {'-'*14}")
for r in rows:
    win = r["window"]
    rng = f"{r['start']} → {r['end']}"
    trades = r["total_trades"]
    net = float(r["total_net"])
    sign = "+" if net >= 0 else ""
    print(f"{win:<6}  {rng:<22}  {trades:>7}  ${sign}{net:>12,.0f}")
print(f"{'-'*6}  {'-'*22}  {'-'*7}  {'-'*14}")
sign = "+" if mean_annual >= 0 else ""
print(f"{'mean':<6}  {'':<22}  {'':<7}  ${sign}{mean_annual:>12,.0f}/yr")
print(f"{'positive windows':<31}  {n_pos}/{len(rows)}")
print()

# Decision rule.
print(f"PRE-REGISTERED DECISION RULE:")
print(f"  SHADOW DEPLOY:  ≥4/6 windows positive AND mean ≥ $50k/yr AND r < 0.5")
print(f"  WALK-FORWARD CANDIDATE: ≥3/6 windows positive AND mean > 0")
print(f"  REJECT: otherwise")
print()
print(f"OBSERVED:")
print(f"  positive windows:   {n_pos}/{len(rows)}")
print(f"  mean annual NET:    ${mean_annual:+,.0f}/yr")
print()

# Tier evaluation.
if n_pos >= 4 and mean_annual >= 50_000:
    print("⚠ Conditions for SHADOW DEPLOY met on count/mean. Final tier requires correlation")
    print("  check r < 0.5 with deployed-candidate per-window NET — pending separate calc.")
elif n_pos >= 3 and mean_annual > 0:
    print("✓ VERDICT: WALK-FORWARD CANDIDATE — eligible for re-test in next milestone.")
    print("  NOT deployed; requires forward-paper rules to settle before next iteration.")
else:
    print("✗ VERDICT: REJECT — falls below the WALK-FORWARD CANDIDATE threshold.")
    print("  No threshold-sweep retry permitted under the pre-registered rule.")
EOF
