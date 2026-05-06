#!/usr/bin/env bash
set -euo pipefail

# Cross-instrument analysis: finds the min_rr that maximises combined total_$1k
# while being profitable in all years on every tested symbol.
#
# Usage:
#   ./scripts/cross_analysis.sh
#   ./scripts/cross_analysis.sh "BTCUSDT ETHUSDT SOLUSDT BNBUSDT" 2020 2025 04
#   ./scripts/cross_analysis.sh "BTCUSDT ETHUSDT" 2021 2024 12 "0 1 2 3"

SYMBOLS="${1:-BTCUSDT ETHUSDT SOLUSDT BNBUSDT}"
START_YEAR="${2:-2020}"
END_YEAR="${3:-2025}"
END_YEAR_MONTH="${4:-04}"
RR_VALUES="${5:-0 0.5 1.0 1.5 2.0 2.5 3.0}"

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
COMBINED=$(mktemp /tmp/cross-results.XXXXXXXX)
BINARY=$(mktemp /tmp/backtest-bin.XXXXXXXX)
trap 'rm -f "$COMBINED" "$BINARY"' EXIT

# ── Phase 1: compile once ────────────────────────────────────────────────────
echo "→ Compiling..."
(cd "$ROOT" && go build -o "$BINARY" ./cmd/backtest)

