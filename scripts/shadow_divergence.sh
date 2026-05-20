#!/usr/bin/env bash
# shadow_divergence.sh — compare alt5-15-336 vs alt5-15-504 shadow journals.
#
# Matches trades by (symbol, side, entry) and classifies each pair as:
#   MATCHED  — both closed, same outcome + same close_ts (byte-identical path)
#   DIVERGED — both closed but different close (max-hold force-close vs natural exit)
#   PENDING  — one cohort closed (force-close at max-hold) but the other still
#              holding past 336h — the divergence event has fired, the
#              comparison just isn't fully resolved yet
#   ORPHAN   — trade closed in one cohort with no corresponding open in the
#              other (should not happen — indicates a journal-replay bug)
#
# Diverged + Pending trades are the signal: they show whether the 336h max-hold
# cap improved or worsened PnL compared to the 504h cap.
#
# Usage:
#   ./scripts/shadow_divergence.sh                   # default VPS
#   ./scripts/shadow_divergence.sh root@host         # alt VPS
#   ./scripts/shadow_divergence.sh local             # use ./logs/journal locally
#   ONELINE=1 ./scripts/shadow_divergence.sh         # single-line summary (for daily_digest)
set -euo pipefail

VPS="${1:-root@178.105.24.230}"
JOURNAL_DIR_DEFAULT="/var/log/paper-live/journal"
JOURNAL_DIR="${JOURNAL_DIR:-$JOURNAL_DIR_DEFAULT}"
ONELINE="${ONELINE:-0}"

SHADOW_A="alt5-15-336"
SHADOW_B="alt5-15-504"
LABEL_A="336h"
LABEL_B="504h"

run_cmd() {
    if [[ "$VPS" == "local" ]]; then
        bash -c "JOURNAL_DIR='$JOURNAL_DIR' bash -s" <<<"$1"
    else
        ssh "$VPS" "JOURNAL_DIR='$JOURNAL_DIR_DEFAULT' bash -s" <<<"$1"
    fi
}

