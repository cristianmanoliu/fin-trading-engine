#!/usr/bin/env bash
# breadth_anchor_build.sh — build a daily equal-weight alt-breadth index from
# the 16 deployed non-BTC symbols. For each UTC day, average the daily close
# return across symbols that have data, then compound into a cumulative index
# (base=100). Output is the same format as btc_anchor_build.sh so regime_label.py
# can consume it unchanged.
#
# Output: data/anchor/BREADTH-1d.csv  with header: date,open,high,low,close
#   close = cumulative breadth index value (base=100 at first day).
#   open=high=low=close per row (labeler uses close only).
#
# Research-only. Not a registered edge claim.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

OUTDIR="$ROOT/data/anchor"
OUT="$OUTDIR/BREADTH-1d.csv"
mkdir -p "$OUTDIR"

# Deployed-16 symbols (non-BTC, from configs/symbols.yaml).
SYMBOLS=(
  ROSEUSDT BCHUSDT GRTUSDT 1INCHUSDT ADAUSDT KAVAUSDT
  1000SHIBUSDT ENSUSDT XLMUSDT IMXUSDT ETCUSDT RUNEUSDT
  AVAXUSDT APTUSDT DOTUSDT FILUSDT
)

echo "→ Building breadth index from ${#SYMBOLS[@]} deployed symbols" >&2

# Step 1: extract daily close for each symbol → tmp dir
TMPDIR_LOCAL="$(mktemp -d)"
trap 'rm -rf "$TMPDIR_LOCAL"' EXIT

for sym in "${SYMBOLS[@]}"; do
  mapfile -t FILES < <(find "$ROOT/data" -maxdepth 1 -name "${sym}-1m-*.csv" | sort)
  if [[ ${#FILES[@]} -eq 0 ]]; then
    echo "  WARN: no 1m CSVs for $sym — skipping" >&2
    continue
  fi
  out_sym="$TMPDIR_LOCAL/${sym}.csv"
  # Aggregate 1m → daily close (last close of each UTC day).
  cat "${FILES[@]}" | awk -F, -v sym="$sym" '
    $1 == "open_time" || $1 == "Open time" { next }
    $1 == "" { next }
    {
      day_ms = int($1 / 86400000) * 86400000
      closep[day_ms] = $5 + 0
    }
    END {
      n = 0
      for (d in closep) { days[n++] = d }
      for (i=0;i<n;i++) for (j=i+1;j<n;j++) if (days[j]<days[i]) { t=days[i]; days[i]=days[j]; days[j]=t }
      for (i=0;i<n;i++) printf "%d,%.8f\n", days[i], closep[days[i]]
    }
  ' > "$out_sym"
  echo "  $sym: $(wc -l < "$out_sym") daily rows" >&2
done

# Step 2: merge all symbol daily closes into a single breadth index via Python.
# Python handles the cross-day alignment and cumulative compounding cleanly.
python3 - "$TMPDIR_LOCAL" "$OUT" <<'PYEOF'
import sys, os, csv, datetime
from collections import defaultdict

tmpdir, outpath = sys.argv[1], sys.argv[2]

# Load all per-symbol daily closes: { day_ms -> { sym -> close } }
sym_data = {}
for fname in os.listdir(tmpdir):
    if not fname.endswith(".csv"):
        continue
    sym = fname[:-4]
    closes = {}
    with open(os.path.join(tmpdir, fname)) as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            parts = line.split(",")
            closes[int(parts[0])] = float(parts[1])
    sym_data[sym] = closes

# Collect all days across all symbols.
all_days = sorted(set(d for closes in sym_data.values() for d in closes))

if not all_days:
    print("ERROR: no data found", file=sys.stderr)
    sys.exit(1)

# For each day: compute equal-weight average daily return across symbols
# that have data on both day[i] and day[i-1].
# prev_day: the most recent prior day in the global day list (may skip weekends for sparse symbols).
index_val = 100.0
rows = []
prev_closes = {}  # sym -> close on previous day in global list

for day_ms in all_days:
    returns = []
    curr_closes = {}
    for sym, closes in sym_data.items():
        c = closes.get(day_ms)
        if c is None:
            continue
        curr_closes[sym] = c
        p = prev_closes.get(sym)
        if p and p > 0:
            returns.append((c - p) / p)
    # Update prev_closes with all symbols that had data today.
    prev_closes.update(curr_closes)

    if returns:
        avg_ret = sum(returns) / len(returns)
        index_val *= (1.0 + avg_ret)

    date_str = datetime.datetime.fromtimestamp(day_ms / 1000, datetime.timezone.utc).strftime("%Y-%m-%d")
    rows.append((date_str, index_val))

with open(outpath, "w", newline="") as f:
    w = csv.writer(f)
    w.writerow(["date", "open", "high", "low", "close"])
    for date_str, val in rows:
        w.writerow([date_str, f"{val:.6f}", f"{val:.6f}", f"{val:.6f}", f"{val:.6f}"])

print(f"→ Wrote {outpath} ({len(rows)} daily rows)", file=sys.stderr)
print(f"  range: {rows[0][0]} → {rows[-1][0]}", file=sys.stderr)
print(f"  index: {rows[0][1]:.2f} → {rows[-1][1]:.6f}", file=sys.stderr)
PYEOF
