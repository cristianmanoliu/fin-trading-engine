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
#   STRATEGY|<label>|<first_ts>|<last_ts>|<trades>|<wins>|<net_pnl>|<fee>|<slip>|<notional>|<notional_losers>
#   STRATEGY|<label>|NODATA                                            (no closes — may still have OPEN records)
#   SYMBOL|<label>|<symbol>|<sym_pnl>|<sym_trades>
#   CLOSE|<label>|<ts>|<symbol>|<pnl>|<outcome>                        (per-close, fed to HODL helper)
#   OPEN|<label>|<symbol>|<side>|<entry>|<stop>|<target>|<open_ts>     (currently-held — opens > closes)
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
    # Concatenate open + close events and aggregate via awk. Open records are
    # emitted so the local side can render currently-held positions even when
    # no closes exist yet (the "(no data yet)" gap that hides riding trades).
    # Cost columns (fee_usd, slip_usd, notional_usd) carry "//0" defaults so
    # older close events written before the schema extension still parse.
    # The same awk emits per-close TSV records (CLOSE|...) so the local-side
    # BTC-HODL benchmark helper can do windowed comparison without re-parsing
    # journals — no process substitution (which does not survive bash -s heredoc).
    cat "${files[@]}" | jq -r '"'"'select(.event=="open" or .event=="close") | [.event, .ts, .symbol, (.pnl_usd // 0), (.outcome // "STOP"), (.fee_usd // 0), (.slip_usd // 0), (.notional_usd // 0), (.side // ""), (.entry // 0), (.stop // 0), (.target // 0)] | @tsv'"'"' 2>/dev/null \
    | awk -F"\t" -v label="$label" '"'"'
        BEGIN { first_ts=""; last_ts=""; total=0; wins=0; pnl=0
                fee_usd=0; slip_usd_losers=0; notional=0; notional_losers=0 }
        $1 == "close" {
            # Emit per-close TSV record for downstream HODL comparator.
            printf "CLOSE|%s|%s|%s|%s|%s\n", label, $2, $3, $4, $5
            if (first_ts=="") first_ts=$2
            last_ts=$2
            total++
            if ($5=="TARGET" || $5=="PARTIAL") wins++
            pnl += $4
            fee_usd += $6
            notional += $8
            if ($5=="STOP") {
                slip_usd_losers += $7
                notional_losers += $8
            }
            sym_pnl[$3] += $4
            sym_count[$3]++
            closes_count[$3]++
        }
        $1 == "open" {
            opens_count[$3]++
            # Track LATEST open per symbol; END emits details for any symbol
            # whose opens > closes (= position currently held). Files are
            # cat-ed in lexicographic order (which matches chronological order
            # for the YYYY-MM filename suffix), so the last assignment wins.
            last_open_side[$3]   = $9
            last_open_entry[$3]  = $10
            last_open_stop[$3]   = $11
            last_open_target[$3] = $12
            last_open_ts[$3]     = $2
        }
        END {
            for (s in opens_count) {
                if (opens_count[s] > (closes_count[s] + 0)) {
                    printf "OPEN|%s|%s|%s|%s|%s|%s|%s\n", \
                        label, s, last_open_side[s], last_open_entry[s], \
                        last_open_stop[s], last_open_target[s], last_open_ts[s]
                }
            }
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

# Pre-fetch current prices for any symbols with open positions, so the
# render_open_table helper can show a "now=…  +X% to stop" column. Gracefully
# degrades on any failure (network down, timeout, parse error) — the column
# is simply omitted for that symbol. Hitting Binance fapi /v1/ticker/price
# is 1 weight per call; even with 16 unique symbols across 4 cohorts that's
# trivial vs the 6000/min cap. No flag needed: if you're already online
# enough to ssh to the VPS, you're online enough for this.
unique_open_symbols=$(echo "$DATA" | awk -F'|' '$1=="OPEN" {print $3}' | sort -u)
# Space-separated SYM=PRICE pairs — passable to awk via -v without newline
# parse issues. Empty if all fetches fail or no open positions exist.
PRICE_TABLE=""
if [[ -n "$unique_open_symbols" ]]; then
    for sym in $unique_open_symbols; do
        price=$(curl -s --max-time 5 "https://fapi.binance.com/fapi/v1/ticker/price?symbol=${sym}" 2>/dev/null \
                | sed -nE 's/.*"price":"([0-9.]+)".*/\1/p')
        if [[ -n "$price" ]]; then
            PRICE_TABLE+="${sym}=${price} "
        fi
    done
fi

# Pretty header
SEP="$(printf '%0.s═' {1..78})"
echo
echo "$SEP"
echo "  Forward-paper go/no-go status — $(date -u '+%Y-%m-%d %H:%M UTC')"
echo "  Source: $VPS  ($JOURNAL_DIR)"
echo "$SEP"

# Iterate strategy lines
echo "$DATA" | grep "^STRATEGY|" | while IFS='|' read -r _ label first_ts last_ts trades wins pnl fee_usd slip_usd_losers notional notional_losers; do
    # Currently-held positions for this cohort (regardless of close status).
    open_pos_lines=$(echo "$DATA" | awk -F'|' -v lbl="$label" '$1=="OPEN" && $2==lbl {print}')
    open_count=0
    open_longs=0
    open_shorts=0
    if [[ -n "$open_pos_lines" ]]; then
        open_count=$(printf '%s\n' "$open_pos_lines" | wc -l | tr -d ' ')
        open_longs=$(printf '%s\n' "$open_pos_lines" | awk -F'|' '$4=="LONG"' | wc -l | tr -d ' ')
        open_shorts=$(printf '%s\n' "$open_pos_lines" | awk -F'|' '$4=="SHORT"' | wc -l | tr -d ' ')
    fi
    render_open_table() {
        # %.6g strips FP serialization noise (0.005360641999999999 → 0.00536064)
        # while preserving 6 sig figs — sufficient for an eyeball status check.
        # When a current price is available in PRICE_TABLE (pre-fetched at the
        # top of the script), append "R=… (TP=+NR)" — signed R-multiple in the
        # strategy's native unit. +R = profit toward target, −R = adverse toward
        # stop (−1R = stop touched, +TP_R = target hit). target-RR varies by
        # strategy variant so we display it alongside.
        # Pre-compute opened-timestamp epoch in bash (macOS BSD awk lacks
        # mktime; doing it in awk would either be wrong-by-TZ-offset or require
        # gawk). We append the epoch as the 9th pipe-field so awk can read it
        # directly. Per-line `date -j` invocation is O(n_open); n is small
        # (≤50 across all cohorts) so the process-spawn cost is negligible.
        augmented_lines=""
        while IFS= read -r _line; do
            [[ -z "$_line" ]] && continue
            _ts=$(printf '%s' "$_line" | awk -F'|' '{print substr($8,1,19)}')
            _epoch=$(date -u -j -f "%Y-%m-%dT%H:%M:%S" "$_ts" +%s 2>/dev/null || \
                     date -u -d "$_ts" +%s 2>/dev/null || echo "0")
            augmented_lines+="${_line}|${_epoch}"$'\n'
        done <<<"$open_pos_lines"

        printf '%s' "$augmented_lines" | awk -F'|' -v price_table="$PRICE_TABLE" -v now="$NOW" '
            BEGIN {
                # PRICE_TABLE is space-separated "SYM=PRICE" pairs.
                n = split(price_table, pairs, " ")
                for (i = 1; i <= n; i++) {
                    if (pairs[i] != "" && split(pairs[i], kv, "=") == 2) {
                        prices[kv[1]] = kv[2] + 0
                    }
                }
            }
            {
                sym = $3; side = $4
                entry = $5 + 0; stop = $6 + 0; target = $7 + 0
                ts = substr($8, 1, 19)
                opened_epoch = $9 + 0
                if (opened_epoch > 0) {
                    held_h = (now - opened_epoch) / 3600
                    held_str = sprintf("%5.1fh", held_h)
                } else {
                    held_str = "    ?h"
                }
                if (sym in prices && entry != stop) {
                    p = prices[sym]
                    if (side == "SHORT") {
                        # SHORT: profit when price falls; stop above entry, target below
                        r       = (entry - p)      / (stop - entry)
                        target_r= (entry - target) / (stop - entry)
                    } else {
                        # LONG: profit when price rises; stop below entry, target above
                        r       = (p - entry)      / (entry - stop)
                        target_r= (target - entry) / (entry - stop)
                    }
                    printf "      %-13s %-5s  entry=%-10.6g stop=%-10.6g target=%-10.6g now=%-10.6g R=%+5.2f (TP=%+.1fR)  held=%s  opened=%s\n",
                           sym, side, entry, stop, target, p, r, target_r, held_str, ts
                } else {
                    printf "      %-13s %-5s  entry=%-10.6g stop=%-10.6g target=%-10.6g (no price)                            held=%s  opened=%s\n",
                           sym, side, entry, stop, target, held_str, ts
                }
            }'
    }

    if [[ "$first_ts" == "NODATA" ]]; then
        if [[ "$open_count" -eq 0 ]]; then
            printf "\n  %-26s  (no data yet)\n" "$label"
        else
            printf "\n  %-26s  no closes yet — %d open (%d LONG, %d SHORT)\n" \
                "$label" "$open_count" "$open_longs" "$open_shorts"
            render_open_table
        fi
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
    # PnL/single-sym verdicts are gated on the trade-count floor so n=1 doesn't
    # spuriously declare FAIL. The threshold criteria are advisory-only per the
    # kill-bar mis-calibration finding (calibration verdict 2026-05-07); the
    # decision-grade kill mechanism is scripts/run_drift_check.sh.
    s_wr=$([[ "$trades" -ge "$MIN_TRADES" ]] && pass_or_fail "$wr_pct" "$MIN_WR_PCT" ge || echo "PENDING")
    if [[ "$trades" -ge "$MIN_TRADES" ]]; then
        s_pnl=$(pass_or_fail "$pnl" "0" ge)
        s_sym=$(pass_or_fail "$max_sym_pct_val" "$MAX_SYM_PCT" le)
    else
        s_pnl="INSUFFICIENT"
        s_sym="INSUFFICIENT"
    fi

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
    if [[ "$trades" -ge "$MIN_TRADES" ]]; then
        s_hodl_cumul=$(pass_or_fail "$hodl_delta_usd" "0" ge)
    else
        s_hodl_cumul="INSUFFICIENT"
    fi
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
    if [[ "$open_count" -gt 0 ]]; then
        printf "    Open positions:      %d (%d LONG, %d SHORT)\n" \
            "$open_count" "$open_longs" "$open_shorts"
        render_open_table
    fi
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
echo "  - Open positions = symbols where opens > closes in the journal — visibility for trades"
echo "    riding pre-first-close (e.g. fresh-deploy windows or rare-signal cohorts)"
echo "  - Open-position prices fetched live from Binance fapi /v1/ticker/price;"
echo "    R-multiple is the strategy's native unit: +R = profit toward target,"
echo "    −R = adverse toward stop (−1R = stop touched, TP=+NR shows the target R)"
echo "  - PnL/single-sym/HODL-cumul verdicts gated on trades ≥ \$MIN_TRADES"
echo "    (kill-bar mis-calibration verdict 2026-05-07 made these advisory-only;"
echo "    decision-grade kill is scripts/run_drift_check.sh)"
echo "  - Kill criteria 'first 60 days net-negative' = same as Net PnL FAIL post 60-day mark"
echo "  - BTC-HODL benchmark notional: \$$BENCHMARK_NOTIONAL (override via BENCHMARK_NOTIONAL env);"
echo "    consecutive 30d window kill threshold: \$$KILL_HODL_WINDOW_USD (override via KILL_HODL_WINDOW_USD)"
echo "  - All criteria from CLAUDE.md ## Forward-paper go/no-go criteria"
echo "$SEP"
echo
