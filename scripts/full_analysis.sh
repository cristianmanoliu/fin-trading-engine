#!/usr/bin/env bash
set -euo pipefail

# Multi-year, multi-min_rr backtest analysis.
# Downloads data, runs all combinations, prints a heatmap + summary table.
#
# Usage:
#   ./scripts/full_analysis.sh                              → BTCUSDT 2020–2025
#   ./scripts/full_analysis.sh BTCUSDT 2020 2025 04        → custom range
#   ./scripts/full_analysis.sh BTCUSDT 2020 2025 04 "0 1 2 3"  → custom rr values

SYMBOL="${1:-BTCUSDT}"
START_YEAR="${2:-2020}"
END_YEAR="${3:-2025}"
END_YEAR_MONTH="${4:-04}"
RR_VALUES="${5:-0 0.5 1.0 1.5 2.0 2.5 3.0}"

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
RESULTS=$(mktemp /tmp/analysis-results.XXXXXXXX)
BINARY=$(mktemp /tmp/backtest-bin.XXXXXXXX)
trap 'rm -f "$RESULTS" "$BINARY"' EXIT

# ── Phase 1: compile once ─────────────────────────────────────────────────────
echo "→ Compiling..."
(cd "$ROOT" && go build -o "$BINARY" ./cmd/backtest)

# ── Phase 2: download missing months ─────────────────────────────────────────
mkdir -p "${ROOT}/data"
echo "→ Checking data for ${SYMBOL} ${START_YEAR}–${END_YEAR}..."

