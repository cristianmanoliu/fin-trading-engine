#!/usr/bin/env bash
# daily_digest.sh — rich Telegram summary of forward-paper status.
#
# Runs on VPS via cron at 09:00 UTC. Produces a single-message digest with:
#   - LIVE: trades (with delta from yesterday), W/L, PnL, named wins,
#     open positions with entry prices, WR vs backtest, power-floor countdown
#   - Shadows: comparison table with divergence detection
#   - New-trade narrative (trades since last digest)
#
# Persists a snapshot after each run so the next run can compute deltas.
#
# Usage:
#   DRY_RUN=1 JOURNAL_DIR=./logs/journal ./scripts/daily_digest.sh
#   ./scripts/daily_digest.sh          # reads JOURNAL_DIR, sends via notify_telegram
#
# Environment:
#   JOURNAL_DIR      — journal root (default: /var/log/paper-live/journal)
#   SNAPSHOT_DIR     — where to persist daily snapshots (default: /var/log/paper-live/digest_snapshots)
#   DRY_RUN=1        — print to stdout instead of sending Telegram
#   FETCH_PRICES=1   — force price fetch even in DRY_RUN (default: skip in DRY_RUN)
#
# Exit codes:
#   0  — success (message sent or printed)
#   1  — JOURNAL_DIR not found
#   2  — bash < 4 (script uses associative arrays / declare -A)
set -euo pipefail

# Hard-require bash 4+ — script uses `declare -A` for shadow comparison.
# Production runs on Hetzner Ubuntu (bash 5); fail-fast with a clear message
# on older shells (e.g. macOS /bin/bash is 3.2) rather than cryptic
# `declare: -A: invalid option` mid-script.
if [[ "${BASH_VERSINFO[0]:-0}" -lt 4 ]]; then
    echo "daily_digest.sh requires bash 4+ (current: ${BASH_VERSION})" >&2
    echo "On macOS: brew install bash && /opt/homebrew/bin/bash $0" >&2
    exit 2
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LIB="${SCRIPT_DIR}/lib/notify.sh"

# shellcheck source=lib/notify.sh
source "$LIB"

JOURNAL_DIR="${JOURNAL_DIR:-/var/log/paper-live/journal}"
# Preserve whether the caller pinned SNAPSHOT_DIR so DRY_RUN can isolate
# its write target without trampling an explicit override.
_SNAPSHOT_DIR_USER_SET="${SNAPSHOT_DIR+1}"
SNAPSHOT_DIR="${SNAPSHOT_DIR:-/var/log/paper-live/digest_snapshots}"
DRY_RUN="${DRY_RUN:-0}"
FETCH_PRICES="${FETCH_PRICES:-0}"

# DRY_RUN must not mutate production state. The snapshot write at the end
# of the script is unconditional (by design — the production cron needs it
# to compute next-day deltas). If a caller runs DRY_RUN against the
# production SNAPSHOT_DIR after the daily cron has fired, they overwrite
# the day's baseline and next morning's "24h delta" lies by whatever
# closed between cron and the DRY_RUN. Redirect to a tmpdir unless the
# caller explicitly pinned SNAPSHOT_DIR (test harness pattern).
if [[ "$DRY_RUN" == "1" ]] && [[ -z "$_SNAPSHOT_DIR_USER_SET" ]]; then
    # Portable form: `mktemp -d -t prefix.XXXXXX` on BSD/macOS treats the
    # template as a LITERAL prefix and appends extra entropy, so the path
    # gains an unwanted `.XXXXXX` segment. The explicit `${TMPDIR:-/tmp}/`
    # path keeps macOS local + Linux CI behaviour identical.
    SNAPSHOT_DIR="$(mktemp -d "${TMPDIR:-/tmp}/daily_digest_dryrun.XXXXXX")"
    trap 'rm -rf "$SNAPSHOT_DIR"' EXIT
fi

TODAY="$(date -u '+%Y-%m-%d')"

# Forward-paper reference values
BACKTEST_WR="20.6"
MIN_TRADES=150
MIN_DAYS=60
FORWARD_START="2026-05-05"

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

