#!/usr/bin/env bash
set -euo pipefail

# Run all instruments using their per-symbol configs and show a year-by-year
# PnL heatmap. Unlike cross_analysis.sh (which sweeps min_rr), this script
# uses each symbol's already-decided min_rr — it's a validation / live-readiness check.
#
# Usage:
#   ./scripts/validate_all.sh
#   ./scripts/validate_all.sh "BTCUSDT ETHUSDT SOLUSDT" 2021 2024 12

SYMBOLS="${1:-BTCUSDT ETHUSDT BNBUSDT SOLUSDT XRPUSDT LINKUSDT LTCUSDT DOGEUSDT}"
START_YEAR="${2:-2020}"
END_YEAR="${3:-2025}"
END_YEAR_MONTH="${4:-04}"

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
RESULTS=$(mktemp /tmp/validate-results.XXXXXXXX)
BINARY=$(mktemp /tmp/backtest-bin.XXXXXXXX)
trap 'rm -f "$RESULTS" "$BINARY"' EXIT

# ── Phase 1: compile once ────────────────────────────────────────────────────
echo "→ Compiling..."
(cd "$ROOT" && go build -o "$BINARY" ./cmd/backtest)

# ── Phase 2: download missing months ─────────────────────────────────────────
mkdir -p "${ROOT}/data"
for symbol in $SYMBOLS; do
  echo "→ Checking data for ${symbol}..."
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