for (( year = START_YEAR; year <= END_YEAR; year++ )); do
  last=$(( year == END_YEAR ? 10#$END_YEAR_MONTH : 12 ))
  for (( m = 1; m <= last; m++ )); do
    mm=$(printf '%02d' "$m")
    csv="${ROOT}/data/${SYMBOL}-1m-${year}-${mm}.csv"
    if [[ ! -f "$csv" ]]; then
      url="https://data.binance.vision/data/futures/um/monthly/klines/${SYMBOL}/1m/${SYMBOL}-1m-${year}-${mm}.zip"
      zipfile="${ROOT}/data/${SYMBOL}-1m-${year}-${mm}.zip"
      echo "  Downloading ${SYMBOL}-1m-${year}-${mm}..."
      if curl -f -# -L "$url" -o "$zipfile" 2>/dev/null && unzip -t "$zipfile" >/dev/null 2>&1; then
        unzip -o "$zipfile" -d "${ROOT}/data"
        rm "$zipfile"
      else
        echo "  No data for ${SYMBOL}-1m-${year}-${mm}, skipping"
        rm -f "$zipfile"
      fi
    fi
  done
done

# ── Phase 3: run all (month × min_rr) combinations ───────────────────────────
echo "year,month,min_rr,trades,wins,total_pnl,avg_win,avg_loss" > "$RESULTS"

total_months=0
for (( year = START_YEAR; year <= END_YEAR; year++ )); do
  last=$(( year == END_YEAR ? 10#$END_YEAR_MONTH : 12 ))
  total_months=$(( total_months + last ))
done
n_rrs=$(echo "$RR_VALUES" | wc -w | tr -d ' ')
total_runs=$(( total_months * n_rrs ))

echo "→ Running ${n_rrs} min_rr values × ${total_months} months = ${total_runs} backtests..."
run=0

for rr in $RR_VALUES; do
  for (( year = START_YEAR; year <= END_YEAR; year++ )); do
    last=$(( year == END_YEAR ? 10#$END_YEAR_MONTH : 12 ))
    for (( m = 1; m <= last; m++ )); do
      mm=$(printf '%02d' "$m")
      csv="${ROOT}/data/${SYMBOL}-1m-${year}-${mm}.csv"

      cfg=$(mktemp /tmp/backtest-cfg.XXXXXXXX)
      sed "s|csv_path:.*|csv_path: ${csv}|; s|min_rr:.*|min_rr: ${rr}|" \
        "${ROOT}/configs/default.yaml" > "$cfg"

      summary=$("$BINARY" --config "$cfg" 2>&1 \
        | jq -r 'select(.msg | test("SUMMARY")) | "\(.total_trades) \(.wins) \(.total_pnl_pts) \(.avg_win_pts) \(.avg_loss_pts) \(.total_pnl_usd // 0) \(.expectancy_usd // 0)"' \
        2>/dev/null || true)

      rm -f "$cfg"
      run=$(( run + 1 ))
      pct=$(( run * 100 / total_runs ))
      filled=$(( pct * 40 / 100 ))
      bar=$(printf '%0.s#' $(seq 1 $filled) 2>/dev/null || true)
      printf "\r  [%-40s] %3d%% (%d/%d)  rr=%-4s  %d-%s" "$bar" "$pct" "$run" "$total_runs" "$rr" "$year" "$mm" >&2

      if [[ -n "$summary" ]]; then
        read -r T W P AW AL PUSD EUSD <<< "$summary"
        echo "${year},${mm},${rr},${T},${W},${P},${AW},${AL},${PUSD}" >> "$RESULTS"
      else
        echo "${year},${mm},${rr},0,0,0,0,0,0" >> "$RESULTS"
      fi
    done
  done
done
printf "\n\n" >&2

# ── Phase 4: aggregate + display ─────────────────────────────────────────────
awk -F, \
  -v rrs="$RR_VALUES" \
  -v start_year="$(( START_YEAR ))" \
  -v end_year="$(( END_YEAR ))" \
  -v symbol="$SYMBOL" \
'
NR > 1 {
  yr = $1+0; rr = $3
  t=$4+0; w=$5+0; p=$6+0; aw=$7+0; al=$8+0; usd=$9+0; l=t-w

  ry_t[rr,yr]+=t;  ry_w[rr,yr]+=w;  ry_p[rr,yr]+=p;  ry_usd[rr,yr]+=usd
  ry_ws[rr,yr]+=aw*w; ry_ls[rr,yr]+=al*l

  r_t[rr]+=t; r_w[rr]+=w; r_p[rr]+=p; r_usd[rr]+=usd
  r_ws[rr]+=aw*w; r_ls[rr]+=al*l
}

function fmt(v,    s) {
  s = sprintf("%.0f", v)
  return (v >= 0) ? "+" s : s
}

END {
  n_rrs = split(rrs, rr_arr, " ")
  n_years = 0
  for (y = start_year; y <= end_year; y++) yr_arr[++n_years] = y

  # separator
  sep = ""
  for (i=0;i<80;i++) sep=sep"═"

  # ── Heatmap ──────────────────────────────────────────────────────────────
  printf "\n%s\n", sep
  printf "  EXPECTANCY / TRADE (pts)    %s  %d – %d\n", symbol, start_year, end_year
  printf "%s\n\n", sep

  # header row
  printf "%-7s", "min_rr"
  for (i=1;i<=n_years;i++) printf "  %6d", yr_arr[i]
  printf "  %8s  %5s\n", "OVERALL", "yrs+"
  # divider
  printf "%-7s", "───────"
  for (i=1;i<=n_years;i++) printf "  ──────"
  printf "  ────────  ─────\n"

  for (ri=1;ri<=n_rrs;ri++) {
    rr=rr_arr[ri]
    printf "%-7s", rr
    ypos=0
    for (i=1;i<=n_years;i++) {
      yr=yr_arr[i]; t=ry_t[rr,yr]+0
      xp=(t>0)?ry_p[rr,yr]/t:0
      if(xp>0) ypos++
      printf "  %6s", fmt(xp)
    }
    tt=r_t[rr]+0
    ov=(tt>0)?r_p[rr]/tt:0
    printf "  %8s  %d/%d\n", fmt(ov), ypos, n_years
  }

  # ── Summary table ─────────────────────────────────────────────────────────
  printf "\n%s\n", sep
  printf "  OVERALL SUMMARY  (%d years combined)\n", n_years
  printf "%s\n\n", sep

  printf "%-7s  %7s  %5s  %9s  %10s  %10s  %7s  %7s  %5s  %5s\n", \
    "min_rr","trades","win%","exp/trade","total_pts","total_$1k","avg_win","avg_loss","W/L","yrs+"
  printf "%-7s  %7s  %5s  %9s  %10s  %10s  %7s  %7s  %5s  %5s\n", \
    "───────","───────","─────","─────────","──────────","──────────","───────","───────","─────","─────"

  best_rr=""; best_usd=-999999; best_xp=-999999; any_usd=0

  for (ri=1;ri<=n_rrs;ri++) {
    rr=rr_arr[ri]
    t=r_t[rr]+0; w=r_w[rr]+0; p=r_p[rr]+0; u=r_usd[rr]+0
    if(t==0) continue
    if(u!=0) any_usd=1
    l=t-w
    win_pct=w/t*100; xp=p/t
    avg_w=(w>0)?r_ws[rr]/w:0
    avg_l=(l>0)?r_ls[rr]/l:0
    wl=(avg_l>0)?avg_w/avg_l:0

    ypos=0
    for(i=1;i<=n_years;i++){
      yr=yr_arr[i]; yt=ry_t[rr,yr]+0
      if(yt>0 && ry_p[rr,yr]/yt>0) ypos++
    }

    tag=""
    if(ypos==n_years) tag=" ◀"
    if(ypos==n_years) {
      if(any_usd ? u>best_usd : xp>best_xp) {
        best_usd=u; best_xp=xp; best_rr=rr
      }
    }

    printf "%-7s  %7d  %4.1f%%  %+9.1f  %+10.1f  %+10.2f  %7.0f  %7.0f  %5.2f  %d/%d%s\n", \
      rr, t, win_pct, xp, p, u, avg_w, avg_l, wl, ypos, n_years, tag
  }

  printf "\n%s\n", sep
  if(best_rr != "") {
    if(any_usd)
      printf "  RECOMMENDATION:  min_rr = %s   (highest total_$1k, profitable in all %d years)\n", best_rr, n_years
    else
      printf "  RECOMMENDATION:  min_rr = %s   (best expectancy, profitable in all %d years)\n", best_rr, n_years
  } else
    printf "  RECOMMENDATION:  review heatmap — no single setting profitable in all years\n"
  printf "%s\n\n", sep
}
' "$RESULTS"