# ── Phase 2: download missing months for all symbols ─────────────────────────
mkdir -p "${ROOT}/data"
for symbol in $SYMBOLS; do
  echo "→ Checking data for ${symbol} ${START_YEAR}–${END_YEAR}..."
  for (( year = START_YEAR; year <= END_YEAR; year++ )); do
    last=$(( year == END_YEAR ? 10#$END_YEAR_MONTH : 12 ))
    for (( m = 1; m <= last; m++ )); do
      mm=$(printf '%02d' "$m")
      csv="${ROOT}/data/${symbol}-1m-${year}-${mm}.csv"
      if [[ ! -f "$csv" ]]; then
        url="https://data.binance.vision/data/futures/um/monthly/klines/${symbol}/1m/${symbol}-1m-${year}-${mm}.zip"
        zipfile="${ROOT}/data/${symbol}-1m-${year}-${mm}.zip"
        if curl -f -# -L "$url" -o "$zipfile" 2>/dev/null && unzip -t "$zipfile" >/dev/null 2>&1; then
          unzip -o "$zipfile" -d "${ROOT}/data" >/dev/null
          rm "$zipfile"
        else
          rm -f "$zipfile"
        fi
      fi
    done
  done
done

# ── Phase 3: run all (symbol × month × min_rr) combinations ─────────────────
echo "symbol,year,month,min_rr,trades,wins,total_pnl,avg_win,avg_loss,total_usd" > "$COMBINED"

n_syms=$(echo "$SYMBOLS" | wc -w | tr -d ' ')
n_rrs=$(echo "$RR_VALUES" | wc -w | tr -d ' ')
total_months=0
for (( year = START_YEAR; year <= END_YEAR; year++ )); do
  last=$(( year == END_YEAR ? 10#$END_YEAR_MONTH : 12 ))
  total_months=$(( total_months + last ))
done
total_runs=$(( n_syms * total_months * n_rrs ))

echo ""
echo "→ Running ${n_syms} symbols × ${total_months} months × ${n_rrs} min_rr values = ${total_runs} backtests..."
run=0

for symbol in $SYMBOLS; do
  for rr in $RR_VALUES; do
    for (( year = START_YEAR; year <= END_YEAR; year++ )); do
      last=$(( year == END_YEAR ? 10#$END_YEAR_MONTH : 12 ))
      for (( m = 1; m <= last; m++ )); do
        mm=$(printf '%02d' "$m")
        csv="${ROOT}/data/${symbol}-1m-${year}-${mm}.csv"

        run=$(( run + 1 ))
        pct=$(( run * 100 / total_runs ))
        filled=$(( pct * 40 / 100 ))
        bar=$(printf '%0.s#' $(seq 1 $filled) 2>/dev/null || true)
        printf "\r  [%-40s] %3d%% (%d/%d)  %s rr=%-4s  %d-%s" \
          "$bar" "$pct" "$run" "$total_runs" "$symbol" "$rr" "$year" "$mm" >&2

        if [[ ! -f "$csv" ]]; then
          echo "${symbol},${year},${mm},${rr},0,0,0,0,0,0" >> "$COMBINED"
          continue
        fi

        cfg=$(mktemp /tmp/cross-cfg.XXXXXXXX)
        CONFIGS_TO_CLEAN+=("$cfg")
        sed "s|csv_path:.*|csv_path: ${csv}|; s|min_rr:.*|min_rr: ${rr}|" \
          "${ROOT}/configs/default.yaml" > "$cfg"

        summary=$("$BINARY" --config "$cfg" 2>&1 \
          | jq -r 'select(.msg | test("SUMMARY")) | "\(.total_trades) \(.wins) \(.total_pnl_pts) \(.avg_win_pts) \(.avg_loss_pts) \(.total_pnl_usd // 0)"' \
          2>/dev/null || true)

        if [[ -n "$summary" ]]; then
          read -r T W P AW AL PUSD <<< "$summary"
          echo "${symbol},${year},${mm},${rr},${T},${W},${P},${AW},${AL},${PUSD}" >> "$COMBINED"
        else
          echo "${symbol},${year},${mm},${rr},0,0,0,0,0,0" >> "$COMBINED"
        fi
      done
    done
  done
done
printf "\n\n" >&2

# ── Phase 4: aggregate and display ───────────────────────────────────────────
awk -F, \
  -v rrs="$RR_VALUES" \
  -v symbols="$SYMBOLS" \
  -v start_year="$START_YEAR" \
  -v end_year="$END_YEAR" \
'
NR > 1 {
  sym=$1; yr=$2+0; rr=$4
  t=$5+0; w=$6+0; p=$7+0; aw=$8+0; al=$9+0; usd=$10+0; l=t-w

  sry_t[sym,rr,yr]+=t
  sry_p[sym,rr,yr]+=p

  sr_t[sym,rr]+=t; sr_w[sym,rr]+=w; sr_p[sym,rr]+=p; sr_usd[sym,rr]+=usd
  sr_ws[sym,rr]+=aw*w; sr_ls[sym,rr]+=al*l

  r_t[rr]+=t; r_w[rr]+=w; r_p[rr]+=p; r_usd[rr]+=usd
  r_ws[rr]+=aw*w; r_ls[rr]+=al*l
}

function pct_str(v) { return (v >= 0) ? sprintf("+%.0f", v) : sprintf("%.0f", v) }

END {
  n_rrs  = split(rrs, rr_arr, " ")
  n_syms = split(symbols, sym_arr, " ")
  n_years = 0
  for (y = start_year+0; y <= end_year+0; y++) yr_arr[++n_years] = y

  sep = ""; for(i=0;i<95;i++) sep=sep"═"

  # ── Per-symbol breakdown at each min_rr ─────────────────────────────────
  printf "\n%s\n  PER-SYMBOL BREAKDOWN\n%s\n\n", sep, sep

  for (ri=1;ri<=n_rrs;ri++) {
    rr=rr_arr[ri]
    printf "  min_rr = %s\n", rr
    printf "  %-10s  %7s  %5s  %10s  %6s\n", "symbol","trades","win%","total_$1k","yrs+"
    for (si=1;si<=n_syms;si++) {
      sym=sym_arr[si]
      t=sr_t[sym,rr]+0; w=sr_w[sym,rr]+0; u=sr_usd[sym,rr]+0
      if(t==0) { printf "  %-10s  %7s  %5s  %10s  %6s\n", sym,"—","—","—","—"; continue }
      ypos=0
      for(yi=1;yi<=n_years;yi++){
        yr=yr_arr[yi]; yt=sry_t[sym,rr,yr]+0
        if(yt>0 && sry_p[sym,rr,yr]/yt>0) ypos++
      }
      printf "  %-10s  %7d  %4.1f%%  %+10.0f  %d/%d\n", \
        sym, t, w/t*100, u, ypos, n_years
    }
    printf "\n"
  }

  # ── Cross-instrument aggregate table ───────────────────────────────────
  printf "%s\n  CROSS-INSTRUMENT AGGREGATE\n%s\n\n", sep, sep

  printf "%-7s  %8s  %5s  %10s  %9s  %7s  %5s  %s\n", \
    "min_rr","trades","win%","total_$1k","avg_$/sym","W/L","syms+","(all years profitable per symbol)"
  printf "%-7s  %8s  %5s  %10s  %9s  %7s  %5s\n", \
    "───────","────────","─────","──────────","─────────","─────","─────"

  best_rr=""; best_usd=-999999

  for (ri=1;ri<=n_rrs;ri++) {
    rr=rr_arr[ri]
    t=r_t[rr]+0; w=r_w[rr]+0; p=r_p[rr]+0; u=r_usd[rr]+0
    if(t==0) { printf "%-7s  —\n", rr; continue }
    l=t-w
    avg_w=(w>0)?r_ws[rr]/w:0
    avg_l=(l>0)?r_ls[rr]/l:0
    wl=(avg_l>0)?avg_w/avg_l:0

    all_ok=0
    sym_detail=""
    for (si=1;si<=n_syms;si++) {
      sym=sym_arr[si]
      ypos=0
      for(yi=1;yi<=n_years;yi++){
        yr=yr_arr[yi]; yt=sry_t[sym,rr,yr]+0
        if(yt>0 && sry_p[sym,rr,yr]/yt>0) ypos++
      }
      if(ypos==n_years) all_ok++
      sym_detail=sym_detail sprintf(" %s:%d/%d", sym, ypos, n_years)
    }

    tag=""
    if(all_ok==n_syms) tag=" ◀"
    if(all_ok==n_syms && u>best_usd) { best_usd=u; best_rr=rr }

    printf "%-7s  %8d  %4.1f%%  %+10.0f  %+9.0f  %5.2f  %d/%d%s  %s\n", \
      rr, t, w/t*100, u, u/n_syms, wl, all_ok, n_syms, tag, sym_detail
  }

  printf "\n%s\n", sep
  if(best_rr != "")
    printf "  RECOMMENDATION:  min_rr = %s   (highest combined_$1k, all %d symbols profitable every year)\n", best_rr, n_syms
  else
    printf "  RECOMMENDATION:  no single min_rr profitable every year across all %d symbols — review breakdown\n", n_syms
  printf "%s\n\n", sep
}
' "$COMBINED"
