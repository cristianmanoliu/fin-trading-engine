#!/usr/bin/env bash
# daily_digest.sh — compact Telegram summary of forward-paper status.
#
# Runs on VPS via cron at 09:00 UTC. Summarises all cohorts (live +
# shadows) in a single message: per-cohort trades/WR/PnL + open
# positions + best R-multiple highlight (>2.0R). Sources lib/notify.sh
# for delivery.
#
# Usage:
#   DRY_RUN=1 JOURNAL_DIR=./logs/journal ./scripts/daily_digest.sh
#   ./scripts/daily_digest.sh          # reads JOURNAL_DIR, sends via notify_telegram
#
# Environment:
#   JOURNAL_DIR      — journal root (default: /var/log/paper-live/journal)
#   DRY_RUN=1        — print to stdout instead of sending Telegram
#   FETCH_PRICES=1   — force price fetch even in DRY_RUN (default: skip in DRY_RUN)
#
# Exit codes:
#   0  — success (message sent or printed)
#   1  — JOURNAL_DIR not found
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LIB="${SCRIPT_DIR}/lib/notify.sh"

# shellcheck source=lib/notify.sh
source "$LIB"

JOURNAL_DIR="${JOURNAL_DIR:-/var/log/paper-live/journal}"
DRY_RUN="${DRY_RUN:-0}"
FETCH_PRICES="${FETCH_PRICES:-0}"

TODAY="$(date -u '+%Y-%m-%d')"

# ── Validate JOURNAL_DIR ─────────────────────────────────────────────────────
if [[ ! -d "$JOURNAL_DIR" ]]; then
    msg="JOURNAL_DIR not found: $JOURNAL_DIR"
    if [[ "$DRY_RUN" == "1" ]]; then
        echo "$msg" >&2
    else
        notify_telegram WARN "daily_digest" "$msg"
    fi
    exit 1
fi

