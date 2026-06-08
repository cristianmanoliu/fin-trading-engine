#!/usr/bin/env bash
# regime_switch_baseline.sh — continuous always-short baseline for the switch
# test. One bin/backtest per symbol over the full concatenated 2020-2026 1m
# history (NO regime episodes), side=short. Annual P&L bucketed by year. This is
# the deployed-strategy control the switch must beat on Crit 2 (no back-to-back
# red). X,Y-independent → written once, copied to every episodes_X*_Y*_baseline.csv.
#
# Usage: bash tasks/regime_switch_baseline.sh
#   env: RESDIR, JOBS, EXTRA_BASE_FLAGS (per-cohort EMA/max-hold)
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"; cd "$ROOT"

if [[ "${1:-}" == "--worker" ]]; then
  shift; sym="$1"
  # Concatenate all monthly CSVs for this symbol, 2020-01..2026-12, in order.
  merged="$(mktemp "$RG_WORKDIR/b.${sym}.XXXXXXXX")"
  found=0
  for y in 2020 2021 2022 2023 2024 2025 2026; do
    for m in 01 02 03 04 05 06 07 08 09 10 11 12; do
      csv="$RG_DATA_DIR/${sym}-1m-${y}-${m}.csv"
      [[ -f "$csv" ]] && { awk -F, '$1=="open_time"||$1==""{next}{print}' "$csv" >> "$merged"; found=1; }
    done
  done
  if [[ $found -eq 0 || ! -s "$merged" ]]; then rm -f "$merged"; exit 0; fi
  cfg="$(mktemp "$RG_WORKDIR/bcfg.${sym}.XXXXXXXX")"
  sed "s|csv_path:.*|csv_path: ${merged}|; s|ema_mode:.*|ema_mode: true|; s|^symbol:.*|symbol: ${sym}|" \
      "$RG_BASE_CFG" > "$cfg"
  jdir="$(mktemp -d "$RG_WORKDIR/bj.${sym}.XXXXXXXX")"
  "$RG_BINARY" --config "$cfg" $RG_BASE_FLAGS --side-filter short \
    --funding-csv-dir "$RG_FUNDING_DIR" --journal-dir "$jdir" >/dev/null 2>&1 || true
  # Bucket close-event pnl_usd by close-year.
  python3 - "$sym" "$jdir" <<'PY'
import sys, os, json, glob
sym = sys.argv[1]; jdir = sys.argv[2]
buckets = {}
for f in glob.glob(os.path.join(jdir, "*.jsonl")):
    with open(f) as fh:
        for ln in fh:
            ln = ln.strip()
            if not ln: continue
            try: e = json.loads(ln)
            except: continue
            if e.get("event") == "close":
                yr = e.get("ts", "")[:4]
                buckets[yr] = buckets.get(yr, 0.0) + e.get("pnl_usd", 0.0)
for yr, pnl in sorted(buckets.items()):
    print(f"{sym},{yr},short,{pnl:.2f},0,0")
PY
  rm -rf "$merged" "$cfg" "$jdir"
  exit 0
fi

BINARY="$ROOT/bin/backtest"; DATA_DIR="$ROOT/data"; FUNDING_DIR="$ROOT/data/funding"
BASE_CFG="$ROOT/configs/default.yaml"
RESDIR="${RESDIR:-$ROOT/tasks/regime_switch_results}"; mkdir -p "$RESDIR"
JOBS="${JOBS:-12}"

echo "→ Building binary..." >&2
go build -o "$BINARY" ./cmd/backtest
mapfile -t SYMS < <(awk '/^universe:/{f=1;next} /^[a-z_]+:/{f=0} f && /^  - /{print $2}' "$ROOT/configs/symbols.yaml")

WORKDIR="$(mktemp -d)"; trap 'rm -rf "$WORKDIR"' EXIT
BASE_FLAGS="--signal-tf 4H --fee-bps 10 --stop-slippage-bps 5 \
            --exact-fills --include-boundary --max-hold-hours 504"
[[ -n "${EXTRA_BASE_FLAGS:-}" ]] && BASE_FLAGS="$BASE_FLAGS $EXTRA_BASE_FLAGS"
export RG_WORKDIR="$WORKDIR" RG_DATA_DIR="$DATA_DIR" RG_BASE_CFG="$BASE_CFG" \
       RG_BINARY="$BINARY" RG_FUNDING_DIR="$FUNDING_DIR" RG_BASE_FLAGS="$BASE_FLAGS"

BASELINE_BODY="$RESDIR/_baseline_body.csv"
printf '%s\n' "${SYMS[@]}" | xargs -P "$JOBS" -L 1 bash "$ROOT/tasks/regime_switch_baseline.sh" --worker \
  > "$BASELINE_BODY" || true
echo "→ baseline rows: $(wc -l < "$BASELINE_BODY")" >&2
