#!/usr/bin/env bash
# monitor.sh — One-stop dashboard for the deployed paper-live fleet on VPS.
# Shows engine health, trade counts (split into backfill-replay vs live),
# aggregate WR/PnL, and currently-open positions.
#
# Usage:
#   ./scripts/monitor.sh                       # default VPS, current month
#   ./scripts/monitor.sh root@<host>           # custom host
#   ./scripts/monitor.sh --full                # adds last 10 closed trades per symbol
#
# Re-reads the deploy timestamp from systemd ActiveEnterTimestamp on every run,
# so the live-vs-backfill split stays correct after every redeploy.
set -uo pipefail

TARGET="root@178.105.24.230"
MODE="brief"

for arg in "$@"; do
    case "$arg" in
        root@*|*@*) TARGET="$arg" ;;
        --full|-f)  MODE="full" ;;
        --help|-h)
            sed -n '2,11p' "$0" | sed 's/^# //; s/^#//'; exit 0 ;;
    esac
done

# 16 deployed symbols (kept in sync with deploy/redeploy.sh and scripts/paper_live_watchdog.sh)
SYMBOLS_LC=(btcusdt ethusdt solusdt runeusdt linkusdt xlmusdt apeusdt enjusdt
            tiausdt zilusdt ldousdt bnbusdt hbarusdt iotausdt grtusdt trxusdt)

REMOTE_SCRIPT='
set -uo pipefail
SYMBOLS_LC=($SYMBOLS_LC_STR)
MONTH=$(date -u "+%Y-%m")
JOURNAL_DIR=/var/log/paper-live/journal
LOG_DIR=/var/log/paper-live
MODE="$MODE"
BOLD=$(tput bold 2>/dev/null || true); DIM=$(tput dim 2>/dev/null || true)
GREEN=$(tput setaf 2 2>/dev/null || true); RED=$(tput setaf 1 2>/dev/null || true)
YELLOW=$(tput setaf 3 2>/dev/null || true); BLUE=$(tput setaf 4 2>/dev/null || true)
RESET=$(tput sgr0 2>/dev/null || true)

# Latest deploy = max ActiveEnterTimestamp across all paper-live services.
DEPLOY_TS=$(for s in "${SYMBOLS_LC[@]}"; do
    systemctl show "paper-live@${s}.service" --property=ActiveEnterTimestamp --value 2>/dev/null
done | grep -v "^$" | while read -r ts; do date -d "$ts" -u "+%Y-%m-%dT%H:%M:%SZ" 2>/dev/null; done | sort | tail -1)

NOW=$(date -u "+%Y-%m-%d %H:%M:%S UTC")
echo
echo "${BOLD}═══ trading-engine paper-live dashboard ═══${RESET}"
echo "  host:        $(hostname)"
echo "  time (UTC):  ${NOW}"
echo "  deploy_ts:   ${DEPLOY_TS}"
echo "  journal:     ${MONTH}"
echo

