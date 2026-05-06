#!/usr/bin/env bash
# paper_live_compare.sh — side-by-side comparison of live strategy vs each shadow.
#
# Reads journal files for live + each shadow strategy from the VPS (or local logs/journal).
# Produces a table comparing trade counts, WR, NET PnL, average hold time, top symbols.
#
# Becomes useful after ~14 days of trade flow (to have enough trades per strategy
# for meaningful comparison). With <50 trades per strategy, treat results as
# anecdotal — not decision-grade.
#
# Usage:
#   ./scripts/paper_live_compare.sh                   # default VPS
#   ./scripts/paper_live_compare.sh root@host         # alt VPS
#   ./scripts/paper_live_compare.sh local             # use ./logs/journal locally
set -euo pipefail

VPS="${1:-root@178.105.24.230}"
JOURNAL_DIR_DEFAULT="/var/log/paper-live/journal"
JOURNAL_DIR="${JOURNAL_DIR:-$JOURNAL_DIR_DEFAULT}"

# --- runner ---
remote_or_local() {
    if [[ "$VPS" == "local" ]]; then
        bash -c "JOURNAL_DIR='$JOURNAL_DIR' bash -s" <<<"$1"
    else
        ssh "$VPS" "JOURNAL_DIR='$JOURNAL_DIR_DEFAULT' bash -s" <<<"$1"
    fi
}

