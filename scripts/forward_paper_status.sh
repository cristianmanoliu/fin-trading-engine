#!/usr/bin/env bash
# forward_paper_status.sh — check live + shadow strategies against the go/no-go
# criteria from CLAUDE.md ## Forward-paper go/no-go criteria.
#
# This is the strategic-decision-grade checker. For local-process liveness see
# scripts/paper_live_status.sh.
#
# Usage:
#   ./scripts/forward_paper_status.sh                       # uses default VPS
#   ./scripts/forward_paper_status.sh root@host             # alt VPS
#   JOURNAL_DIR=./logs/journal ./scripts/forward_paper_status.sh local
#                                                            # local journals
#
# Per strategy (live + each shadow), prints:
#   - days elapsed since first trade
#   - trade count, wins, WR
#   - total NET PnL
#   - top symbol contributors + concentration check
#   - PASS/FAIL/IN-PROGRESS per criterion
#   - overall verdict: WAITING / DEPLOY-READY / KILL
set -euo pipefail

# --- args ---
VPS="${1:-root@178.105.24.230}"
JOURNAL_DIR_DEFAULT="/var/log/paper-live/journal"
JOURNAL_DIR="${JOURNAL_DIR:-$JOURNAL_DIR_DEFAULT}"

# --- criteria from CLAUDE.md (deploy gate) ---
MIN_TRADES=150       # ≥150 live trades accumulated
MIN_DAYS=60          # ≥60 calendar days
MIN_WR_PCT=14        # Realized WR ≥ 14% (breakeven ≈ 14.3% at 6:1 RR)
MAX_SYM_PCT=40       # No single symbol > 40% of cumulative PnL

# --- kill criteria ---
KILL_MAX_SLIP_BP=25  # Slip > 25bp is the cliff edge (model not yet wired into journal)

# --- runner ---
remote_or_local() {
    if [[ "$VPS" == "local" ]]; then
        bash -c "JOURNAL_DIR='$JOURNAL_DIR' bash -s" <<<"$1"
    else
        ssh "$VPS" "JOURNAL_DIR='$JOURNAL_DIR_DEFAULT' bash -s" <<<"$1"
    fi
}

# Aggregate journal data on the (remote or local) host. Outputs:
#   STRATEGY|<label>|<first_ts>|<last_ts>|<trades>|<wins>|<net_pnl>
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
    # Concatenate close events (regular + partial) and aggregate via awk.
    cat "${files[@]}" | jq -r '"'"'select(.event=="close") | [.ts, .symbol, (.pnl_usd // 0), (.outcome // "STOP")] | @tsv'"'"' 2>/dev/null \
    | awk -F"\t" -v label="$label" '"'"'
        BEGIN { first_ts=""; last_ts=""; total=0; wins=0; pnl=0 }
        {
            if (first_ts=="") first_ts=$1
            last_ts=$1
            total++
            if ($4=="TARGET" || $4=="PARTIAL") wins++
            pnl += $3
            sym_pnl[$2] += $3
            sym_count[$2]++
        }
        END {
            if (total==0) { printf "STRATEGY|%s|NODATA\n", label; exit }
            printf "STRATEGY|%s|%s|%s|%d|%d|%.2f\n", label, first_ts, last_ts, total, wins, pnl
            for (s in sym_pnl) printf "SYMBOL|%s|%s|%.2f|%d\n", label, s, sym_pnl[s], sym_count[s]
        }'"'"'
}