# ── Phase 3: run backtests ────────────────────────────────────────────────────
n_syms=$(echo "$SYMBOLS" | wc -w | tr -d ' ')
total_months=0
for (( year = START_YEAR; year <= END_YEAR; year++ )); do
  last=$(( year == END_YEAR ? 10#$END_YEAR_MONTH : 12 ))
  total_months=$(( total_months + last ))
done
total_runs=$(( n_syms * total_months ))

echo ""
echo "→ Running ${n_syms} symbols × ${total_months} months = ${total_runs} backtests..."

echo "symbol,year,month,trades,wins,total_pnl,avg_win,avg_loss,total_usd" > "$RESULTS"

# Build symbol→min_rr map for display (reads from each config file).
SYM_RR_MAP=""
for symbol in $SYMBOLS; do
  sym_lower=$(echo "$symbol" | tr '[:upper:]' '[:lower:]')
  cfg_file="${ROOT}/configs/${sym_lower}.yaml"
  [[ ! -f "$cfg_file" ]] && cfg_file="${ROOT}/configs/default.yaml"
  rr=$(grep 'min_rr:' "$cfg_file" | head -1 | awk '{print $2}')
  SYM_RR_MAP="${SYM_RR_MAP}${symbol}:${rr},"
done

run=0
for symbol in $SYMBOLS; do
  sym_lower=$(echo "$symbol" | tr '[:upper:]' '[:lower:]')
  cfg_file="${ROOT}/configs/${sym_lower}.yaml"
  [[ ! -f "$cfg_file" ]] && cfg_file="${ROOT}/configs/default.yaml"

  for (( year = START_YEAR; year <= END_YEAR; year++ )); do
    last=$(( year == END_YEAR ? 10#$END_YEAR_MONTH : 12 ))
    for (( m = 1; m <= last; m++ )); do
      mm=$(printf '%02d' "$m")
      csv="${ROOT}/data/${symbol}-1m-${year}-${mm}.csv"

      run=$(( run + 1 ))
      pct=$(( run * 100 / total_runs ))
      filled=$(( pct * 40 / 100 ))
      bar=$(printf '%0.s#' $(seq 1 $filled) 2>/dev/null || true)
      printf "\r  [%-40s] %3d%% (%d/%d)  %s  %d-%s" \
        "$bar" "$pct" "$run" "$total_runs" "$symbol" "$year" "$mm" >&2

      if [[ ! -f "$csv" ]]; then
        echo "${symbol},${year},${mm},0,0,0,0,0,0" >> "$RESULTS"
        continue
      fi

      cfg=$(mktemp /tmp/val-cfg.XXXXXXXX)
      sed "s|csv_path:.*|csv_path: ${csv}|" "$cfg_file" > "$cfg"

      summary=$("$BINARY" --config "$cfg" 2>&1 \
        | jq -r 'select(.msg | test("SUMMARY")) | "\(.total_trades) \(.wins) \(.total_pnl_pts) \(.avg_win_pts) \(.avg_loss_pts) \(.total_pnl_usd // 0)"' \
        2>/dev/null || true)
      rm -f "$cfg"

      if [[ -n "$summary" ]]; then
        read -r T W P AW AL PUSD <<< "$summary"
        echo "${symbol},${year},${mm},${T},${W},${P},${AW},${AL},${PUSD}" >> "$RESULTS"
      else
        echo "${symbol},${year},${mm},0,0,0,0,0,0" >> "$RESULTS"
      fi
    done
  done
done
printf "\n\n" >&2

# ── Phase 4: aggregate and display ───────────────────────────────────────────
awk -F, \
  -v symbols="$SYMBOLS" \
  -v rr_map="$SYM_RR_MAP" \
  -v start_year="$START_YEAR" \
  -v end_year="$END_YEAR" \
'
NR > 1 {
  sym=$1; yr=$2+0
  t=$4+0; w=$5+0; usd=$9+0; l=t-w

  sy_t[sym,yr]+=t; sy_w[sym,yr]+=w; sy_usd[sym,yr]+=usd
  s_t[sym]+=t;     s_w[sym]+=w;     s_usd[sym]+=usd
  y_usd[yr]+=usd
  total_usd+=usd;  total_t+=t;  total_w+=w
}

function kfmt(v) { return sprintf("%+.0fk", v/1000) }

END {
  n_syms = split(symbols, sym_arr, " ")
  n_years = 0
  for (y = start_year+0; y <= end_year+0; y++) yr_arr[++n_years] = y

  # Parse rr_map: "BTCUSDT:1.5,ETHUSDT:1.5,..."
  n_pairs = split(rr_map, pairs, ",")
  for (i=1; i<=n_pairs; i++) {
    n = split(pairs[i], kv, ":")
    if (n == 2) rr[kv[1]] = kv[2]
  }

  sep = ""; for(i=0;i<105;i++) sep=sep"═"

  printf "\n%s\n", sep
  printf "  VALIDATION — per-symbol min_rr  |  $1,000 stake/trade  |  %d–%d\n", start_year, end_year
  printf "%s\n\n", sep

  # header
  printf "%-20s", "symbol (min_rr)"
  for (i=1;i<=n_years;i++) printf "  %6d", yr_arr[i]
  printf "  %9s  %7s  %5s\n", "TOTAL_$", "trades", "win%"

  printf "%-20s", "────────────────────"
  for (i=1;i<=n_years;i++) printf "  %6s", "──────"
  printf "  %9s  %7s  %5s\n", "─────────", "───────", "─────"

  for (si=1;si<=n_syms;si++) {
    sym=sym_arr[si]
    label = sym " (" rr[sym] ")"
    printf "%-20s", label
    profitable=0
    for (i=1;i<=n_years;i++) {
      yr=yr_arr[i]; usd=sy_usd[sym,yr]+0
      if (usd > 0) profitable++
      printf "  %6s", kfmt(usd)
    }
    t=s_t[sym]+0; w=s_w[sym]+0; u=s_usd[sym]+0
    win_pct=(t>0)?w/t*100:0
    printf "  %+9.0f  %7d  %4.1f%%\n", u, t, win_pct
  }

  printf "%-20s", "────────────────────"
  for (i=1;i<=n_years;i++) printf "  %6s", "──────"
  printf "  %9s  %7s  %5s\n", "─────────", "───────", "─────"

  printf "%-20s", "COMBINED"
  for (i=1;i<=n_years;i++) printf "  %6s", kfmt(y_usd[yr_arr[i]]+0)
  total_win_pct=(total_t>0)?total_w/total_t*100:0
  printf "  %+9.0f  %7d  %4.1f%%\n", total_usd, total_t, total_win_pct

  printf "\n%s\n", sep
  printf "  TOTAL:  %+.0f   |   avg/symbol:  %+.0f   |   %d symbols\n", \
    total_usd, total_usd/n_syms, n_syms
  printf "%s\n\n", sep
}
' "$RESULTS"
