#!/usr/bin/env bash
# pdh_sidecount.sh — was PDH/PDL actually contaminated? Count LONG opens pre vs
# post fix on the full 57-sym universe under --side-filter short.
set -uo pipefail
shopt -s nullglob
ROOT="$(cd "$(dirname "$0")/.." && pwd)"; cd "$ROOT"
source scripts/lib/symbols.sh
WORK=$(mktemp -d /tmp/pdh_sc.XXXX); trap 'rm -rf "$WORK"' EXIT
go build -o "$WORK/post" ./cmd/backtest
git worktree add -q --detach "$WORK/wt" "d1d0fae^"
( cd "$WORK/wt" && go build -o "$WORK/pre" ./cmd/backtest )
git worktree remove --force "$WORK/wt"
FLAGS="--signal-tf 4H --side-filter short --max-hold-hours 504 --pdh-pdl-break-mode --funding-csv-dir $ROOT/data/funding --fee-bps 10 --stop-slippage-bps 5 --exact-fills --include-boundary --pessimistic-ambiguous"
run() {
  local bin="$1" jd="$2"; mkdir -p "$jd"
  for s in $(get_symbols universe); do
    local m="$WORK/$s.csv"
    if [[ ! -f "$m" ]]; then
      local fs=( data/${s}-1m-20[0-9][0-9]-*.csv )
      [[ ${#fs[@]} -gt 0 ]] && cat "${fs[@]}" >> "$m"
    fi
    [[ -s "$m" ]] || continue
    local c="$WORK/c-$s.yaml"
    sed "s|momentum_mode:.*|momentum_mode: false|;s|ema_mode:.*|ema_mode: true|;s|target_rr:.*|target_rr: 6.0|;s|csv_path:.*|csv_path: $m|" configs/default.yaml > "$c"
    "$bin" --config "$c" --symbol "$s" $FLAGS --journal-dir "$jd" >/dev/null 2>&1 || true
  done
}
sides() {
  python3 - "$1" <<'PY'
import json,glob,sys,os
jd=sys.argv[1]; ol=os_=0
for fp in glob.glob(os.path.join(jd,"*.jsonl")):
    for l in open(fp):
        l=l.strip()
        if not l: continue
        try: e=json.loads(l)
        except: continue
        if e.get("event")=="open":
            sd=(e.get("side") or "").upper()
            ol+= sd=="LONG"; os_+= sd=="SHORT"
print(f"LONG={ol} SHORT={os_}")
PY
}
run "$WORK/pre"  "$WORK/jpre"
run "$WORK/post" "$WORK/jpost"
echo "PRE-FIX  PDH/PDL (57 sym): $(sides "$WORK/jpre")"
echo "POST-FIX PDH/PDL (57 sym): $(sides "$WORK/jpost")"
