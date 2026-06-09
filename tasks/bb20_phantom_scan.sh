#!/usr/bin/env bash
# bb20_phantom_scan.sh — quantify the bb20 "exit>=entry TARGET" fill artifact
# across all 57 symbols for the 2020-2021 bull years. A short labeled TARGET with
# exit >= entry is phantom profit (price rose against the short but it booked a
# win). Reports per-symbol total vs phantom, and the portfolio sum.
# Read-only. Research diagnostic.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"; cd "$ROOT"

if [[ "${1:-}" == "--worker" ]]; then
  sym="$2"; WD="$3"
  M="$WD/m.$sym"; : > "$M"; found=0
  for y in 2020 2021; do for m in 01 02 03 04 05 06 07 08 09 10 11 12; do
    f="data/${sym}-1m-${y}-${m}.csv"
    [[ -f "$f" ]] && { awk -F, '$1=="open_time"||$1==""{next}{print}' "$f" >> "$M"; found=1; }
  done; done
  [[ $found -eq 0 ]] && { rm -f "$M"; exit 0; }
  CFG="$WD/c.$sym"
  sed "s|csv_path:.*|csv_path: $M|; s|ema_mode:.*|ema_mode: true|; s|^symbol:.*|symbol: $sym|" \
      configs/default.yaml > "$CFG"
  JD="$WD/j.$sym"; mkdir -p "$JD"
  bin/backtest --config "$CFG" --signal-tf 4H --fee-bps 10 --stop-slippage-bps 5 \
    --exact-fills --include-boundary --max-hold-hours 504 \
    --bollinger-mode --bollinger-period 20 --bollinger-std-mult 2.0 --side-filter short \
    --funding-csv-dir data/funding --journal-dir "$JD" >/dev/null 2>&1 || true
  python3 - "$sym" "$JD" <<'PY'
import json, glob, sys
sym=sys.argv[1]; jd=sys.argv[2]
tot=0.0; phantom=0.0; n=0
for f in glob.glob(jd+"/*.jsonl"):
    for ln in open(f):
        ln=ln.strip()
        if not ln: continue
        try: e=json.loads(ln)
        except: continue
        if e.get("event")=="close":
            tot+=e.get("pnl_usd",0)
            if e.get("outcome")=="TARGET" and e.get("exit",0)>=e.get("entry",0):
                phantom+=e.get("pnl_usd",0); n+=1
print(f"{sym} {tot:.0f} {phantom:.0f} {n}")
PY
  rm -f "$M" "$CFG"; rm -rf "$JD"
  exit 0
fi

echo "→ Building binary..." >&2
go build -o bin/backtest ./cmd/backtest
mapfile -t SYMS < <(awk '/^universe:/{f=1;next} /^[a-z_]+:/{f=0} f && /^  - /{print $2}' configs/symbols.yaml)
echo "→ ${#SYMS[@]} symbols" >&2
WD="$(mktemp -d)"; trap 'rm -rf "$WD"' EXIT
SELF="$ROOT/tasks/bb20_phantom_scan.sh"
printf '%s\n' "${SYMS[@]}" | xargs -P "${JOBS:-10}" -L1 -I{} bash "$SELF" --worker {} "$WD" > "$WD/out.txt"
echo "sym total_pnl phantom_pnl phantom_n"
sort -k3 -n "$WD/out.txt"
echo "----------------------------------------------------------"
awk '{t+=$2; p+=$3; n+=$4} END{
  printf "PORTFOLIO 2020-21 (bull): total=$%d  phantom=$%d  phantom_trades=%d\n", t, p, n;
  printf "bb20 bull WITHOUT the phantom fills ≈ $%d\n", t-p
}' "$WD/out.txt"