# ── Ensure snapshot dir exists ───────────────────────────────────────────────
mkdir -p "$SNAPSHOT_DIR"

# ── Check for any journal files ──────────────────────────────────────────────
shopt -s nullglob
all_journals=( "${JOURNAL_DIR}"/*-*.jsonl )
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

# ── Helper: parse one cohort's journal files (full detail) ──────────────────
# Produces:
#   STATS\ttrades\twins\tpnl_usd
#   OPEN\tsymbol\tside\tentry\tstop\ttarget
#   WIN\tsymbol\tpnl_usd\tts\tmfe_r
#   NEWTRADE\tsymbol\tside\tpnl_usd\toutcome\tts\tmfe_r
parse_cohort_full() {
    local -a files=("$@")
    if [[ ${#files[@]} -eq 0 ]]; then
        return
    fi
    cat "${files[@]}" \
    | jq -r 'select(.event=="open" or .event=="close")
              | [.event, (.symbol // ""), (.pnl_usd // 0), (.outcome // ""), (.side // ""),
                 (.entry // 0), (.stop // 0), (.target // 0), (.ts // ""), (.mfe_r // 0)]
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
                if (outcome=="TARGET") {
                    wins++
                    printf "WIN\t%s\t%s\t%s\t%s\n", sym, $3, $9, $10
                }
                pnl += $3
            }
            printf "NEWTRADE\t%s\t%s\t%s\t%s\t%s\t%s\n", sym, $5, $3, outcome, $9, $10
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

# ── Determine if we should fetch prices ─────────────────────────────────────
do_fetch="0"
if [[ "$DRY_RUN" != "1" ]] || [[ "$FETCH_PRICES" == "1" ]]; then
    do_fetch="1"
fi

# ── Load yesterday's snapshot ────────────────────────────────────────────────
YESTERDAY="$(date -u -d "yesterday" '+%Y-%m-%d' 2>/dev/null || \
             date -u -v-1d '+%Y-%m-%d' 2>/dev/null || echo "")"
PREV_SNAPSHOT="${SNAPSHOT_DIR}/digest_${YESTERDAY}.json"
prev_live_trades=0; prev_live_pnl=0
prev_shadow_data=""
if [[ -n "$YESTERDAY" ]] && [[ -f "$PREV_SNAPSHOT" ]]; then
    prev_live_trades=$(jq -r '.live_trades // 0' "$PREV_SNAPSHOT" 2>/dev/null || echo 0)
    prev_live_pnl=$(jq -r '.live_pnl // 0' "$PREV_SNAPSHOT" 2>/dev/null || echo 0)
    prev_shadow_data=$(jq -r '.shadows // empty' "$PREV_SNAPSHOT" 2>/dev/null || echo "")
fi

# ── Process live cohort ──────────────────────────────────────────────────────
live_files=( "${JOURNAL_DIR}"/*-*.jsonl )

live_raw=""
live_trades=0; live_wins=0; live_pnl=0
live_open_data=""
live_win_data=""
live_new_trades=""

if [[ ${#live_files[@]} -gt 0 ]]; then
    live_raw=$(parse_cohort_full "${live_files[@]}")
    stats_line=$(echo "$live_raw" | grep "^STATS" | head -1)
    if [[ -n "$stats_line" ]]; then
        live_trades=$(echo "$stats_line" | awk -F'\t' '{print $2}')
        live_wins=$(echo   "$stats_line" | awk -F'\t' '{print $3}')
        live_pnl=$(echo    "$stats_line" | awk -F'\t' '{print $4}')
    fi
    live_open_data=$(echo "$live_raw" | grep "^OPEN" || true)
    live_win_data=$(echo "$live_raw" | grep "^WIN" || true)
    live_new_trades=$(echo "$live_raw" | grep "^NEWTRADE" || true)
fi

# ── Compute days elapsed ────────────────────────────────────────────────────
# Canonical anchor per results/time_anchor_resolution_2026-05-12.md is
# first close (NOT deploy date). Matches stage_promotion_check.py and
# forward_paper_status.sh aggregate. Sole exception path: when no closes
# exist yet, fall back to FORWARD_START so the digest still produces a
# meaningful pre-first-close day-count.
NOW_EPOCH=$(date -u +%s)
first_close_ts=""
if [[ ${#live_files[@]} -gt 0 ]] && [[ -f "${live_files[0]}" ]]; then
    first_close_ts=$(cat "${live_files[@]}" 2>/dev/null \
        | jq -r 'select(.event=="close") | .ts' 2>/dev/null \
        | sort | head -1)
fi
if [[ -n "$first_close_ts" ]]; then
    start_epoch=$(date -d "$first_close_ts" +%s 2>/dev/null || \
                  date -u -j -f "%Y-%m-%dT%H:%M:%SZ" "${first_close_ts%%.*}Z" +%s 2>/dev/null || \
                  echo "$NOW_EPOCH")
else
    # No closes yet — fall back to deploy date so day-count isn't zero.
    start_epoch=$(date -d "${FORWARD_START}" +%s 2>/dev/null || \
                  date -u -j -f "%Y-%m-%d" "${FORWARD_START}" +%s 2>/dev/null || \
                  echo "$NOW_EPOCH")
fi
live_days=$(( (NOW_EPOCH - start_epoch) / 86400 ))

# ── Build LIVE section ──────────────────────────────────────────────────────
MSG="Forward-Paper Digest — ${TODAY} (day ${live_days})"
MSG+=$'\n'

# Closed trades with delta
delta_trades=""
if [[ "$prev_live_trades" -gt 0 ]]; then
    new_count=$(( live_trades - prev_live_trades ))
    if [[ "$new_count" -gt 0 ]]; then
        delta_trades=" (+${new_count} new)"
    elif [[ "$new_count" -eq 0 ]]; then
        delta_trades=" (unchanged)"
    fi
fi

losses=$(( live_trades - live_wins ))
live_pnl_int=$(awk -v p="$live_pnl" 'BEGIN{printf "%+.0f", p}')

MSG+=$'\n'"LIVE"
MSG+=$'\n'"  Trades: ${live_trades}${delta_trades} (${live_wins}W/${losses}L)"
MSG+=$'\n'"  PnL: \$${live_pnl_int}"

# PnL delta from yesterday
if [[ "$prev_live_trades" -gt 0 ]]; then
    pnl_delta=$(awk -v now="$live_pnl" -v prev="$prev_live_pnl" 'BEGIN{printf "%+.0f", now-prev}')
    MSG+=" (24h: \$${pnl_delta})"
fi

# Win rate vs backtest
if [[ "$live_trades" -gt 0 ]]; then
    wr_pct=$(awk -v w="$live_wins" -v t="$live_trades" 'BEGIN{printf "%.1f", w*100/t}')
    MSG+=$'\n'"  WR: ${wr_pct}% (backtest: ${BACKTEST_WR}%, n=${live_trades} is noise)"
fi

# Named wins
if [[ -n "$live_win_data" ]]; then
    win_list=""
    while IFS=$'\t' read -r _tag sym pnl_usd ts mfe_r; do
        [[ "$_tag" != "WIN" ]] && continue
        pnl_fmt=$(awk -v p="$pnl_usd" 'BEGIN{printf "%+.0f", p}')
        win_list="${win_list}${sym} \$${pnl_fmt}, "
    done <<< "$live_win_data"
    if [[ -n "$win_list" ]]; then
        MSG+=$'\n'"  Wins: ${win_list%, }"
    fi
fi

# Open positions with entry prices and R-multiples
if [[ -n "$live_open_data" ]]; then
    open_count=0
    open_lines=""
    while IFS=$'\t' read -r _tag sym side entry stop target; do
        [[ "$_tag" != "OPEN" ]] && continue
        open_count=$(( open_count + 1 ))
        r_str=""
        if [[ "$do_fetch" == "1" ]] && [[ -n "$sym" ]]; then
            price=$(fetch_price "$sym")
            if [[ -n "$price" ]]; then
                r=$(compute_r "$side" "$entry" "$stop" "$price")
                [[ -n "$r" ]] && r_str=" R=${r}"
            fi
        fi
        open_lines="${open_lines}    ${sym} ${side} @ ${entry}${r_str}"$'\n'
    done <<< "$live_open_data"
    if [[ "$open_count" -gt 0 ]]; then
        MSG+=$'\n'"  Open: ${open_count}"
        MSG+=$'\n'"${open_lines%$'\n'}"
    fi
fi

# New trades since yesterday (narrative)
if [[ -n "$live_new_trades" ]] && [[ -n "$YESTERDAY" ]]; then
    new_narrative=""
    while IFS=$'\t' read -r _tag sym side pnl_usd outcome ts mfe_r; do
        [[ "$_tag" != "NEWTRADE" ]] && continue
        trade_date="${ts:0:10}"
        if [[ "$trade_date" == "$TODAY" ]] || [[ "$trade_date" == "$YESTERDAY" ]]; then
            pnl_fmt=$(awk -v p="$pnl_usd" 'BEGIN{printf "%+.0f", p}')
            mfe_str=""
            if [[ -n "$mfe_r" ]] && [[ "$mfe_r" != "0" ]]; then
                mfe_str=" MFE=${mfe_r}R"
            fi
            new_narrative="${new_narrative}  ${sym} ${side} ${outcome} \$${pnl_fmt}${mfe_str}"$'\n'
        fi
    done <<< "$live_new_trades"
    if [[ -n "$new_narrative" ]]; then
        MSG+=$'\n'"New (24h):"
        MSG+=$'\n'"${new_narrative%$'\n'}"
    fi
fi

# Power-floor countdown
days_remaining=$(( MIN_DAYS - live_days ))
(( days_remaining < 0 )) && days_remaining=0
trades_remaining=$(( MIN_TRADES - live_trades ))
(( trades_remaining < 0 )) && trades_remaining=0

if [[ "$live_days" -gt 0 ]] && [[ "$live_trades" -gt 0 ]]; then
    rate=$(awk -v t="$live_trades" -v d="$live_days" 'BEGIN{r=t/d; if(r<=0) r=0.01; printf "%.2f", r}')
    days_for_trades=$(awk -v tr="$trades_remaining" -v r="$rate" 'BEGIN{printf "%.0f", tr/r}')
else
    rate="0.00"
    days_for_trades="?"
fi

binding="$days_remaining"
binding_label="days"
if [[ "$days_for_trades" != "?" ]] && [[ "$days_for_trades" -gt "$binding" ]]; then
    binding="$days_for_trades"
    binding_label="trades"
fi

if [[ "$binding" -gt 0 ]]; then
    gate_epoch=$(( NOW_EPOCH + binding * 86400 ))
    gate_date=$(date -u -r "$gate_epoch" '+%Y-%m-%d' 2>/dev/null || \
                date -u -d "@$gate_epoch" '+%Y-%m-%d' 2>/dev/null || echo "?")
    MSG+=$'\n'"Countdown: ${live_trades}/${MIN_TRADES} trades, day ${live_days}/${MIN_DAYS}"
    MSG+=" (~${gate_date}, bound by ${binding_label})"
fi

# ── Process shadow cohorts ───────────────────────────────────────────────────
shadow_base="${JOURNAL_DIR}/shadow"
shadow_json_parts=""

if [[ -d "$shadow_base" ]]; then
    MSG+=$'\n'
    MSG+=$'\n'"SHADOWS"

    # Collect shadow data for comparison
    declare -A shadow_trades shadow_pnl shadow_wins

    for label_dir in "${shadow_base}"/*/; do
        [[ -d "$label_dir" ]] || continue
        label_name=$(basename "$label_dir")
        label_upper=$(echo "$label_name" | tr '[:lower:]' '[:upper:]' | tr '-' ' ')

        shadow_files=( "${label_dir}"*-*.jsonl )
        if [[ ${#shadow_files[@]} -eq 0 ]]; then
            MSG+=$'\n'"  ${label_upper}: no trades yet"
            continue
        fi

        shadow_raw=$(parse_cohort_full "${shadow_files[@]}")
        s_trades=0; s_wins=0; s_pnl=0; s_open_count=0

        stats_line=$(echo "$shadow_raw" | grep "^STATS" | head -1)
        if [[ -n "$stats_line" ]]; then
            s_trades=$(echo "$stats_line" | awk -F'\t' '{print $2}')
            s_wins=$(echo   "$stats_line" | awk -F'\t' '{print $3}')
            s_pnl=$(echo    "$stats_line" | awk -F'\t' '{print $4}')
        fi
        s_open_count=$(echo "$shadow_raw" | grep -c "^OPEN" || echo 0)

        shadow_trades[$label_name]=$s_trades
        shadow_pnl[$label_name]=$s_pnl
        shadow_wins[$label_name]=$s_wins

        s_pnl_int=$(awk -v p="$s_pnl" 'BEGIN{printf "%+.0f", p}')
        s_wr=""
        if [[ "$s_trades" -gt 0 ]]; then
            s_wr=$(awk -v w="$s_wins" -v t="$s_trades" 'BEGIN{printf "%.0f", w*100/t}')
            s_wr="${s_wr}%"
        fi

        MSG+=$'\n'"  ${label_upper}: ${s_trades} trades WR=${s_wr} PnL=\$${s_pnl_int} open=${s_open_count}"

        # Build JSON for snapshot
        shadow_json_parts="${shadow_json_parts}\"${label_name}\":{\"trades\":${s_trades},\"pnl\":${s_pnl},\"wins\":${s_wins}},"
    done

    # Divergence detection: delegate to shadow_divergence.sh ONELINE mode.
    # The harness (shipped 2026-05-18, scripts/shadow_divergence.sh) does
    # per-trade matching: MATCHED (byte-identical), DIVERGED (336h force-
    # close vs 504h natural exit), ORPHAN (one side only). The previous
    # inline check only compared aggregate trade-count + total PnL — it
    # would report "DIVERGED" if either side had ONE more closed trade
    # for any reason (e.g. a single race-condition close), and conversely
    # miss a divergence if total PnL happened to be equal by coincidence.
    # The harness's per-trade classifier is strictly more informative.
    div_script="${SCRIPT_DIR}/shadow_divergence.sh"
    if [[ -f "$div_script" ]]; then
        div_target="${SHADOW_DIVERGENCE_TARGET:-local}"
        # Run with JOURNAL_DIR override (already in environment). Suppress
        # stderr so a transient harness error doesn't pollute the Telegram
        # digest; `|| true` keeps daily_digest's set -e from tripping.
        div_line=$(JOURNAL_DIR="$JOURNAL_DIR" ONELINE=1 \
            bash "$div_script" "$div_target" 2>/dev/null || true)
        if [[ -n "$div_line" ]]; then
            MSG+=$'\n'"  ${div_line}"
        fi
    fi
fi

# ── Layer 3 shadow-parity (same-tick stub-primary + testnet-shadow) ─────────
# Per real_money_executor_architecture_decision_rule_2026-05-08.md, Layer 3
# is the formally-required gate before STAGE_1: BinanceLive(testnet) shadow
# running on the SAME tick stream as the existing Stub primary, for ≥7d.
# Verdict via scripts/layer3_verdict.sh. Enabled on a subset of live engines
# via deploy/systemd/layer3.conf drop-in (results/layer3_enablement_2026-05-19.md).
# Journal subdirectory: ${JOURNAL_DIR}/layer3/. Falls back to a "not active"
# line when no journal dir exists (Layer 3 not yet enabled).
LAYER3_DIR="${JOURNAL_DIR}/layer3"
LAYER3_MIN_DAYS=7
layer3_status=""
if [[ -d "$LAYER3_DIR" ]]; then
    layer3_files=( "${LAYER3_DIR}"/*-*.jsonl )
    if [[ ${#layer3_files[@]} -gt 0 ]] && [[ -f "${layer3_files[0]}" ]]; then
        # Count distinct symbols (= engines wrapped) + closed fills.
        n_engines=$(printf '%s\n' "${layer3_files[@]}" | xargs -n 1 basename 2>/dev/null \
            | awk -F'-' '{print $1}' | sort -u | wc -l | tr -d ' ')
        # jq -c writes each matching event as one compact line so `wc -l`
        # gives the actual event count (vs default pretty-print which
        # expands each event across N lines — CI hit "16 fill(s)" on a
        # 2-close fixture because of this 2026-05-19).
        n_fills=$(cat "${layer3_files[@]}" 2>/dev/null \
            | jq -c 'select(.event=="close" and .outcome!="PARTIAL")' 2>/dev/null | wc -l | tr -d ' ')
        # Earliest open event determines the 7d countdown.
        earliest_ts=$(cat "${layer3_files[@]}" 2>/dev/null \
            | jq -r 'select(.event=="open") | .ts' 2>/dev/null | sort | head -1)
        if [[ -n "$earliest_ts" ]]; then
            earliest_epoch=$(date -d "$earliest_ts" +%s 2>/dev/null || \
                             date -u -j -f "%Y-%m-%dT%H:%M:%SZ" "${earliest_ts%%.*}Z" +%s 2>/dev/null || \
                             echo "$NOW_EPOCH")
            days_in_window=$(( (NOW_EPOCH - earliest_epoch) / 86400 ))
            layer3_status="${n_engines} engine(s), ${n_fills} fill(s), ${days_in_window}/${LAYER3_MIN_DAYS}d window"
        else
            layer3_status="${n_engines} engine(s) wrapped, awaiting first event"
        fi
    else
        # Directory exists but empty — Layer 3 wrapper active but no events yet
        layer3_status="enabled, awaiting first event"
    fi
else
    layer3_status="not active"
fi

MSG+=$'\n'
MSG+=$'\n'"LAYER 3"
MSG+=$'\n'"  ${layer3_status}"

# ── Verdict line ─────────────────────────────────────────────────────────────
MSG+=$'\n'
MSG+="Verdict: WAITING (day ${live_days}/${MIN_DAYS}, ${live_trades}/${MIN_TRADES} trades)"

# ── Trade-rate staleness check ──────────────────────────────────────────────
STALE_HOURS=48
newest_event_ts=""
if [[ ${#live_files[@]} -gt 0 ]]; then
    newest_event_ts=$(cat "${live_files[@]}" \
        | jq -r '.ts // empty' 2>/dev/null \
        | sort | tail -1)
fi

if [[ -n "$newest_event_ts" ]]; then
    newest_epoch=$(date -d "${newest_event_ts}" +%s 2>/dev/null || \
                   date -u -j -f "%Y-%m-%dT%H:%M:%SZ" "${newest_event_ts%%.*}Z" +%s 2>/dev/null || \
                   echo "$NOW_EPOCH")
    gap_h=$(( (NOW_EPOCH - newest_epoch) / 3600 ))

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

# ── Save today's snapshot ────────────────────────────────────────────────────
SNAPSHOT_FILE="${SNAPSHOT_DIR}/digest_${TODAY}.json"
shadow_json="{"
if [[ -n "$shadow_json_parts" ]]; then
    shadow_json="${shadow_json}${shadow_json_parts%,}}"
else
    shadow_json="{}"
fi
cat > "$SNAPSHOT_FILE" <<SNAP
{"live_trades":${live_trades},"live_pnl":${live_pnl},"live_wins":${live_wins},"date":"${TODAY}","shadows":${shadow_json}}
SNAP

# ── Snapshot cleanup (keep 30 days) ──────────────────────────────────────────
find "$SNAPSHOT_DIR" -name "digest_*.json" -mtime +30 -delete 2>/dev/null || true

# ── Output ───────────────────────────────────────────────────────────────────
if [[ "$DRY_RUN" == "1" ]]; then
    echo "$MSG"
else
    notify_telegram INFO "daily_digest" "$MSG"
fi
