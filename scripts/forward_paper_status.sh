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
KILL_MAX_FEE_BP=12   # Realized round-trip fee ≤ 12bp (vs 10bp modeled — 20% slack)
KILL_MAX_SLIP_BP=25  # Realized stop-side slip ≤ 25bp on losing-trade subsample (cliff edge)
# BTC-HODL benchmark notional: CLAUDE.md specifies $32k from the deployed-32
# era. Current deployed is 16 engines × $1k stake = $16k. Override via env if
# you want to reconcile with current notional. Kill-window threshold $5k absolute.
BENCHMARK_NOTIONAL="${BENCHMARK_NOTIONAL:-32000}"
KILL_HODL_WINDOW_USD="${KILL_HODL_WINDOW_USD:-5000}"
HODL_HELPER="$(cd "$(dirname "$0")" && pwd)/btc_hodl_benchmark.py"

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
    # Cost columns (fee_usd, slip_usd, notional_usd) carry "//0" defaults so
    # older close events written before the schema extension still parse.
    # The same awk emits per-close TSV records (CLOSE|...) so the local-side
    # BTC-HODL benchmark helper can do windowed comparison without re-parsing
    # journals — no process substitution (which does not survive bash -s heredoc).
    cat "${files[@]}" | jq -r '"'"'select(.event=="close") | [.ts, .symbol, (.pnl_usd // 0), (.outcome // "STOP"), (.fee_usd // 0), (.slip_usd // 0), (.notional_usd // 0)] | @tsv'"'"' 2>/dev/null \
    | awk -F"\t" -v label="$label" '"'"'
        BEGIN { first_ts=""; last_ts=""; total=0; wins=0; pnl=0
                fee_usd=0; slip_usd_losers=0; notional=0; notional_losers=0 }
        {
            # Emit per-close TSV record for downstream HODL comparator.
            printf "CLOSE|%s|%s|%s|%s|%s\n", label, $1, $2, $3, $4
            if (first_ts=="") first_ts=$1
            last_ts=$1
            total++
            if ($4=="TARGET" || $4=="PARTIAL") wins++
            pnl += $3
            fee_usd += $5
            notional += $7
            if ($4=="STOP") {
                slip_usd_losers += $6
                notional_losers += $7
            }
            sym_pnl[$2] += $3
            sym_count[$2]++
        }
        END {
            if (total==0) { printf "STRATEGY|%s|NODATA\n", label; exit }
            printf "STRATEGY|%s|%s|%s|%d|%d|%.2f|%.2f|%.2f|%.2f|%.2f\n", \
                label, first_ts, last_ts, total, wins, pnl, \
                fee_usd, slip_usd_losers, notional, notional_losers
            for (s in sym_pnl) printf "SYMBOL|%s|%s|%.2f|%d\n", label, s, sym_pnl[s], sym_count[s]
        }'"'"'
}