DATA=$(run_cmd '
set -euo pipefail
shopt -s nullglob

SHADOW_A="'"$SHADOW_A"'"
SHADOW_B="'"$SHADOW_B"'"
DIR_A="${JOURNAL_DIR}/shadow/${SHADOW_A}"
DIR_B="${JOURNAL_DIR}/shadow/${SHADOW_B}"

if [[ ! -d "$DIR_A" ]]; then echo "ERROR|dir_a_missing|$DIR_A"; exit 0; fi
if [[ ! -d "$DIR_B" ]]; then echo "ERROR|dir_b_missing|$DIR_B"; exit 0; fi

# Extract all events from both shadows.
# Output: SHADOW|event|symbol|ts|side|entry|stop|target|exit|pnl_usd|outcome|mfe_r|mae_r
extract() {
    local label="$1"; shift
    local files=("$@")
    [[ ${#files[@]} -eq 0 ]] && return
    cat "${files[@]}" | jq -r \
        --arg label "$label" \
        '"'"'select(.event=="open" or .event=="close")
        | [$label, .event, (.symbol // ""), (.ts // ""), (.side // ""),
           (.entry // 0), (.stop // 0), (.target // 0), (.exit // 0),
           (.pnl_usd // 0), (.outcome // ""), (.mfe_r // 0), (.mae_r // 0)]
        | @tsv'"'"' 2>/dev/null
}

files_a=( "${DIR_A}"/*-*.jsonl )
files_b=( "${DIR_B}"/*-*.jsonl )

extract "A" "${files_a[@]}"
extract "B" "${files_b[@]}"
')

if echo "$DATA" | grep -q "^ERROR|"; then
    err=$(echo "$DATA" | grep "^ERROR|" | head -1)
    echo "ERROR: $(echo "$err" | cut -d'|' -f2-)"
    exit 1
fi

if [[ -z "$DATA" ]]; then
    echo "No journal data found for either shadow."
    exit 0
fi

# Process with awk: match trades by (symbol, side, entry); compare events.
#
# Match-key design: opens and closes both carry (symbol, side, entry) so we
# use that triple as the match key, keeping opens and closes addressable in
# the same map. Caveat: re-entries at exactly the same entry price on the
# same symbol+side collide — currently rare in production, not handled.
RESULT=$(echo "$DATA" | awk -F'\t' '
BEGIN {
    matched = 0; diverged = 0; orphan_a = 0; orphan_b = 0
    pending_a = 0; pending_b = 0  # A force-closed, B still open (or vice versa)
    open_a = 0; open_b = 0
    total_pnl_a = 0; total_pnl_b = 0
    div_advantage_a = 0; div_advantage_b = 0; div_tie = 0
    div_pnl_sum_a = 0; div_pnl_sum_b = 0
    pending_pnl_sum_a = 0; pending_pnl_sum_b = 0
}

{
    shadow = $1
    event  = $2
    sym    = $3
    ts     = $4
    side   = $5
    entry  = $6 + 0
    stop   = $7 + 0
    target = $8 + 0
    exit_p = $9 + 0
    pnl    = $10 + 0
    outcome= $11
    mfe_r  = $12 + 0
    mae_r  = $13 + 0

    # Unified match key — opens and closes both carry (sym, side, entry).
    mkey = sym "|" side "|" sprintf("%.10g", entry)

    if (event == "open") {
        if (shadow == "A") {
            a_open[mkey] = 1
            a_open_ts[mkey] = ts
            open_a++
        } else {
            b_open[mkey] = 1
            b_open_ts[mkey] = ts
            open_b++
        }
    }

    if (event == "close") {
        if (shadow == "A") {
            a_close[mkey] = 1
            a_close_ts[mkey] = ts
            a_pnl[mkey] = pnl
            a_outcome[mkey] = outcome
            a_exit[mkey] = exit_p
            a_mfe[mkey] = mfe_r
            total_pnl_a += pnl
        } else {
            b_close[mkey] = 1
            b_close_ts[mkey] = ts
            b_pnl[mkey] = pnl
            b_outcome[mkey] = outcome
            b_exit[mkey] = exit_p
            b_mfe[mkey] = mfe_r
            total_pnl_b += pnl
        }
    }
}

END {
    # Sweep A-closed trades.
    for (mkey in a_close) {
        if (mkey in b_close) {
            # Both closed — MATCHED or DIVERGED.
            if (a_outcome[mkey] == b_outcome[mkey] && a_close_ts[mkey] == b_close_ts[mkey]) {
                matched++
            } else {
                diverged++
                pnl_a = a_pnl[mkey]
                pnl_b = b_pnl[mkey]
                delta = pnl_a - pnl_b
                if (delta > 0.01) div_advantage_a++
                else if (delta < -0.01) div_advantage_b++
                else div_tie++
                div_pnl_sum_a += pnl_a
                div_pnl_sum_b += pnl_b

                split(mkey, parts, "|")
                sym = parts[1]
                side = parts[2]
                printf "DIV|%s|%s|%s|%s|%.2f|%s|%s|%.2f|%.2f\n", \
                    sym, side, \
                    a_outcome[mkey], a_close_ts[mkey], pnl_a, \
                    b_outcome[mkey], b_close_ts[mkey], pnl_b, \
                    pnl_a - pnl_b
            }
        } else if (mkey in b_open) {
            # A closed, B has the position but has not closed it yet.
            # This is the canonical FILUSDT shape — divergence event fired
            # when A hits max-hold while B keeps holding past 336h. NOT an
            # orphan; report as PENDING so daily_digest surfaces it.
            pending_a++
            pending_pnl_sum_a += a_pnl[mkey]
            split(mkey, parts, "|")
            printf "PEND_A|%s|%s|%s|%s|%.2f|%s\n", \
                parts[1], parts[2], \
                a_outcome[mkey], a_close_ts[mkey], a_pnl[mkey], \
                b_open_ts[mkey]
        } else {
            # A closed, B never opened — true ORPHAN (journal-replay bug
            # signal: positions should always open on both cohorts since
            # they consume the same signal stream).
            orphan_a++
            split(mkey, parts, "|")
            printf "ORPHAN_A|%s|%s|%s|%.2f\n", parts[1], a_outcome[mkey], a_close_ts[mkey], a_pnl[mkey]
        }
    }
    # Sweep B-closed trades that are NOT already matched/diverged.
    for (mkey in b_close) {
        if (mkey in a_close) continue  # already counted above
        if (mkey in a_open) {
            pending_b++
            pending_pnl_sum_b += b_pnl[mkey]
            split(mkey, parts, "|")
            printf "PEND_B|%s|%s|%s|%s|%.2f|%s\n", \
                parts[1], parts[2], \
                b_outcome[mkey], b_close_ts[mkey], b_pnl[mkey], \
                a_open_ts[mkey]
        } else {
            orphan_b++
            split(mkey, parts, "|")
            printf "ORPHAN_B|%s|%s|%s|%.2f\n", parts[1], b_outcome[mkey], b_close_ts[mkey], b_pnl[mkey]
        }
    }

    # Count still-open in each — positions opened that have no close on
    # EITHER cohort yet.
    still_open_a = open_a - (matched + diverged + orphan_a + pending_a)
    still_open_b = open_b - (matched + diverged + orphan_b + pending_b)
    if (still_open_a < 0) still_open_a = 0
    if (still_open_b < 0) still_open_b = 0

    printf "SUMMARY|%d|%d|%d|%d|%d|%d|%.2f|%.2f|%d|%d|%d|%.2f|%.2f|%.2f|%.2f|%d|%d\n", \
        matched, diverged, pending_a, pending_b, orphan_a, orphan_b, \
        total_pnl_a, total_pnl_b, \
        div_advantage_a, div_advantage_b, div_tie, \
        div_pnl_sum_a, div_pnl_sum_b, \
        pending_pnl_sum_a, pending_pnl_sum_b, \
        still_open_a, still_open_b
}
')

# Parse summary
SUMMARY=$(echo "$RESULT" | grep "^SUMMARY|" | head -1)
if [[ -z "$SUMMARY" ]]; then
    echo "No closed trades found in either shadow."
    exit 0
fi

IFS='|' read -r _ s_matched s_diverged s_pending_a s_pending_b s_orphan_a s_orphan_b \
    s_pnl_a s_pnl_b \
    s_adv_a s_adv_b s_tie \
    s_div_pnl_a s_div_pnl_b \
    s_pend_pnl_a s_pend_pnl_b \
    s_open_a s_open_b <<< "$SUMMARY"

s_pending_total=$((s_pending_a + s_pending_b))

# One-line mode for daily_digest integration
if [[ "$ONELINE" == "1" ]]; then
    if [[ "$s_diverged" -eq 0 && "$s_pending_total" -eq 0 ]]; then
        echo "336 vs 504: identical (no trade >336h yet)"
    elif [[ "$s_diverged" -eq 0 ]]; then
        # Pending only — one cohort force-closed, the other still riding.
        pend_pnl_delta=$(awk -v a="$s_pend_pnl_a" -v b="$s_pend_pnl_b" 'BEGIN{printf "%+.0f", a-b}')
        echo "336 vs 504: PENDING ${s_pending_total} trades (336h closed @max-hold, 504h still holding), 336h realized PnL \$${pend_pnl_delta}"
    else
        pnl_delta=$(awk -v a="$s_div_pnl_a" -v b="$s_div_pnl_b" 'BEGIN{printf "%+.0f", a-b}')
        suffix=""
        if [[ "$s_pending_total" -gt 0 ]]; then suffix=" (+${s_pending_total} pending)"; fi
        if [[ "$s_adv_a" -gt "$s_adv_b" ]]; then
            echo "336 vs 504: DIVERGED ${s_diverged} trades, 336h wins ${s_adv_a}/${s_diverged}, PnL delta \$${pnl_delta}${suffix}"
        elif [[ "$s_adv_b" -gt "$s_adv_a" ]]; then
            echo "336 vs 504: DIVERGED ${s_diverged} trades, 504h wins ${s_adv_b}/${s_diverged}, PnL delta \$${pnl_delta}${suffix}"
        else
            echo "336 vs 504: DIVERGED ${s_diverged} trades, tied ${s_adv_a}/${s_adv_b}, PnL delta \$${pnl_delta}${suffix}"
        fi
    fi
    exit 0
fi

# Full report
SEP="$(printf '%0.s═' {1..80})"
ROW="$(printf '%0.s─' {1..80})"

echo
echo "$SEP"
echo "  Shadow Divergence: ${SHADOW_A} (${LABEL_A}) vs ${SHADOW_B} (${LABEL_B})"
echo "  $(date -u '+%Y-%m-%d %H:%M UTC')"
echo "$SEP"

echo
printf "  %-25s %10s %10s\n" "" "$LABEL_A" "$LABEL_B"
printf "  %s\n" "$ROW"

pnl_a_fmt=$(awk -v p="$s_pnl_a" 'BEGIN{printf "%+.0f", p}')
pnl_b_fmt=$(awk -v p="$s_pnl_b" 'BEGIN{printf "%+.0f", p}')
printf "  %-25s %10s %10s\n" "Total PnL" "\$$pnl_a_fmt" "\$$pnl_b_fmt"
printf "  %-25s %10d %10d\n" "Closed trades" \
    "$((s_matched + s_diverged + s_pending_a + s_orphan_a))" \
    "$((s_matched + s_diverged + s_pending_b + s_orphan_b))"
printf "  %-25s %10d %10d\n" "Still open" "$s_open_a" "$s_open_b"

echo
printf "  %-25s %10s\n" "Matched (identical)" "$s_matched"
printf "  %-25s %10s\n" "Diverged (both closed)" "$s_diverged"
printf "  %-25s %10s\n" "Pending ($LABEL_A only)" "$s_pending_a"
printf "  %-25s %10s\n" "Pending ($LABEL_B only)" "$s_pending_b"

if [[ "$s_orphan_a" -gt 0 ]] || [[ "$s_orphan_b" -gt 0 ]]; then
    printf "  %-25s %10s\n" "Orphan ($LABEL_A only)" "$s_orphan_a"
    printf "  %-25s %10s\n" "Orphan ($LABEL_B only)" "$s_orphan_b"
fi

if [[ "$s_diverged" -gt 0 ]]; then
    echo
    echo "  Divergence detail"
    printf "  %s\n" "$ROW"
    printf "  %-12s %-6s  %-8s %10s  %-8s %10s  %10s\n" \
        "Symbol" "Side" "336h out" "336h PnL" "504h out" "504h PnL" "Delta"
    printf "  %s\n" "$ROW"

    echo "$RESULT" | grep "^DIV|" | sort -t'|' -k2 | while IFS='|' read -r _ sym side out_a ts_a pnl_a out_b ts_b pnl_b delta; do
        pnl_a_f=$(awk -v p="$pnl_a" 'BEGIN{printf "%+.0f", p}')
        pnl_b_f=$(awk -v p="$pnl_b" 'BEGIN{printf "%+.0f", p}')
        delta_f=$(awk -v d="$delta" 'BEGIN{printf "%+.0f", d}')
        printf "  %-12s %-6s  %-8s %10s  %-8s %10s  %10s\n" \
            "$sym" "$side" "$out_a" "\$$pnl_a_f" "$out_b" "\$$pnl_b_f" "\$$delta_f"
    done

    echo
    div_delta=$(awk -v a="$s_div_pnl_a" -v b="$s_div_pnl_b" 'BEGIN{printf "%+.0f", a-b}')
    printf "  Diverged PnL:  %s = \$%s  |  %s = \$%s  |  Delta = \$%s\n" \
        "$LABEL_A" "$(awk -v p="$s_div_pnl_a" 'BEGIN{printf "%+.0f", p}')" \
        "$LABEL_B" "$(awk -v p="$s_div_pnl_b" 'BEGIN{printf "%+.0f", p}')" \
        "$div_delta"
    printf "  Advantage:     %s wins %d  |  %s wins %d  |  ties %d\n" \
        "$LABEL_A" "$s_adv_a" "$LABEL_B" "$s_adv_b" "$s_tie"
fi

if [[ "$s_pending_total" -gt 0 ]]; then
    echo
    echo "  Pending detail (divergence event fired; other cohort still holding)"
    printf "  %s\n" "$ROW"
    printf "  %-12s %-6s  %-10s %-22s %10s  %-22s\n" \
        "Symbol" "Side" "Closed by" "Close ts (UTC)" "PnL" "Open ts on other (UTC)"
    printf "  %s\n" "$ROW"
    echo "$RESULT" | grep -E "^PEND_[AB]\\|" | sort -t'|' -k2 | while IFS='|' read -r tag sym side outc ts pnl other_ts; do
        pnl_f=$(awk -v p="$pnl" 'BEGIN{printf "%+.0f", p}')
        closed_by="$LABEL_A"
        [[ "$tag" == "PEND_B" ]] && closed_by="$LABEL_B"
        printf "  %-12s %-6s  %-10s %-22s %10s  %-22s\n" \
            "$sym" "$side" "$closed_by" "${ts:0:19}" "\$$pnl_f" "${other_ts:0:19}"
    done
    pend_delta=$(awk -v a="$s_pend_pnl_a" -v b="$s_pend_pnl_b" 'BEGIN{printf "%+.0f", a-b}')
    echo
    printf "  Pending realized: %s = \$%s  |  %s = \$%s  |  Net delta = \$%s\n" \
        "$LABEL_A" "$(awk -v p="$s_pend_pnl_a" 'BEGIN{printf "%+.0f", p}')" \
        "$LABEL_B" "$(awk -v p="$s_pend_pnl_b" 'BEGIN{printf "%+.0f", p}')" \
        "$pend_delta"
fi

if [[ "$s_diverged" -eq 0 && "$s_pending_total" -eq 0 ]]; then
    echo
    echo "  No divergence yet — all trades resolved identically."
    echo "  First expected divergence: when a position exceeds 336h hold time."
    echo "  (Any position open >14 days will be force-closed by 336h but kept by 504h.)"

    # Show candidates for divergence
    CANDIDATES=$(run_cmd '
    set -euo pipefail
    shopt -s nullglob
    DIR="${JOURNAL_DIR}/shadow/'"$SHADOW_A"'"
    [[ -d "$DIR" ]] || exit 0
    for f in "$DIR"/*-*.jsonl; do
        sym=$(basename "$f" | sed "s/-20[0-9][0-9].*//")
        opens=$(jq -c "select(.event==\"open\")" "$f" 2>/dev/null | wc -l | tr -d " ")
        closes=$(jq -c "select(.event==\"close\")" "$f" 2>/dev/null | wc -l | tr -d " ")
        if [ "$opens" -gt "$closes" ]; then
            open_ts=$(jq -r "select(.event==\"open\") | .ts" "$f" 2>/dev/null | tail -1)
            echo "${sym}|${open_ts}"
        fi
    done
    ')

    if [[ -n "$CANDIDATES" ]]; then
        echo
        echo "  Pending divergence candidates (currently open):"
        printf "  %s\n" "$ROW"
        printf "  %-12s %-22s %10s %10s\n" "Symbol" "Opened" "Hours held" "336h fires"
        printf "  %s\n" "$ROW"

        NOW_EPOCH=$(date -u +%s)
        echo "$CANDIDATES" | sort -t'|' -k2 | while IFS='|' read -r sym open_ts; do
            [[ -z "$sym" ]] && continue
            open_epoch=$(date -d "${open_ts}" +%s 2>/dev/null || \
                         date -u -j -f "%Y-%m-%dT%H:%M:%SZ" "${open_ts}" +%s 2>/dev/null || \
                         echo "$NOW_EPOCH")
            held_h=$(( (NOW_EPOCH - open_epoch) / 3600 ))
            fire_epoch=$(( open_epoch + 336 * 3600 ))
            fire_date=$(date -u -d "@$fire_epoch" '+%Y-%m-%d %H:%M' 2>/dev/null || \
                        date -u -r "$fire_epoch" '+%Y-%m-%d %H:%M' 2>/dev/null || echo "?")
            printf "  %-12s %-22s %8dh   %s UTC\n" "$sym" "${open_ts:0:19}" "$held_h" "$fire_date"
        done
    fi
fi

echo
echo "$SEP"
echo