# Aggregate journal data per strategy. Outputs:
#   STRATEGY|<label>|<first_ts>|<last_ts>|<trades>|<wins>|<targets>|<stops>|<partials>|<net_pnl>|<avg_pnl>
#   SYMBOL|<label>|<symbol>|<sym_pnl>|<sym_trades>
DATA=$(remote_or_local '
set -euo pipefail
shopt -s nullglob

aggregate() {
    local label="$1"; shift
    local files=("$@")
    if [[ ${#files[@]} -eq 0 ]]; then
        echo "STRATEGY|${label}|NODATA"
        return
    fi
    cat "${files[@]}" | jq -r '"'"'select(.event=="close") | [.ts, .symbol, (.pnl_usd // 0), (.outcome // "STOP")] | @tsv'"'"' 2>/dev/null \
    | awk -F"\t" -v label="$label" '"'"'
        BEGIN { first_ts=""; last_ts=""; total=0; wins=0; targets=0; stops=0; partials=0; pnl=0 }
        {
            if (first_ts=="") first_ts=$1
            last_ts=$1
            total++
            outcome=$4
            if (outcome=="TARGET") { wins++; targets++ }
            else if (outcome=="PARTIAL") { wins++; partials++ }
            else stops++
            pnl += $3
            sym_pnl[$2] += $3
            sym_count[$2]++
        }
        END {
            if (total==0) { printf "STRATEGY|%s|NODATA\n", label; exit }
            avg = pnl/total
            printf "STRATEGY|%s|%s|%s|%d|%d|%d|%d|%d|%.2f|%.2f\n", label, first_ts, last_ts, total, wins, targets, stops, partials, pnl, avg
            for (s in sym_pnl) printf "SYMBOL|%s|%s|%.2f|%d\n", label, s, sym_pnl[s], sym_count[s]
        }'"'"'
}

# Live: top-level *.jsonl
live_files=( "${JOURNAL_DIR}"/*-*.jsonl )
aggregate "live" "${live_files[@]}"

# Shadows: shadow/*/
if [[ -d "${JOURNAL_DIR}/shadow" ]]; then
    for label_dir in "${JOURNAL_DIR}/shadow"/*/; do
        [[ -d "$label_dir" ]] || continue
        label_name=$(basename "$label_dir")
        shadow_files=( "${label_dir}"*-*.jsonl )
        aggregate "shadow/${label_name}" "${shadow_files[@]}"
    done
fi
')

# --- pretty output ---
SEP="$(printf '%0.s═' {1..96})"
ROW="$(printf '%0.s─' {1..96})"

echo
echo "$SEP"
echo "  Live vs Shadow comparison — $(date -u '+%Y-%m-%d %H:%M UTC')"
echo "  Source: $VPS  ($JOURNAL_DIR)"
echo "$SEP"

# Table header
printf "\n  %-30s %7s %5s %5s %8s %8s %12s %10s\n" \
    "Strategy" "Trades" "Wins" "WR%" "Targets" "Stops" "NET PnL" "Avg/trade"
printf "  %s\n" "${ROW:0:88}"

# Iterate strategy lines
total_strategies=0
total_with_data=0
echo "$DATA" | grep "^STRATEGY|" | while IFS='|' read -r _ label first_ts last_ts trades wins targets stops partials pnl avg; do
    total_strategies=$((total_strategies + 1))
    if [[ "$first_ts" == "NODATA" ]]; then
        printf "  %-30s %7s %5s %5s %8s %8s %12s %10s\n" "$label" "—" "—" "—" "—" "—" "(no data)" "—"
        continue
    fi
    total_with_data=$((total_with_data + 1))

    if [[ "$trades" -gt 0 ]]; then
        wr=$(awk "BEGIN{printf \"%.1f\", $wins*100/$trades}")
    else
        wr="0.0"
    fi
    pnl_int=$(awk -v p="$pnl" 'BEGIN{printf "%+d", p}')
    avg_int=$(awk -v a="$avg" 'BEGIN{printf "%+d", a}')

    extra=""
    if [[ "$partials" -gt 0 ]]; then
        extra=" (+${partials} partial)"
    fi
    printf "  %-30s %7d %5d %4s%% %8d %8d %12s %10s%s\n" "$label" "$trades" "$wins" "$wr" "$targets" "$stops" "\$$pnl_int" "\$$avg_int" "$extra"
done

# Reference window from any-strategy data
first_strategy_data=$(echo "$DATA" | grep "^STRATEGY|" | grep -v "NODATA" | head -1)
if [[ -n "$first_strategy_data" ]]; then
    ref_first=$(echo "$first_strategy_data" | cut -d'|' -f3)
    ref_last=$(echo "$first_strategy_data" | cut -d'|' -f4)
    echo
    printf "  Trade window:        %s → %s\n" "${ref_first:0:19}" "${ref_last:0:19}"
fi

# Diagnostic: trade counts per strategy (low-power warning)
echo
warn_low=0
echo "$DATA" | grep "^STRATEGY|" | grep -v "NODATA" | while IFS='|' read -r _ label _ _ trades _ _ _ _ _ _; do
    if [[ "$trades" -lt 50 ]]; then
        echo "  ⚠  $label has only $trades trades — comparison is anecdotal until ≥50 closes per strategy"
    fi
done

# Per-strategy top 3 symbols
echo
printf "  %-30s %s\n" "Strategy" "Top 3 symbols (by PnL)"
printf "  %s\n" "${ROW:0:88}"
echo "$DATA" | grep "^STRATEGY|" | grep -v "NODATA" | cut -d'|' -f2 | while read -r label; do
    top3=$(echo "$DATA" | awk -F'|' -v lbl="$label" '$1=="SYMBOL" && $2==lbl { printf "%s|%.0f|%d\n", $3, $4, $5 }' | sort -t'|' -k2 -gr | head -3)
    if [[ -z "$top3" ]]; then
        top3_fmt="(no symbols)"
    else
        top3_fmt=$(echo "$top3" | awk -F'|' '{ printf "%s%s@%+d ($%dT) ", (NR>1?", ":""), $1, $2, $3 }')
    fi
    printf "  %-30s %s\n" "$label" "$top3_fmt"
done

echo
echo "$SEP"
echo "  Use forward_paper_status.sh for go/no-go criteria evaluation per strategy."
echo "  This script shows side-by-side comparison only; criteria checking is separate."
echo "$SEP"
echo
