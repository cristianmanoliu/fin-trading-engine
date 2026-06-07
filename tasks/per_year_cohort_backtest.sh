#!/usr/bin/env bash
# per_year_cohort_backtest.sh — DESCRIPTIVE per-year NET PnL for each live/shadow
# cohort, on the DEPLOYED-16 universe, using ONE consistent harness so every
# number is comparable to the others and to the forward-paper $ figures.
#
# NOT a registered sweep, NOT an edge claim. Purely answers "what would each of
# the 8 deployed algos have made on the 16 live coins, year by year
# (2020 → 2026-04, the full extent of local data)?"
# Costs match live: fee=10bp, slip=5bp, funding=historical CSV.
# 2026 is Jan–Apr only and stops before the forward-paper window (~2026-05-05).
#
# Methodology notes:
#   - Per-symbol continuous run within a year (cat that year's monthly CSVs ->
#     one merged file -> one Stub instance), per the no-month-segmentation
#     invariant. Year boundaries DO force-close open positions at year-end last
#     price; this is a deliberate per-year bucketing, applied identically to all
#     cohorts, so it cannot bias the cross-cohort comparison.
#   - Deployed-16 re-injects selection look-ahead vs the 57-sym registered
#     matrix. Fine for a descriptive table; do NOT cite for an edge claim.
#
# Output: tasks/per_year_cohort_backtest.csv  (cohort,year,net_pnl,trades,wins)
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

BINARY="$ROOT/bin/backtest"
DATA_DIR="$ROOT/data"
FUNDING_DIR="$ROOT/data/funding"
BASE_CFG="$ROOT/configs/default.yaml"
# OUT is set after UNIVERSE is resolved (below) so the two runs don't collide.

# Ensure binary is current
echo "→ Rebuilding backtest binary..." >&2
go build -o "$BINARY" ./cmd/backtest

# Symbol set is selectable via UNIVERSE env var:
#   UNIVERSE=deployed (default) -> the live deployed-16 (selection look-ahead)
#   UNIVERSE=full               -> all 57 in symbols.yaml `universe:` group
#                                  (NO selection look-ahead — the honest
#                                  "edge must hold on every instrument" test)
UNIVERSE="${UNIVERSE:-deployed}"
if [[ "$UNIVERSE" == "full" ]]; then
  # Parse the universe: block from symbols.yaml (single source of truth).
  mapfile -t DEPLOYED < <(
    awk '/^universe:/{f=1;next} /^[a-z_]+:/{f=0} f && /^  - /{print $2}' \
      "$ROOT/configs/symbols.yaml"
  )
else
  DEPLOYED=(ROSEUSDT BCHUSDT GRTUSDT 1INCHUSDT ADAUSDT KAVAUSDT 1000SHIBUSDT \
            ENSUSDT XLMUSDT IMXUSDT ETCUSDT RUNEUSDT AVAXUSDT APTUSDT DOTUSDT FILUSDT)
fi
SIDE="${SIDE:-short}"   # direction filter: short (default) | long | both
echo "→ UNIVERSE=${UNIVERSE}: ${#DEPLOYED[@]} symbols | SIDE=${SIDE}" >&2

# Universe+side-suffixed output so different runs don't overwrite each other.
uni_tag="deployed"; [[ "$UNIVERSE" == "full" ]] && uni_tag="full57"
if [[ "$UNIVERSE" == "deployed" && "$SIDE" == "short" ]]; then
  OUT="$ROOT/tasks/per_year_cohort_backtest.csv"            # original (back-compat)
elif [[ "$UNIVERSE" == "full" && "$SIDE" == "short" ]]; then
  OUT="$ROOT/tasks/per_year_cohort_backtest_full57.csv"     # original (back-compat)
else
  OUT="$ROOT/tasks/per_year_cohort_backtest_${uni_tag}_${SIDE}.csv"
fi

YEARS=(2020 2021 2022 2023 2024 2025 2026)
# Full coverage: local CSVs run through 2026-04 for every deployed symbol.
# 2026 is partial (Jan–Apr). Boundary at 2026-04 deliberately stops BEFORE the
# forward-paper window (live started ~2026-05-05) to respect the locked
# no-backtest-over-forward-paper convention. Missing months are simply skipped
# per-symbol, so over-specifying [2026]=12 is harmless (May–Dec just don't exist).
declare -A END_MONTH=( [2020]=12 [2021]=12 [2022]=12 [2023]=12 [2024]=12 [2025]=12 [2026]=4 )