# ── Engine health ─────────────────────────────────────────────────────────────
echo "${BOLD}── ENGINE HEALTH ──${RESET}"
n_active=0; n_total=${#SYMBOLS_LC[@]}; n_stale=0
for s in "${SYMBOLS_LC[@]}"; do
    state=$(systemctl is-active "paper-live@${s}.service" 2>/dev/null)
    log="${LOG_DIR}/${s}.log"
    log_age=$(( $(date +%s) - $(stat -c %Y "$log" 2>/dev/null || echo 0) ))
    if [[ "$state" == "active" ]] && (( log_age <= 60 )); then
        n_active=$((n_active+1))
        flag="${GREEN}●${RESET}"
    elif [[ "$state" == "active" ]]; then
        n_stale=$((n_stale+1))
        flag="${YELLOW}●${RESET}"
        printf "  %s  %-12s  %-10s  log_age=%ds  ${YELLOW}(stale log)${RESET}\n" "$flag" "$s" "$state" "$log_age"
        continue
    else
        flag="${RED}●${RESET}"
        printf "  %s  %-12s  %-10s  ${RED}DEAD${RESET}\n" "$flag" "$s" "$state"
        continue
    fi
done
if (( n_active == n_total )) && (( n_stale == 0 )); then
    echo "  ${GREEN}✓ all ${n_total} engines healthy${RESET}"
else
    echo "  ${YELLOW}${n_active}/${n_total} active${RESET}"
fi
echo

# ── Trade summary (per symbol) ─────────────────────────────────────────────────
echo "${BOLD}── TRADES SINCE DEPLOY  (deploy: ${DEPLOY_TS}) ──${RESET}"
printf "  %-10s %5s %5s %5s %5s %5s %12s %12s\n" \
    "Symbol" "open" "close" "wins" "loss" "live" "live_pnl" "total_pnl"
printf "  %-10s %5s %5s %5s %5s %5s %12s %12s\n" \
    "------" "----" "-----" "----" "----" "----" "--------" "---------"
for s_lc in "${SYMBOLS_LC[@]}"; do
    s_uc=$(echo "$s_lc" | tr "[:lower:]" "[:upper:]")
    jrnl="${JOURNAL_DIR}/${s_uc}-${MONTH}.jsonl"
    if [[ ! -f "$jrnl" ]]; then
        printf "  %-10s ${DIM}(no journal yet)${RESET}\n" "$s_uc"
        continue
    fi
    read -r opens closes wins loss live live_pnl total_pnl <<< $(jq -rs --arg ts "$DEPLOY_TS" "
        {opens:    [.[] | select(.event==\"open\")] | length,
         closes:   [.[] | select(.event==\"close\")] | length,
         wins:     [.[] | select(.event==\"close\" and .outcome==\"TARGET\")] | length,
         loss:     [.[] | select(.event==\"close\" and .outcome==\"STOP\")]   | length,
         live_op:  [.[] | select(.event==\"open\"  and .ts >= \$ts)] | length,
         live_pnl: ([.[] | select(.event==\"close\" and .ts >= \$ts) | .pnl_usd] | add // 0),
         total_pnl: ([.[] | select(.event==\"close\") | .pnl_usd] | add // 0)}
        | \"\(.opens) \(.closes) \(.wins) \(.loss) \(.live_op) \(.live_pnl) \(.total_pnl)\"
    " "$jrnl")
    pnl_color=""
    if awk "BEGIN{exit !($live_pnl > 0)}"; then pnl_color="$GREEN"; fi
    if awk "BEGIN{exit !($live_pnl < 0)}"; then pnl_color="$RED"; fi
    printf "  %-10s %5d %5d %5d %5d %5d ${pnl_color}%+12.2f${RESET} %+12.2f\n" \
        "$s_uc" "$opens" "$closes" "$wins" "$loss" "$live" "$live_pnl" "$total_pnl"
done
echo

# ── Aggregates ─────────────────────────────────────────────────────────────────
echo "${BOLD}── AGGREGATES ──${RESET}"
agg_files=()
for s_lc in "${SYMBOLS_LC[@]}"; do
    s_uc=$(echo "$s_lc" | tr "[:lower:]" "[:upper:]")
    f="${JOURNAL_DIR}/${s_uc}-${MONTH}.jsonl"
    [[ -f "$f" ]] && agg_files+=("$f")
done
if (( ${#agg_files[@]} > 0 )); then
    jq -rs --arg ts "$DEPLOY_TS" "
      def fmt(n): if n >= 0 then \"+\\(n|tostring)\" else (n|tostring) end;
      {
        total_opens:  [.[] | select(.event==\"open\")]  | length,
        total_closes: [.[] | select(.event==\"close\")] | length,
        live_opens:   [.[] | select(.event==\"open\"  and .ts >= \$ts)] | length,
        live_closes:  [.[] | select(.event==\"close\" and .ts >= \$ts)] | length,
        total_wins:   [.[] | select(.event==\"close\" and .outcome==\"TARGET\")] | length,
        total_loss:   [.[] | select(.event==\"close\" and .outcome==\"STOP\")]   | length,
        live_wins:    [.[] | select(.event==\"close\" and .outcome==\"TARGET\" and .ts >= \$ts)] | length,
        live_loss:    [.[] | select(.event==\"close\" and .outcome==\"STOP\"   and .ts >= \$ts)] | length,
        live_pnl:     ([.[] | select(.event==\"close\" and .ts >= \$ts) | .pnl_usd] | add // 0),
        total_pnl:    ([.[] | select(.event==\"close\") | .pnl_usd] | add // 0)
      }
      | \"  total opens:    \\(.total_opens) (live: \\(.live_opens), backfill replay: \\(.total_opens - .live_opens))
  total closes:   \\(.total_closes) (live: \\(.live_closes))
  win/loss:       all=\\(.total_wins)/\\(.total_loss)  live=\\(.live_wins)/\\(.live_loss)
  win rate:       all=\\(if .total_closes>0 then ((.total_wins*100/.total_closes)|tostring|.[0:5]) else \"–\" end)%  live=\\(if .live_closes>0 then ((.live_wins*100/.live_closes)|tostring|.[0:5]) else \"–\" end)%
  PnL (USD):      live=\\(.live_pnl|tostring|.[0:9])  total=\\(.total_pnl|tostring|.[0:9])\"
    " "${agg_files[@]}"
else
    echo "  (no journal files yet for ${MONTH})"
fi
echo

# ── Currently open positions ──────────────────────────────────────────────────
echo "${BOLD}── CURRENTLY OPEN POSITIONS ──${RESET}"
any_open=false
for s_lc in "${SYMBOLS_LC[@]}"; do
    s_uc=$(echo "$s_lc" | tr "[:lower:]" "[:upper:]")
    jrnl="${JOURNAL_DIR}/${s_uc}-${MONTH}.jsonl"
    [[ -f "$jrnl" ]] || continue
    last_event=$(jq -rs ".[] | .event" "$jrnl" 2>/dev/null | tail -1)
    if [[ "$last_event" == "open" ]]; then
        any_open=true
        line=$(jq -rs ".[] | select(.event==\"open\") | [.ts,.side,.entry,.target,.stop,.reason] | @tsv" "$jrnl" | tail -1)
        IFS=$'\''\t'\'' read -r ts side entry target stop reason <<< "$line"
        printf "  %-10s  %s  %-5s  entry=%-9s target=%-9s stop=%s\n" \
            "$s_uc" "${ts:0:19}" "$side" "$entry" "$target" "$stop"
    fi
done
$any_open || echo "  ${DIM}(none — all positions closed)${RESET}"
echo

# ── Recent closed trades (--full mode only) ───────────────────────────────────
if [[ "$MODE" == "full" ]]; then
    echo "${BOLD}── LAST 5 CLOSED TRADES PER SYMBOL ──${RESET}"
    for s_lc in "${SYMBOLS_LC[@]}"; do
        s_uc=$(echo "$s_lc" | tr "[:lower:]" "[:upper:]")
        jrnl="${JOURNAL_DIR}/${s_uc}-${MONTH}.jsonl"
        [[ -f "$jrnl" ]] || continue
        recent=$(jq -rs --arg ts "$DEPLOY_TS" "
            [.[] | select(.event==\"close\" and .ts >= \$ts)] | sort_by(.ts) | reverse | .[0:5] |
            .[] | [(.ts|.[0:19]), .outcome, (.entry|tostring), (.exit|tostring), (.pnl_usd|tostring|.[0:8])] | @tsv
        " "$jrnl")
        if [[ -n "$recent" ]]; then
            echo "  ${BOLD}${s_uc}${RESET}"
            while IFS=$'\''\t'\'' read -r ts outc entry exit_ pnl; do
                [[ -z "$ts" ]] && continue
                color="$GREEN"; [[ "$outc" == "STOP" ]] && color="$RED"
                printf "    %s  ${color}%-6s${RESET}  entry=%-9s exit=%-9s  pnl=%s\n" "$ts" "$outc" "$entry" "$exit_" "$pnl"
            done <<< "$recent"
        fi
    done
    echo
fi
'

# Bash heredoc — substitute env vars and ssh in
ssh "$TARGET" "SYMBOLS_LC_STR='${SYMBOLS_LC[*]}' MODE='$MODE' bash -s" <<<"$REMOTE_SCRIPT"