# ── Check for any journal files ──────────────────────────────────────────────
shopt -s nullglob
all_journals=( "${JOURNAL_DIR}"/*-*.jsonl )
# Also check shadow subdirectories
if [[ -d "${JOURNAL_DIR}/shadow" ]]; then
    for _sd in "${JOURNAL_DIR}/shadow"/*/; do
        [[ -d "$_sd" ]] && all_journals+=( "${_sd}"*-*.jsonl )
    done
fi
if [[ ${#all_journals[@]} -eq 0 ]]; then
    msg="Daily Digest — ${TODAY}

no journal data found in $JOURNAL_DIR"
    if [[ "$DRY_RUN" == "1" ]]; then
        echo "$msg"
    else
        notify_telegram WARN "daily_digest" "$msg"
    fi
    exit 0
fi

# ── Helper: parse one cohort's journal files ─────────────────────────────────
# Outputs tab-separated: trades wins pnl_usd open_sides
# open_sides is a space-separated list of SIDE values for open positions.
parse_cohort() {
    local -a files=("$@")
    if [[ ${#files[@]} -eq 0 ]]; then
        echo "0	0	0	"
        return
    fi
    cat "${files[@]}" \
    | jq -r 'select(.event=="open" or .event=="close")
              | [.event, (.pnl_usd // 0), (.outcome // ""), (.side // "")]
              | @tsv' 2>/dev/null \
    | awk -F'\t' '
        BEGIN { total=0; wins=0; pnl=0 }
        $1=="close" {
            outcome=$3
            if (outcome=="PARTIAL") next
            total++
            if (outcome=="TARGET") wins++
            pnl += $2
            closes[$4]++   # track symbol side on close — we only need open positions
        }
        $1=="open" {
            opens_side[NR] = $4
            opens_count++
        }
        END {
            # Collect open sides (opens without matching close)
            # We approximate: count of open events vs close events
            # (journals are SYMBOL-specific so open > close = open position)
            open_sides=""
            # emit open events that have no paired close — simplified:
            # if opens_count > total, difference is open positions
            extra = opens_count - total
            if (extra < 0) extra = 0
            for (i = 1; i <= opens_count && extra > 0; i++) {
                side = opens_side[i]
                open_sides = (open_sides == "") ? side : open_sides " " side
                extra--
            }
            printf "%d\t%d\t%.2f\t%s\n", total, wins, pnl, open_sides
        }
    '
}

# ── Better per-symbol parsing that tracks open vs closed per-symbol ──────────
# Produces:
#   STATS: trades wins pnl_usd
#   OPEN:  symbol side
parse_cohort_full() {
    local -a files=("$@")
    if [[ ${#files[@]} -eq 0 ]]; then
        return
    fi
    cat "${files[@]}" \
    | jq -r 'select(.event=="open" or .event=="close")
              | [.event, (.symbol // ""), (.pnl_usd // 0), (.outcome // ""), (.side // ""),
                 (.entry // 0), (.stop // 0), (.target // 0)]
              | @tsv' 2>/dev/null \
    | awk -F'\t' '
        BEGIN { total=0; wins=0; pnl=0 }
        $1=="open" {
            sym=$2
            opens[sym]++
            last_side[sym]   = $5
            last_entry[sym]  = $6
            last_stop[sym]   = $7
            last_target[sym] = $8
        }
        $1=="close" {
            sym=$2
            outcome=$4
            closes[sym]++
            if (outcome!="PARTIAL") {
                total++
                if (outcome=="TARGET") wins++
                pnl += $3
            }
        }
        END {
            printf "STATS\t%d\t%d\t%.2f\n", total, wins, pnl
            for (sym in opens) {
                o = opens[sym]+0
                c = closes[sym]+0
                if (o > c) {
                    printf "OPEN\t%s\t%s\t%s\t%s\t%s\n",
                        sym, last_side[sym], last_entry[sym],
                        last_stop[sym], last_target[sym]
                }
            }
        }
    '
}

# ── Fetch current price from Binance fapi ────────────────────────────────────
fetch_price() {
    local sym="$1"
    curl -s --max-time 5 \
        "https://fapi.binance.com/fapi/v1/ticker/price?symbol=${sym}" 2>/dev/null \
    | sed -nE 's/.*"price":"([0-9.]+)".*/\1/p'
}

# ── Compute R-multiple ───────────────────────────────────────────────────────
# Returns empty string on error.
compute_r() {
    local side="$1" entry="$2" stop="$3" price="$4"
    awk -v side="$side" -v entry="$entry" -v stop="$stop" -v price="$price" '
    BEGIN {
        if (entry==stop || entry==0 || stop==0 || price==0) { print ""; exit }
        if (side=="SHORT") r=(entry-price)/(stop-entry)
        else               r=(price-entry)/(entry-stop)
        printf "%+.2f", r
    }'
}

# ── Format open positions line ───────────────────────────────────────────────
# Takes open_data lines (OPEN\tsym\tside\tentry\tstop\ttarget)
format_open_line() {
    local open_data="$1"
    local do_fetch="$2"  # 1 = fetch prices

    local n_open=0 n_long=0 n_short=0 best_r="" best_sym="" best_side=""
    local sym_list=""

    while IFS=$'\t' read -r tag sym side entry stop target; do
        [[ "$tag" != "OPEN" ]] && continue
        n_open=$(( n_open + 1 ))
        # Track symbol names for display
        sym_list="${sym_list}${sym}(${side}) "
        if [[ "$side" == "LONG" ]]; then
            n_long=$(( n_long + 1 ))
        elif [[ "$side" == "SHORT" ]]; then
            n_short=$(( n_short + 1 ))
        fi
        if [[ "$do_fetch" == "1" ]] && [[ -n "$sym" ]]; then
            price=$(fetch_price "$sym")
            if [[ -n "$price" ]]; then
                r=$(compute_r "$side" "$entry" "$stop" "$price")
                if [[ -n "$r" ]]; then
                    # Track best R > 2.0
                    is_better=$(awk -v r="$r" -v best="${best_r:-0}" \
                        'BEGIN{print (r+0 > best+0 && r+0 > 2.0) ? 1 : 0}')
                    if [[ "$is_better" == "1" ]]; then
                        best_r="$r"
                        best_sym="$sym"
                        best_side="$side"
                    fi
                fi
            fi
        fi
    done <<< "$open_data"

    if [[ "$n_open" -eq 0 ]]; then
        return
    fi

    # Build open line — show count breakdown + symbol list
    local parts=""
    if [[ "$n_long" -gt 0 ]] && [[ "$n_short" -gt 0 ]]; then
        parts="${n_long} LONG, ${n_short} SHORT"
    elif [[ "$n_long" -gt 0 ]]; then
        parts="${n_long} LONG"
    elif [[ "$n_short" -gt 0 ]]; then
        parts="${n_short} SHORT"
    fi

    local open_line="  Open: ${n_open} (${parts}) [${sym_list% }]"
    if [[ -n "$best_r" ]]; then
        open_line="${open_line} | best ${best_sym} ${best_side} R=${best_r}"
    fi
    echo "$open_line"
}

# ── Compute days elapsed and trade count from live cohort ────────────────────
get_live_stats_for_verdict() {
    local -a files=("$@")
    if [[ ${#files[@]} -eq 0 ]]; then
        echo "0	0"
        return
    fi
    cat "${files[@]}" \
    | jq -r 'select(.event=="close") | [.ts, (.outcome // "")] | @tsv' 2>/dev/null \
    | awk -F'\t' '
        BEGIN { first_ts=""; total=0 }
        {
            outcome=$2
            if (outcome!="PARTIAL") {
                total++
                if (first_ts=="") first_ts=$1
            }
        }
        END { printf "%s\t%d\n", first_ts, total }
    '
}

# ── Determine if we should fetch prices ─────────────────────────────────────
do_fetch="0"
if [[ "$DRY_RUN" != "1" ]] || [[ "$FETCH_PRICES" == "1" ]]; then
    do_fetch="1"
fi

# ── Process live cohort ──────────────────────────────────────────────────────
shopt -s nullglob
live_files=( "${JOURNAL_DIR}"/*-*.jsonl )

live_raw=""
live_trades=0; live_wins=0; live_pnl=0
live_open_data=""

if [[ ${#live_files[@]} -gt 0 ]]; then
    live_raw=$(parse_cohort_full "${live_files[@]}")
    stats_line=$(echo "$live_raw" | grep "^STATS" | head -1)
    if [[ -n "$stats_line" ]]; then
        live_trades=$(echo "$stats_line" | awk -F'\t' '{print $2}')
        live_wins=$(echo   "$stats_line" | awk -F'\t' '{print $3}')
        live_pnl=$(echo    "$stats_line" | awk -F'\t' '{print $4}')
    fi
    live_open_data=$(echo "$live_raw" | grep "^OPEN" || true)
fi

# ── Get live first-close timestamp for verdict ───────────────────────────────
live_first_ts=""
live_days=0
if [[ ${#live_files[@]} -gt 0 ]]; then
    live_ts_data=$(get_live_stats_for_verdict "${live_files[@]}")
    live_first_ts=$(echo "$live_ts_data" | awk -F'\t' '{print $1}')
    if [[ -n "$live_first_ts" ]]; then
        NOW_EPOCH=$(date -u +%s)
        first_epoch=$(date -d "${live_first_ts}" +%s 2>/dev/null || \
                      date -u -j -f "%Y-%m-%dT%H:%M:%SZ" "${live_first_ts%%.*}Z" +%s 2>/dev/null || \
                      echo "$NOW_EPOCH")
        live_days=$(( (NOW_EPOCH - first_epoch) / 86400 ))
    fi
fi

# ── Build live section ───────────────────────────────────────────────────────
build_cohort_section() {
    local label="$1"
    local trades="$2" wins="$3" pnl="$4"
    local open_data="$5"
    local do_fetch_arg="$6"

    local header="$label"

    local pnl_int
    pnl_int=$(awk -v p="$pnl" 'BEGIN{printf "%+.0f", p}')

    if [[ "$trades" -eq 0 ]] && [[ -z "$open_data" ]]; then
        printf "%s\n  no trades yet\n" "$header"
        return
    fi

    if [[ "$trades" -gt 0 ]]; then
        local wr_pct
        wr_pct=$(awk -v w="$wins" -v t="$trades" 'BEGIN{printf "%.1f", w*100/t}')
        printf "%s\n  %d trades | WR %s%% | PnL \$%s\n" \
            "$header" "$trades" "$wr_pct" "$pnl_int"
    else
        printf "%s\n  0 trades closed\n" "$header"
    fi

    if [[ -n "$open_data" ]]; then
        format_open_line "$open_data" "$do_fetch_arg"
    fi
}

MSG="Daily Digest — ${TODAY}"
MSG+=$'\n'

# Live section
live_section=$(build_cohort_section "LIVE" "$live_trades" "$live_wins" "$live_pnl" \
    "$live_open_data" "$do_fetch")
MSG+=$'\n'"$live_section"

# ── Process shadow cohorts ───────────────────────────────────────────────────
shadow_base="${JOURNAL_DIR}/shadow"
if [[ -d "$shadow_base" ]]; then
    for label_dir in "${shadow_base}"/*/; do
        [[ -d "$label_dir" ]] || continue
        label_name=$(basename "$label_dir")
        # Uppercase + dashes → spaces (alt5-15-336 → ALT5 15 336)
        label_upper=$(echo "$label_name" | tr '[:lower:]' '[:upper:]' | tr '-' ' ')

        shadow_files=( "${label_dir}"*-*.jsonl )

        if [[ ${#shadow_files[@]} -eq 0 ]]; then
            MSG+=$'\n'"${label_upper}"$'\n'"  no trades yet"$'\n'
            continue
        fi

        shadow_raw=$(parse_cohort_full "${shadow_files[@]}")
        s_trades=0; s_wins=0; s_pnl=0; s_open_data=""

        stats_line=$(echo "$shadow_raw" | grep "^STATS" | head -1)
        if [[ -n "$stats_line" ]]; then
            s_trades=$(echo "$stats_line" | awk -F'\t' '{print $2}')
            s_wins=$(echo   "$stats_line" | awk -F'\t' '{print $3}')
            s_pnl=$(echo    "$stats_line" | awk -F'\t' '{print $4}')
        fi
        s_open_data=$(echo "$shadow_raw" | grep "^OPEN" || true)

        shadow_section=$(build_cohort_section "$label_upper" \
            "$s_trades" "$s_wins" "$s_pnl" "$s_open_data" "$do_fetch")
        MSG+=$'\n'"$shadow_section"
    done
fi

# ── Verdict line ─────────────────────────────────────────────────────────────
MIN_TRADES=150
MIN_DAYS=60

verdict_trades=$live_trades
verdict_days=$live_days

MSG+=$'\n'
MSG+="Verdict: WAITING (day ${verdict_days}/${MIN_DAYS}, ${verdict_trades}/${MIN_TRADES} trades)"

# ── Trade-rate staleness check ──────────────────────────────────────────────
# If no journal event (open or close) across ALL live symbols for >48h,
# fire a separate WARN. Catches silent engine deaths between weekly audits.
STALE_HOURS=48
STALE_THRESHOLD_S=$(( STALE_HOURS * 3600 ))

newest_event_ts=""
if [[ ${#live_files[@]} -gt 0 ]]; then
    newest_event_ts=$(cat "${live_files[@]}" \
        | jq -r '.ts // empty' 2>/dev/null \
        | sort | tail -1)
fi

if [[ -n "$newest_event_ts" ]]; then
    NOW_EPOCH_STALE=$(date -u +%s)
    newest_epoch=$(date -d "${newest_event_ts}" +%s 2>/dev/null || \
                   date -u -j -f "%Y-%m-%dT%H:%M:%SZ" "${newest_event_ts%%.*}Z" +%s 2>/dev/null || \
                   echo "$NOW_EPOCH_STALE")
    gap_h=$(( (NOW_EPOCH_STALE - newest_epoch) / 3600 ))

    if [[ $gap_h -ge $STALE_HOURS ]]; then
        stale_msg="No journal events across all 16 live symbols for ${gap_h}h (threshold: ${STALE_HOURS}h).
Last event: ${newest_event_ts}
Possible causes: all engines down, no signals firing (regime-dependent), journal write failure.
Check: ssh root@178.105.24.230 'systemctl status paper-live@*.service'"
        MSG+=$'\n'"TRADE RATE: STALE (${gap_h}h silent)"
        if [[ "$DRY_RUN" != "1" ]]; then
            notify_telegram WARN "daily_digest: fleet silent ${gap_h}h" "$stale_msg"
        fi
    fi
elif [[ ${#live_files[@]} -eq 0 ]]; then
    MSG+=$'\n'"TRADE RATE: no journals found"
fi

# ── Output ───────────────────────────────────────────────────────────────────
if [[ "$DRY_RUN" == "1" ]]; then
    echo "$MSG"
else
    notify_telegram INFO "daily_digest" "$MSG"
fi
