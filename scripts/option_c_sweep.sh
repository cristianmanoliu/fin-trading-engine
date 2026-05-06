#!/usr/bin/env bash
# option_c_sweep.sh — sweeps target_rr for Option C (EMA9/EMA21 crossover, 4H bias filter).
# Runs all symbols in parallel — one worker per symbol, capped at logical CPU count.
#
# Usage:
#   ./scripts/option_c_sweep.sh
#   ./scripts/option_c_sweep.sh "BTCUSDT ETHUSDT" 2020 2025 04
set -euo pipefail

SYMBOLS="${1:-BTCUSDT ETHUSDT BNBUSDT SOLUSDT XRPUSDT LINKUSDT LTCUSDT DOGEUSDT ADAUSDT AVAXUSDT ATOMUSDT TRXUSDT}"
START_YEAR="${2:-2020}"
END_YEAR="${3:-2025}"
END_YEAR_MONTH="${4:-04}"
RR_VALUES="1.0 1.5 2.0 2.5 3.0 3.5 4.0 5.0"

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BINARY=$(mktemp /tmp/sweep-bin.XXXXXXXX)
RESULTS=$(mktemp /tmp/sweep-results.XXXXXXXX)
trap 'rm -f "$BINARY" "$RESULTS"' EXIT

echo "→ Compiling..."
(cd "$ROOT" && go build -o "$BINARY" ./cmd/backtest)

# Sed expression that enables Option C and disables A and B.
SED_EXTRA="s|momentum_mode:.*|momentum_mode: false|; s|vwap_deviation_mode:.*|vwap_deviation_mode: false|; s|ema_mode:.*|ema_mode: true|"

echo "symbol,year,month,target_rr,trades,wins,total_usd" > "$RESULTS"

export SYMBOLS START_YEAR END_YEAR END_YEAR_MONTH RR_VALUES SED_EXTRA ROOT BINARY RESULTS
bash "$(dirname "$0")/sweep_parallel.sh"

# ── Aggregate and display ─────────────────────────────────────────────────────
awk -F, \
    -v symbols="$SYMBOLS" \
    -v rrs="$RR_VALUES" \
    -v start_year="$START_YEAR" \
    -v end_year="$END_YEAR" \
'
NR > 1 {
    sym=$1; yr=$2+0; rr=$4
    t=$5+0; w=$6+0; usd=$7+0
    rr_t[rr]+=t; rr_w[rr]+=w; rr_usd[rr]+=usd
    rrsy_usd[rr,sym,yr]+=usd
}

function kfmt(v) { return sprintf("%+.0fk", v/1000) }

END {
    n_syms = split(symbols, sym_arr, " ")
    n_rrs  = split(rrs,     rr_arr,  " ")
    n_years = 0
    for (y = start_year+0; y <= end_year+0; y++) yr_arr[++n_years] = y

    sep = ""; for(i=0;i<100;i++) sep=sep"═"
    printf "\n%s\n  OPTION C — EMA9×EMA21 CROSSOVER  target_rr sweep  ($1k stake, %d symbols, %d–%d)\n%s\n\n", \
        sep, n_syms, start_year, end_year, sep

    printf "%-9s  %8s  %5s  %7s  %10s  %9s  %s\n", \
        "target_rr", "trades", "win%", "breakevn", "total_$", "avg_$/sym", "syms_profitable"
    printf "%-9s  %8s  %5s  %7s  %10s  %9s\n", \
        "─────────","────────","─────","────────","──────────","─────────"

    for (ri=1;ri<=n_rrs;ri++) {
        rr=rr_arr[ri]
        t=rr_t[rr]+0; w=rr_w[rr]+0; u=rr_usd[rr]+0
        if (t==0) { printf "%-9s  (no trades)\n", rr; continue }

        beven = 1/(1+rr+0) * 100
        pos_syms=0
        sym_detail=""
        for (si=1;si<=n_syms;si++) {
            sym=sym_arr[si]
            sym_usd=0
            for (yi=1;yi<=n_years;yi++) sym_usd+=rrsy_usd[rr,sym,yr_arr[yi]]+0
            if (sym_usd > 0) pos_syms++
            sym_detail=sym_detail sprintf(" %s:%s", sym, kfmt(sym_usd))
        }

        flag=""
        if (u > 0) flag=" ✓"
        printf "%-9s  %8d  %4.1f%%  %6.1f%%  %+10.0f  %+9.0f%s  | %d/%d syms |%s\n", \
            rr, t, w/t*100, beven, u, u/n_syms, flag, pos_syms, n_syms, sym_detail
    }
    printf "\n%s\n", sep
}
' "$RESULTS"
