#!/usr/bin/env bash
# paper_live_trades.sh — view closed trades and open positions across deployed engines
# Usage: ./scripts/paper_live_trades.sh [user@host]
#   Omit host to read from ./logs/journal/ locally.
set -euo pipefail

# Deployed symbol list — source of truth in configs/symbols.yaml `deployed`.
source "$(dirname "${BASH_SOURCE[0]}")/lib/symbols.sh"
read -ra SYMBOLS <<< "$(get_symbols deployed)"

# Determine where journals live
if [[ "${1:-}" == root@* ]] || [[ "${1:-}" == *@* ]]; then
    VPS="${1}"
    shift || true
else
    VPS=""
fi

run_remote() {
    if [[ -n "$VPS" ]]; then
        ssh -o BatchMode=yes -o ConnectTimeout=10 "$VPS" bash -s <<< "$1"
    else
        bash -c "$1"
    fi
}

MONTH="$(date -u '+%Y-%m')"

SCRIPT=$(cat <<'REMOTE_SCRIPT'
set -euo pipefail

JOURNAL_DIR="${JOURNAL_DIR:-/var/log/paper-live/journal}"
MONTH="${MONTH}"
SYMBOLS_STR="${SYMBOLS_STR}"

read -ra SYMBOLS <<< "$SYMBOLS_STR"

SEP="$(printf '%0.s─' {1..62})"

printf "\nTrade History — $(date -u '+%Y-%m-%d %H:%M UTC') (%s)\n" "$MONTH"
echo "$SEP"

total_trades=0
total_wins=0
total_losses=0
total_pnl=0

for sym in "${SYMBOLS[@]}"; do
    file="$JOURNAL_DIR/${sym}-${MONTH}.jsonl"

    if [[ ! -f "$file" ]]; then
        printf "\n%-10s  no data yet\n" "$sym"
        continue
    fi

    # All close events as TSV: ts side entry exit outcome pnl_usd pnl_pts reason
    closed_tsv=$(jq -r '
        select(.event=="close") |
        [.ts, .side, .entry, .exit, .outcome, .pnl_usd, .pnl_pts] | @tsv
    ' "$file" 2>/dev/null || true)

    count=$(printf '%s' "$closed_tsv" | grep -c $'\t' || echo 0)

    # Per-symbol aggregates (awk handles floats)
    if [[ $count -gt 0 ]]; then
        agg=$(printf '%s' "$closed_tsv" | awk -F'\t' '
            NF >= 6 {
                total++
                if ($5 == "TARGET") wins++
                pnl += $6
            }
            END {
                printf "%d\t%d\t%.2f", total, wins+0, pnl
            }
        ')
        IFS=$'\t' read -r c w pnl_sym <<< "$agg"
        l=$((c - w))
        wr=$((w * 100 / c))

        pnl_int=$(printf '%.0f' "$pnl_sym")
        if [[ "${pnl_sym:0:1}" == "-" ]]; then
            pnl_fmt="-\$${pnl_int#-}"
        else
            pnl_fmt="+\$$pnl_int"
        fi

        printf "\n%-10s  %d trade%s  %dW %dL  %d%% WR  %s\n" \
            "$sym" "$c" "$([[ $c -ne 1 ]] && echo s || true)" \
            "$w" "$l" "$wr" "$pnl_fmt"

        # Individual trades
        while IFS=$'\t' read -r ts side entry exit_ outcome pnl_usd pnl_pts; do
            [[ -z "$ts" ]] && continue
            pnl_i=$(printf '%.0f' "$pnl_usd")
            pts=$(printf '%.4f' "$pnl_pts")
            if [[ $outcome == "TARGET" ]]; then
                mark="WIN "
            else
                mark="LOSS"
            fi
            if [[ "${pnl_i:0:1}" == "-" ]]; then
                pnl_s="-\$${pnl_i#-}"
            else
                pnl_s="+\$$pnl_i"
            fi
            printf "  %s  %-5s  %-9s → %-9s  %s  %s  (%s pts)\n" \
                "${ts:0:16}" "$side" "$entry" "$exit_" "$mark" "$pnl_s" "$pts"
        done <<< "$closed_tsv"

        total_trades=$((total_trades + c))
        total_wins=$((total_wins + w))
        total_losses=$((total_losses + l))
        total_pnl=$(awk "BEGIN{printf \"%.2f\", $total_pnl + $pnl_sym}")
    else
        printf "\n%-10s  no closed trades\n" "$sym"
    fi

    # Show live open position (last event is an open with no matching close)
    last_event=$(jq -r '.event' "$file" 2>/dev/null | tail -1 || true)
    if [[ "$last_event" == "open" ]]; then
        open_line=$(jq -r 'select(.event=="open") | [.ts,.side,.entry,.target,.stop,.reason] | @tsv' "$file" | tail -1)
        IFS=$'\t' read -r ots oside oentry otarget ostop oreason <<< "$open_line"
        reason_short=$(printf '%s' "$oreason" | cut -d'|' -f1 | xargs)
        printf "  %s  %-5s  @ %-9s  tgt=%-9s  stp=%-9s  [OPEN — %s]\n" \
            "${ots:0:16}" "$oside" "$oentry" \
            "$(printf '%.4f' "$otarget")" \
            "$(printf '%.4f' "$ostop")" \
            "$reason_short"
    fi
done

echo
echo "$SEP"

if [[ $total_trades -gt 0 ]]; then
    wr=$((total_wins * 100 / total_trades))
    pnl_i=$(printf '%.0f' "$total_pnl")
    if [[ "${total_pnl:0:1}" == "-" ]]; then
        pnl_s="-\$${pnl_i#-}"
    else
        pnl_s="+\$$pnl_i"
    fi
    printf "TOTAL   %d trades  %dW %dL  %d%% WR  %s\n" \
        "$total_trades" "$total_wins" "$total_losses" "$wr" "$pnl_s"
else
    printf "TOTAL   0 closed trades — engines running, waiting for setups\n"
fi
echo
REMOTE_SCRIPT
)

# Inject variables into the remote environment
JOURNAL_DIR="${JOURNAL_DIR:-/var/log/paper-live/journal}"
EXPORT_VARS="export JOURNAL_DIR='$JOURNAL_DIR' MONTH='$MONTH' SYMBOLS_STR='${SYMBOLS[*]}'"

run_remote "$EXPORT_VARS; $SCRIPT"