# Live: top-level *.jsonl. Use ${arr[@]+"${arr[@]}"} for bash 3.2 (macOS)
# compatibility — direct ${arr[@]} on an empty array errors under set -u.
live_files=( "${JOURNAL_DIR}"/*-*.jsonl )
aggregate "live" ${live_files[@]+"${live_files[@]}"}

# Shadows: scan shadow/*/  for any per-symbol files
if [[ -d "${JOURNAL_DIR}/shadow" ]]; then
    for label_dir in "${JOURNAL_DIR}/shadow"/*/; do
        [[ -d "$label_dir" ]] || continue
        label_name=$(basename "$label_dir")
        shadow_files=( "${label_dir}"*-*.jsonl )
        aggregate "shadow/${label_name}" ${shadow_files[@]+"${shadow_files[@]}"}
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
echo "$DATA" | grep "^STRATEGY|" | while IFS='|' read -r _ label first_ts last_ts trades wins pnl fee_usd slip_usd_losers notional notional_losers; do
    if [[ "$first_ts" == "NODATA" ]]; then
        printf "\n  %-26s  (no data yet)\n" "$label"
        continue
    fi
    # Defaults for old strategy lines that predate the cost columns.
    fee_usd="${fee_usd:-0}"
    slip_usd_losers="${slip_usd_losers:-0}"
    notional="${notional:-0}"
    notional_losers="${notional_losers:-0}"

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

    # Realized fee/slip bps (from journal cost decomposition). When notional is
    # 0 — either every close predates the schema extension or every closed
    # trade had StakeUSDT=0 — display "n/a" rather than dividing by zero.
    if (( $(awk "BEGIN{print ($notional > 0)}") )); then
        fee_bps_val=$(awk "BEGIN{printf \"%.2f\", $fee_usd / $notional * 10000}")
        s_fee=$(pass_or_fail "$fee_bps_val" "$KILL_MAX_FEE_BP" le)
    else
        fee_bps_val="n/a"
        s_fee="PENDING"
    fi
    if (( $(awk "BEGIN{print ($notional_losers > 0)}") )); then
        slip_bps_val=$(awk "BEGIN{printf \"%.2f\", $slip_usd_losers / $notional_losers * 10000}")
        s_slip=$(pass_or_fail "$slip_bps_val" "$KILL_MAX_SLIP_BP" le)
    else
        slip_bps_val="n/a"
        s_slip="PENDING"
    fi

    # BTC-HODL benchmark — cumulative deploy criterion + rolling-30d kill
    # criterion (two consecutive windows underperforming by >$KILL_HODL_WINDOW_USD).
    strategy_closes=$(echo "$DATA" | awk -F'|' -v lbl="$label" '$1=="CLOSE" && $2==lbl {OFS="\t"; print $3, $4, $5, $6}')
    hodl_strategy_usd="0"; hodl_total_usd="0"; hodl_delta_usd="0"
    hodl_n_windows="0"; hodl_kill_pairs="0"; hodl_kill="0"; hodl_warning=""
    if [[ -n "$strategy_closes" ]] && [[ -x "$HODL_HELPER" ]]; then
        hodl_out=$(echo "$strategy_closes" | "$HODL_HELPER" \
            --benchmark-notional "$BENCHMARK_NOTIONAL" \
            --kill-threshold-usd "$KILL_HODL_WINDOW_USD" 2>/dev/null || true)
        if [[ -n "$hodl_out" ]]; then
            IFS=$'\t' read -r hodl_strategy_usd hodl_total_usd hodl_delta_usd \
                hodl_n_windows hodl_kill_pairs hodl_kill hodl_warning <<<"$hodl_out"
        fi
    fi
    s_hodl_cumul=$(pass_or_fail "$hodl_delta_usd" "0" ge)
    if [[ "$hodl_kill" == "1" ]]; then
        s_hodl_window="FAIL"
    elif [[ "$hodl_n_windows" -lt "2" ]]; then
        s_hodl_window="PENDING"
    else
        s_hodl_window="PASS"
    fi

    # Overall verdict — fee/slip and HODL kill criteria check at any data volume
    # since they're per-trade / per-window signals that don't need 60-day power
    # floor confirmation.
    if [[ "$s_fee" == "FAIL" ]] || [[ "$s_slip" == "FAIL" ]]; then
        overall="KILL — realized cost exceeds kill threshold"
    elif [[ "$s_hodl_window" == "FAIL" ]]; then
        overall="KILL — two consecutive 30d windows underperform BTC-HODL"
    elif [[ "$days_elapsed" -lt "$MIN_DAYS" ]] || [[ "$trades" -lt "$MIN_TRADES" ]]; then
        overall="WAITING (insufficient data)"
    elif [[ "$s_pnl" == "FAIL" ]] || [[ "$s_wr" == "FAIL" ]] || [[ "$s_sym" == "FAIL" ]] || [[ "$s_hodl_cumul" == "FAIL" ]]; then
        overall="KILL — at least one criterion failed"
    elif [[ "$s_pnl" == "PASS" ]] && [[ "$s_wr" == "PASS" ]] && [[ "$s_sym" == "PASS" ]] && [[ "$s_fee" == "PASS" ]] && [[ "$s_slip" == "PASS" ]] && [[ "$s_hodl_cumul" == "PASS" ]]; then
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
    printf "    Realized fee bps:    %-6s  / ≤%dbp                [%s]\n" "$fee_bps_val" "$KILL_MAX_FEE_BP" "$s_fee"
    printf "    Realized slip bps:   %-6s  / ≤%dbp (losers)       [%s]\n" "$slip_bps_val" "$KILL_MAX_SLIP_BP" "$s_slip"
    hodl_delta_int=$(awk -v d="$hodl_delta_usd" 'BEGIN{printf "%+d", d}')
    hodl_total_int=$(awk -v h="$hodl_total_usd" 'BEGIN{printf "%+d", h}')
    if [[ -n "$hodl_warning" ]]; then
        printf "    BTC-HODL Δ vs \$%-5d: (%s)                          [PENDING]\n" "$BENCHMARK_NOTIONAL" "$hodl_warning"
    else
        printf "    BTC-HODL Δ vs \$%-5d: \$%-12s  (HODL=\$%s)  [%s]\n" \
            "$BENCHMARK_NOTIONAL" "$hodl_delta_int" "$hodl_total_int" "$s_hodl_cumul"
    fi
    printf "    30d windows / kill-pairs: %s / %s                    [%s]\n" "$hodl_n_windows" "$hodl_kill_pairs" "$s_hodl_window"
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
echo "  - Realized fee/slip bps come from journal cost decomposition (gross/fee/slip/notional)"
echo "  - Slip bps computed on the losing-trade (outcome=STOP) subsample only — partial closes"
echo "    and full target hits are excluded since slippage is modeled on losers only"
echo "  - n/a means no closes carry the cost decomposition yet (engine restart needed before"
echo "    new closes will land in the journal — old closes pre-extension show as 0 fee/slip)"
echo "  - Kill criteria 'first 60 days net-negative' = same as Net PnL FAIL post 60-day mark"
echo "  - BTC-HODL benchmark notional: \$$BENCHMARK_NOTIONAL (override via BENCHMARK_NOTIONAL env);"
echo "    consecutive 30d window kill threshold: \$$KILL_HODL_WINDOW_USD (override via KILL_HODL_WINDOW_USD)"
echo "  - All criteria from CLAUDE.md ## Forward-paper go/no-go criteria"
echo "$SEP"
echo