# Live: top-level *.jsonl
live_files=( "${JOURNAL_DIR}"/*-*.jsonl )
aggregate "live" "${live_files[@]}"

# Shadows: scan shadow/*/  for any per-symbol files
if [[ -d "${JOURNAL_DIR}/shadow" ]]; then
    for label_dir in "${JOURNAL_DIR}/shadow"/*/; do
        [[ -d "$label_dir" ]] || continue
        label_name=$(basename "$label_dir")
        shadow_files=( "${label_dir}"*-*.jsonl )
        aggregate "shadow/${label_name}" "${shadow_files[@]}"
    done
fi
')

# --- process locally ---
if [[ -z "$DATA" ]]; then
    echo "No data returned from $VPS — check connectivity and JOURNAL_DIR."
    exit 1
fi

NOW=$(date -u +%s)

# Pretty header
SEP="$(printf '%0.s═' {1..78})"
echo
echo "$SEP"
echo "  Forward-paper go/no-go status — $(date -u '+%Y-%m-%d %H:%M UTC')"
echo "  Source: $VPS  ($JOURNAL_DIR)"
echo "$SEP"

# Iterate strategy lines
echo "$DATA" | grep "^STRATEGY|" | while IFS='|' read -r _ label first_ts last_ts trades wins pnl; do
    if [[ "$first_ts" == "NODATA" ]]; then
        printf "\n  %-26s  (no data yet)\n" "$label"
        continue
    fi

    # Days elapsed (UTC)
    first_epoch=$(date -j -f "%Y-%m-%dT%H:%M:%SZ" "${first_ts%%.*}Z" +%s 2>/dev/null || \
                  date -d "${first_ts}" +%s 2>/dev/null || echo "$NOW")
    days_elapsed=$(( (NOW - first_epoch) / 86400 ))

    # Win rate
    if [[ "$trades" -gt 0 ]]; then
        wr_pct=$(awk "BEGIN{printf \"%.1f\", $wins*100/$trades}")
    else
        wr_pct="0.0"
    fi

    # Per-symbol concentration: max %
    sym_lines=$(echo "$DATA" | awk -F'|' -v lbl="$label" '$1=="SYMBOL" && $2==lbl {print}')
    abs_pnl=$(awk -v p="$pnl" 'BEGIN{p<0?p=-p:0; print p}')
    if [[ "$abs_pnl" != "0" ]] && [[ -n "$sym_lines" ]]; then
        max_sym_pct=$(echo "$sym_lines" | awk -F'|' -v total="$abs_pnl" '
            {
                p=$4; if (p<0) p=-p
                pct=p/total*100
                if (pct > max) { max=pct; sym=$3 }
            }
            END { printf "%.1f|%s", max+0, sym }')
        max_sym_pct_val=$(echo "$max_sym_pct" | cut -d'|' -f1)
        max_sym_name=$(echo "$max_sym_pct" | cut -d'|' -f2)
    else
        max_sym_pct_val="0.0"
        max_sym_name="—"
    fi

    # Top 3 symbols by abs PnL
    top_syms=$(echo "$sym_lines" | awk -F'|' '{print $3, $4}' | sort -k2 -gr | head -3 | \
        awk '{printf "%s%s%s%+d  ", (NR>1?", ":""), $1, "@", $2}' || true)

    # Verdict per criterion
    pass_or_inprogress() {
        local val="$1" thr="$2" mode="$3"  # mode: ge|le
        if [[ "$mode" == "ge" ]]; then
            if (( $(awk "BEGIN{print ($val >= $thr)}") )); then echo "PASS"
            else echo "IN-PROGRESS"; fi
        else
            if (( $(awk "BEGIN{print ($val <= $thr)}") )); then echo "PASS"
            else echo "FAIL"; fi
        fi
    }
    pass_or_fail() {
        local val="$1" thr="$2" mode="$3"
        if [[ "$mode" == "ge" ]]; then
            if (( $(awk "BEGIN{print ($val >= $thr)}") )); then echo "PASS"; else echo "FAIL"; fi
        else
            if (( $(awk "BEGIN{print ($val <= $thr)}") )); then echo "PASS"; else echo "FAIL"; fi
        fi
    }

    s_trades=$(pass_or_inprogress "$trades" "$MIN_TRADES" ge)
    s_days=$(pass_or_inprogress "$days_elapsed" "$MIN_DAYS" ge)
    s_wr=$([[ "$trades" -ge "$MIN_TRADES" ]] && pass_or_fail "$wr_pct" "$MIN_WR_PCT" ge || echo "PENDING")
    s_pnl=$(pass_or_fail "$pnl" "0" ge)
    s_sym=$(pass_or_fail "$max_sym_pct_val" "$MAX_SYM_PCT" le)

    # Overall verdict
    if [[ "$days_elapsed" -lt "$MIN_DAYS" ]] || [[ "$trades" -lt "$MIN_TRADES" ]]; then
        overall="WAITING (insufficient data)"
    elif [[ "$s_pnl" == "FAIL" ]] || [[ "$s_wr" == "FAIL" ]] || [[ "$s_sym" == "FAIL" ]]; then
        overall="KILL — at least one criterion failed"
    elif [[ "$s_pnl" == "PASS" ]] && [[ "$s_wr" == "PASS" ]] && [[ "$s_sym" == "PASS" ]]; then
        overall="DEPLOY-READY — all criteria met"
    else
        overall="WAITING"
    fi

    pnl_int=$(awk -v p="$pnl" 'BEGIN{printf "%+d", p}')

    printf "\n  ── %s ─%s\n" "$label" "$(printf '%0.s─' $(seq 1 $((70 - ${#label}))))"
    printf "    Days elapsed:        %4d / %d              [%s]\n" "$days_elapsed" "$MIN_DAYS" "$s_days"
    printf "    Trades closed:       %4d / %d              [%s]\n" "$trades" "$MIN_TRADES" "$s_trades"
    printf "    Wins / WR:           %4d / %s%%             [%s]\n" "$wins" "$wr_pct" "$s_wr"
    printf "    Net PnL:             \$%-12s              [%s]\n" "$pnl_int" "$s_pnl"
    printf "    Single-sym pct:      %s%% (%s)        [%s]\n" "$max_sym_pct_val" "$max_sym_name" "$s_sym"
    if [[ -n "$top_syms" ]]; then
        printf "    Top symbols:         %s\n" "$top_syms"
    fi
    printf "    First trade:         %s\n" "${first_ts:0:19}"
    printf "    Last trade:          %s\n" "${last_ts:0:19}"
    printf "    >>> VERDICT: %s\n" "$overall"
done

echo
echo "$SEP"
echo "  Notes:"
echo "  - Realized fee/slippage checks not yet implemented (require journal extension)"
echo "  - Kill criteria 'first 60 days net-negative' = same as Net PnL FAIL post 60-day mark"
echo "  - Kill criteria 'two consecutive 30-day windows underperform BTC-HODL' not yet implemented"
echo "  - All criteria from CLAUDE.md ## Forward-paper go/no-go criteria"
echo "$SEP"
echo