# cohort_label -> extra flags (everything beyond the shared base flags)
declare -A COHORT_FLAGS=(
  ["live"]="--ema-fast-period 9 --ema-slow-period 21 --max-hold-hours 504"
  ["alt5-15-336"]="--ema-fast-period 5 --ema-slow-period 15 --max-hold-hours 336"
  ["alt5-15-504"]="--ema-fast-period 5 --ema-slow-period 15 --max-hold-hours 504"
  ["alt5-21-504"]="--ema-fast-period 5 --ema-slow-period 21 --max-hold-hours 504"
  ["alt7-14-504"]="--ema-fast-period 7 --ema-slow-period 14 --max-hold-hours 504"
  ["alt10-30-504"]="--ema-fast-period 10 --ema-slow-period 30 --max-hold-hours 504"
  ["alt12-26-504"]="--ema-fast-period 12 --ema-slow-period 26 --max-hold-hours 504"
  ["alt21-50-504"]="--ema-fast-period 21 --ema-slow-period 50 --max-hold-hours 504"
  ["bb20"]="--bollinger-mode --bollinger-period 20 --bollinger-std-mult 2.0 --max-hold-hours 504"
)
# Stable ordering for output
COHORT_ORDER=(live alt5-15-336 alt5-15-504 alt5-21-504 alt7-14-504 \
              alt10-30-504 alt12-26-504 alt21-50-504 bb20)

# Shared flags — match live execution model exactly.
# NOTE: target_rr (6.0) and stake_usd (1000) are config-only (no CLI flag);
# they come from configs/default.yaml, which already pins both.
# SIDE (resolved above) selects direction: short (live default) | long | both.
# Lets us test the inverse hypothesis — is the edge pure short-alt beta, or symmetric?
BASE_FLAGS="--signal-tf 4H --side-filter ${SIDE} \
            --fee-bps 10 --stop-slippage-bps 5 \
            --exact-fills --include-boundary"

WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT

echo "cohort,year,net_pnl,trades,wins" > "$OUT"

for cohort in "${COHORT_ORDER[@]}"; do
  extra="${COHORT_FLAGS[$cohort]}"
  for year in "${YEARS[@]}"; do
    last="${END_MONTH[$year]}"
    year_net=0; year_trades=0; year_wins=0
    for sym in "${DEPLOYED[@]}"; do
      merged="$WORKDIR/${sym}-${year}.csv"
      : > "$merged"
      found=0
      for (( m=1; m<=last; m++ )); do
        mm=$(printf '%02d' "$m")
        csv="$DATA_DIR/${sym}-1m-${year}-${mm}.csv"
        [[ -f "$csv" ]] && { cat "$csv" >> "$merged"; found=1; }
      done
      [[ $found -eq 0 ]] && { rm -f "$merged"; continue; }

      cfg="$WORKDIR/cfg-${sym}.yaml"
      sed "s|csv_path:.*|csv_path: ${merged}|; \
           s|ema_mode:.*|ema_mode: true|; \
           s|^symbol:.*|symbol: ${sym}|" "$BASE_CFG" > "$cfg"

      # SUMMARY line is JSON; pull total_pnl_usd, total_trades, wins.
      # Capture into a var first (|| true so a no-SUMMARY / zero-trade run — e.g.
      # a symbol's listing month with too little data — doesn't trip set -e via
      # an empty `read`). Default each field to 0.
      summary="$(
        "$BINARY" --config "$cfg" $BASE_FLAGS $extra \
          --funding-csv-dir "$FUNDING_DIR" 2>&1 \
        | jq -r 'select(.msg | test("SUMMARY")) | "\(.total_pnl_usd // 0) \(.total_trades // 0) \(.wins // 0)"' \
        | tail -1
      )" || true
      read -r net tr wn <<< "${summary:-0 0 0}"
      net="${net:-0}"; tr="${tr:-0}"; wn="${wn:-0}"
      year_net=$(awk "BEGIN{printf \"%.2f\", $year_net + $net}")
      year_trades=$(( year_trades + tr ))
      year_wins=$(( year_wins + wn ))
      rm -f "$merged" "$cfg"
    done
    printf '%s,%s,%.2f,%d,%d\n' "$cohort" "$year" "$year_net" "$year_trades" "$year_wins" >> "$OUT"
    echo "  ${cohort} ${year}: net=\$${year_net} trades=${year_trades} wins=${year_wins}" >&2
  done
done

echo "→ Done. Wrote $OUT" >&2
