#!/usr/bin/env bash
# consolidate_shortonly.sh — read all SHORT_ONLY summary.txt files and produce
# a ranked comparison table + write the consolidated verdict doc.
#
# Prerequisites:
#   tasks/shadow_regime_gate_results/<shadow>/summary.txt  (8 shadows)
#   tasks/regime_gate_results_shortonly/summary.txt        (live config)
#
# Usage:
#   bash tasks/consolidate_shortonly.sh
#
# Outputs:
#   results/shadow_regime_gate_shortonly_verdict_2026-06-08.md
#   (and prints the table to stdout)
#
# Research-only. No engine changes. No live/shadow/VPS interaction.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"; cd "$ROOT"

TODAY="$(date +%Y-%m-%d)"
OUTFILE="$ROOT/results/shadow_regime_gate_shortonly_verdict_${TODAY}.md"

SHADOWS=(alt5-15-336 alt5-15-504 bb20 alt5-21-504 alt7-14-504 alt10-30-504 alt12-26-504 alt21-50-504)

check_summaries() {
  local missing=0
  local f
  for s in "${SHADOWS[@]}"; do
    f="$ROOT/tasks/shadow_regime_gate_results/$s/summary.txt"
    if [[ ! -f "$f" ]]; then
      echo "MISSING: $f" >&2; missing=1
    fi
  done
  f="$ROOT/tasks/regime_gate_results_shortonly/summary.txt"
  if [[ ! -f "$f" ]]; then
    echo "MISSING: $f (live-config short-only)" >&2; missing=1
  fi
  if [[ $missing -eq 1 ]]; then
    echo "ERROR: not all summaries ready. Check grid progress via:" >&2
    echo "  tail /tmp/run_all_grids.log  (shadows)" >&2
    echo "  tail /tmp/live_shortonly_grid.log  (live)" >&2
    exit 1
  fi
}

# Parse a summary.txt and emit: cohort gate_total baseline_total delta verdict picks
parse_summary() {
  local cohort="$1" f="$2"
  python3 - "$cohort" "$f" <<'PYEOF'
import sys, re

cohort = sys.argv[1]
path   = sys.argv[2]

with open(path) as fh:
    txt = fh.read()

gate_total     = int(re.search(r'OOS gate total:\s+([-\d]+)',     txt).group(1))
baseline_total = int(re.search(r'OOS baseline total:\s+([-\d]+)', txt).group(1))
verdict        = re.search(r'VERDICT:\s+(\w+)', txt).group(1)
picks_str      = re.search(r'picks:\s+\[(.+?)\]', txt).group(1)
# Extract distinct picks (X,Y,Z) tuples
picks = re.findall(r'\((\d+),\s*(\d+),\s*(\d+)\)', picks_str)
distinct = sorted(set(picks))
if all(p == distinct[0] for p in picks):
    pick_disp = f"({distinct[0][0]},{distinct[0][1]},{distinct[0][2]})"
else:
    pick_disp = " / ".join(f"({p[0]},{p[1]},{p[2]})" for p in distinct)

delta = gate_total - baseline_total
degenerate = (gate_total == baseline_total)

print(f"{cohort}\t{gate_total}\t{baseline_total}\t{delta}\t{verdict}\t{pick_disp}\t{degenerate}")
PYEOF
}

check_summaries

# Collect rows
declare -a ROWS
while IFS=$'\t' read -r cohort gate base delta verdict picks degen; do
  ROWS+=("$cohort|$gate|$base|$delta|$verdict|$picks|$degen")
done < <(
  parse_summary "live-9/21-504" "$ROOT/tasks/regime_gate_results_shortonly/summary.txt"
  for s in "${SHADOWS[@]}"; do
    parse_summary "$s" "$ROOT/tasks/shadow_regime_gate_results/$s/summary.txt"
  done
)

# Sort by delta descending (python sort)
SORTED="$(python3 -c "
import sys
rows = sys.stdin.read().strip().split('\n')
def delta(r): return int(r.split('|')[3])
rows.sort(key=delta, reverse=True)
print('\n'.join(rows))
" <<< "$(printf '%s\n' "${ROWS[@]}")")"

# Print table to stdout
echo ""
echo "SHORT_ONLY regime-gate — consolidated results (ranked by OOS edge)"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
printf "%-18s %10s %10s %10s  %-9s  %-20s  %s\n" \
  "cohort" "gate_OOS" "base_OOS" "edge" "verdict" "picks" "notes"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
