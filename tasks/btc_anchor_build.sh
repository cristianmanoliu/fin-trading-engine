#!/usr/bin/env bash
# btc_anchor_build.sh — aggregate local BTCUSDT 1m CSVs into a daily OHLC anchor
# series for the regime-gate harness. PRIMARY path: pure-local aggregation
# (BTC 1m IS present in the symlinked data store). No network. Idempotent:
# overwrites the output each run.
#
# Output: data/anchor/BTCUSDT-1d.csv  with header: date,open,high,low,close
#   date = YYYY-MM-DD (UTC).  Daily open = first 1m open of the day,
#   high/low = day extremes, close = last 1m close of the day.
#
# Research-only. Not a registered edge claim.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

OUTDIR="$ROOT/data/anchor"
OUT="$OUTDIR/BTCUSDT-1d.csv"
mkdir -p "$OUTDIR"

# Collect BTC 1m CSVs (symlinks). `find` deref via cat works on symlinks.
mapfile -t FILES < <(find "$ROOT/data" -maxdepth 1 -name 'BTCUSDT-1m-*.csv' | sort)
if [[ ${#FILES[@]} -eq 0 ]]; then
  echo "ERROR: no BTCUSDT-1m-*.csv found in data/ — cannot build anchor" >&2
  exit 1
fi
echo "→ Aggregating ${#FILES[@]} BTC 1m files → daily OHLC" >&2

# awk: skip header rows (col1==open_time); bucket by UTC day from open_time ms;
# track day open (first seen), high (max), low (min), close (last seen).
# NOTE: array named `closep` not `close` — `close` is a reserved built-in in
# BSD/macOS awk and cannot be used as an array name.
cat "${FILES[@]}" | awk -F, '
  $1 == "open_time" || $1 == "Open time" { next }
  $1 == "" { next }
  {
    day_ms = int($1 / 86400000) * 86400000
    o=$2+0; h=$3+0; l=$4+0; c=$5+0
    if (!(day_ms in seen)) { seen[day_ms]=1; open[day_ms]=o; high[day_ms]=h; low[day_ms]=l }
    if (h > high[day_ms]) high[day_ms]=h
    if (l < low[day_ms])  low[day_ms]=l
    closep[day_ms]=c
  }
  END {
    n=0
    for (d in seen) { days[n++]=d }
    for (i=0;i<n;i++) for (j=i+1;j<n;j++) if (days[j]<days[i]) { t=days[i]; days[i]=days[j]; days[j]=t }
    for (i=0;i<n;i++) {
      d=days[i]
      printf "%d,%.2f,%.2f,%.2f,%.2f\n", d, open[d], high[d], low[d], closep[d]
    }
  }
' > "$OUT.raw"

# Convert epoch-ms day to YYYY-MM-DD (UTC) using python (portable; macOS date is fussy).
{
  echo "date,open,high,low,close"
  python3 -c '
import sys, datetime
for line in sys.stdin:
    parts = line.strip().split(",")
    if not parts or not parts[0]:
        continue
    ms = int(parts[0])
    d = datetime.datetime.utcfromtimestamp(ms/1000).strftime("%Y-%m-%d")
    print(",".join([d] + parts[1:]))
' < "$OUT.raw"
} > "$OUT"
rm -f "$OUT.raw"

rows=$(($(wc -l < "$OUT") - 1))
echo "→ Wrote $OUT ($rows daily rows)" >&2
echo "  range: $(sed -n '2p' "$OUT" | cut -d, -f1) → $(tail -1 "$OUT" | cut -d, -f1)" >&2