while IFS='|' read -r cohort gate base delta verdict picks degen; do
  notes=""
  [[ "$degen" == "True" ]] && notes="DEGENERATE(gate=base)"
  printf "%-18s %10s %10s %10s  %-9s  %-20s  %s\n" \
    "$cohort" "$gate" "$base" "$delta" "$verdict" "$picks" "$notes"
done <<< "$SORTED"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

# Write the verdict doc
mkdir -p results
python3 - "$OUTFILE" "$TODAY" <<'PYEOF' <<< "$SORTED"
import sys, datetime

outfile = sys.argv[1]
today   = sys.argv[2]
rows_raw = sys.stdin.read().strip().split('\n')

rows = []
for r in rows_raw:
    parts = r.split('|')
    cohort, gate, base, delta, verdict, picks, degen = parts
    rows.append({
        "cohort": cohort, "gate": int(gate), "base": int(base),
        "delta": int(delta), "verdict": verdict, "picks": picks,
        "degen": degen == "True"
    })

positives  = [r for r in rows if r["verdict"] == "POSITIVE"]
negatives  = [r for r in rows if r["verdict"] == "NEGATIVE"]
degenerates= [r for r in rows if r["degen"]]

doc = f"""# BTC-anchored regime gate (SHORT_ONLY) — cross-cohort verdict

**Date:** {today}
**Question:** For each EMA configuration (live + 8 deployed shadows), does the BTC short-gate
(sit FLAT during non-bearish BTC regimes) beat always-short in OOS walk-forward?

**Gate:** BTC trailing return ≤ −X% over Y days → SHORT regime; else → FLAT (LONG suppressed).
**SHORT_ONLY=1:** LONG days from the regime labeler are relabeled FLAT. The strategy *only*
sits out; it never flips long. This is the deployable hypothesis — LONG was dropped as unstable
noise (breadth verdict 2026-06-08).

**Criteria (pre-registered, locked):**
1. Gate OOS total beats always-short OOS total.
2. No two adjacent losing OOS years.
3. ≤2 distinct (X,Y,Z) picks across 4 folds, same X.

**Anchor:** `data/anchor/BTCUSDT-1d.csv`
**Universe:** 57 symbols, 5y continuous, fee=10bp, slip=5bp, exact-fills, include-boundary.

---

## Results table (ranked by OOS edge = gate − baseline)

| cohort | gate OOS | base OOS | edge | verdict | picks |
|--------|----------|----------|------|---------|-------|
"""
for r in rows:
    degen_flag = " ⚠️ degenerate" if r["degen"] else ""
    doc += f"| {r['cohort']} | ${r['gate']:+,.0f} | ${r['base']:+,.0f} | ${r['delta']:+,.0f} | **{r['verdict']}** | {r['picks']}{degen_flag} |\n"

doc += f"""
---

## Findings

**POSITIVE cohorts ({len(positives)}):** {', '.join(r['cohort'] for r in positives) or 'none'}

**NEGATIVE cohorts ({len(negatives)}):** {', '.join(r['cohort'] for r in negatives) or 'none'}

**Degenerate (gate == baseline):** {', '.join(r['cohort'] for r in degenerates) or 'none'}

A degenerate result means the gate's FLAT carve-outs never fire differently from always-short
for that config — typically fast-EMA + long max-hold (few, long trades that span regimes).
Interpret as: gate has no effect, not as NEGATIVE evidence of harm.

---

## Caveats

1. **SHORT_ONLY ≠ original live verdict.** The original BTC POSITIVE (+$700k, full gate) was
   ~50% LONG P&L (decomposition: short +$531k, long +$529k; 2023 was 66% long). This sweep
   removes that LONG leg entirely. A NEGATIVE short-only result for a cohort that was POSITIVE
   full-gate is *expected* — it means the edge was in the LONG flip, not the sit-out.

2. **Gross of switching costs.** Each regime boundary incurs a force-close + re-entry. Not
   modeled as an extra cost here (episode slicing force-closes at tick price, which includes
   slippage, but there's no explicit "cross the spread twice on a regime switch" cost).

3. **Episode-length confound.** SHORT_ONLY removes LONG episodes, biasing toward shorter hold
   times during SHORT episodes. Configs with tight max-hold (alt5-15-336, 336h) naturally
   produce more frequent entries/exits within a SHORT episode; configs with loose max-hold
   (504h) may see fewer trades per episode.

4. **Research-only.** No live/shadow/VPS changes. Per locked decision rule: any deployment
   decision requires a separate productionization discussion.
"""

with open(outfile, 'w') as fh:
    fh.write(doc)
print(f"→ Wrote {outfile}")
PYEOF
