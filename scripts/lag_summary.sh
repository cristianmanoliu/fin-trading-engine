#!/usr/bin/env bash
# lag_summary.sh — fleet-wide source-to-receipt lag snapshot.
#
# Aggregates per-engine lag percentiles from the latest heartbeat in each
# /var/log/paper-live/<sym>.log. Surfaces:
#   - per-engine p50 / p99 / max / sample-count / tier
#   - fleet rollup (count per tier + max p99 + median p99)
#   - exit code matching the locked tier contract so cron can react
#
# Reads the lag fields written by pkg/marketdata/heartbeat.go (commit
# 033ed02). Engines pre-dating that commit emit no lag fields → NO_DATA
# tier, distinguished from genuine OK 0ms.
#
# Usage:
#   ./scripts/lag_summary.sh                            # default VPS
#   ./scripts/lag_summary.sh root@host                  # custom host
#   ./scripts/lag_summary.sh --quiet                    # 1-line out (cron)
#
# Exit codes:
#   0 HEALTHY      — no engine in DEGRADED / HIGH tier
#   1 DEGRADED     — ≥1 engine in DEGRADED tier (15s < p99 ≤ 30s)
#   2 HIGH         — ≥1 engine in HIGH tier (p99 > 30s)
#   3 SSH_FAILURE  — couldn't reach host or fetch heartbeats
#   4 INPUT_ERROR  — bad flags / missing dependencies
set -euo pipefail

# ─────────────────────────────────────────────────────────────────────
# Internal helpers (sourceable under BASH_SOURCE != $0 source guard)
# ─────────────────────────────────────────────────────────────────────

# classify_lag_ms — bucket a lag-percentile value (ms) into a verdict
# tier. Same contract as post_deploy_check.sh's helper of the same name;
# duplicated here intentionally rather than extracted to lib/ — both
# scripts have small helper sets and the abstraction would add a sourcing
# step without clear reuse benefit.
classify_lag_ms() {
    local lag="$1"
    if [[ -z "$lag" ]] || ! [[ "$lag" =~ ^-?[0-9]+$ ]]; then
        echo "NO_DATA"
        return
    fi
    if   [[ "$lag" -lt 0     ]]; then echo "NO_DATA"
    elif [[ "$lag" -le 1000  ]]; then echo "OK"
    elif [[ "$lag" -le 15000 ]]; then echo "TYPICAL"
    elif [[ "$lag" -le 30000 ]]; then echo "DEGRADED"
    else                              echo "HIGH"
    fi
}

# fleet_verdict — given counts per tier, return the verdict string +
# exit code. Pure function for testability. Verdict precedence:
# HIGH > DEGRADED > HEALTHY (NO_DATA does NOT downgrade — it's
# non-information, not bad-information).
fleet_verdict() {
    local high="$1" degraded="$2"
    if   [[ "$high"     -gt 0 ]]; then echo "HIGH|2"
    elif [[ "$degraded" -gt 0 ]]; then echo "DEGRADED|1"
    else                               echo "HEALTHY|0"
    fi
}

# Test entry-point: when sourced (BASH_SOURCE != $0), stop here.
if [[ "${BASH_SOURCE[0]}" != "${0}" ]]; then
    return 0
fi

# ─────────────────────────────────────────────────────────────────────
# Main flow
# ─────────────────────────────────────────────────────────────────────

QUIET=0
TARGET="root@178.105.24.230"
while [[ $# -gt 0 ]]; do
    case "$1" in
        --quiet) QUIET=1; shift ;;
        --help|-h) sed -n '2,28p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
        --*) echo "unknown flag: $1" >&2; exit 4 ;;
        *) TARGET="$1"; shift ;;
    esac
done

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# shellcheck source=lib/symbols.sh
source "${ROOT}/scripts/lib/symbols.sh"
SYMBOLS_LC=$(get_symbols deployed lower)

# Fetch latest heartbeat per engine via single ssh round-trip. Each
# heartbeat line is the latest JSON from the engine's log; the local
# side parses with jq.
set +e
HEARTBEATS=$(ssh -o BatchMode=yes -o ConnectTimeout=10 "${TARGET}" "for sym in $SYMBOLS_LC; do
    last=\$(grep '\"msg\":\"heartbeat\"' /var/log/paper-live/\${sym}.log 2>/dev/null | tail -1)
    if [[ -z \"\$last\" ]]; then
        echo \"\${sym}|NO_HEARTBEAT\"
    else
        # Format: symbol|p50_ms|p99_ms|max_ms|samples
        echo \"\${sym}|\$(echo \"\$last\" | jq -r '[.lag_p50_ms // \"n/a\", .lag_p99_ms // \"n/a\", .lag_max_ms // \"n/a\", .lag_samples // 0] | @tsv' | tr '\\t' '|')\"
    fi
done" 2>&1)
SSH_EXIT=$?
set -e

if [[ "$SSH_EXIT" -ne 0 ]]; then
    echo "ERROR: ssh to ${TARGET} failed (exit=$SSH_EXIT) — could not fetch heartbeats" >&2
    echo "${HEARTBEATS}" | head -3 >&2
    exit 3
fi

# ── Aggregate ────────────────────────────────────────────────────────
declare -i COUNT_OK=0 COUNT_TYPICAL=0 COUNT_DEGRADED=0 COUNT_HIGH=0 COUNT_NO_DATA=0
declare -a P99_VALUES=()
declare -a DEGRADED_LIST=()
declare -a HIGH_LIST=()
MAX_P99=-1
MAX_P99_SYM=""

# For median: collect numeric p99s, sort, pick middle.
# For tier counts: walk classify_lag_ms result.

# Per-engine table is built as we walk. Stash rows for ordered output.
declare -a TABLE_ROWS=()
TABLE_ROWS+=("$(printf '%-14s %-9s %-9s %-9s %-9s %s' "symbol" "p50_ms" "p99_ms" "max_ms" "samples" "tier")")
TABLE_ROWS+=("$(printf '%-14s %-9s %-9s %-9s %-9s %s' "------" "------" "------" "------" "-------" "----")")

while IFS='|' read -r sym p50 p99 maxlag samples; do
    [[ -z "$sym" ]] && continue
    if [[ "$p50" == "NO_HEARTBEAT" ]]; then
        COUNT_NO_DATA+=1
        TABLE_ROWS+=("$(printf '%-14s %-9s %-9s %-9s %-9s %s' "$sym" "—" "—" "—" "—" "NO_HEARTBEAT")")
        continue
    fi
    tier=$(classify_lag_ms "$p99")
    case "$tier" in
        OK)       COUNT_OK+=1 ;;
        TYPICAL)  COUNT_TYPICAL+=1 ;;
        DEGRADED) COUNT_DEGRADED+=1; DEGRADED_LIST+=("$sym") ;;
        HIGH)     COUNT_HIGH+=1;     HIGH_LIST+=("$sym") ;;
        NO_DATA)  COUNT_NO_DATA+=1 ;;
    esac
    # Track max-p99 across fleet (numeric only).
    if [[ "$p99" =~ ^[0-9]+$ ]]; then
        P99_VALUES+=("$p99")
        if [[ "$p99" -gt "$MAX_P99" ]]; then
            MAX_P99=$p99
            MAX_P99_SYM=$sym
        fi
    fi
    TABLE_ROWS+=("$(printf '%-14s %-9s %-9s %-9s %-9s %s' "$sym" "$p50" "$p99" "$maxlag" "$samples" "$tier")")
done <<< "$HEARTBEATS"

# Median p99 (numeric only). Sort and pick middle. Bash sort.
MEDIAN_P99="n/a"
if [[ ${#P99_VALUES[@]} -gt 0 ]]; then
    sorted=$(printf '%s\n' "${P99_VALUES[@]}" | sort -n)
    n=${#P99_VALUES[@]}
    mid=$(( n / 2 ))
    MEDIAN_P99=$(echo "$sorted" | sed -n "$((mid + 1))p")
fi

# Verdict
IFS='|' read -r VERDICT EXIT_CODE <<< "$(fleet_verdict "$COUNT_HIGH" "$COUNT_DEGRADED")"

# ── Output ───────────────────────────────────────────────────────────
if [[ "$QUIET" -eq 1 ]]; then
    echo "lag_summary: ${VERDICT} — max_p99=${MAX_P99}ms@${MAX_P99_SYM} median_p99=${MEDIAN_P99}ms ok=${COUNT_OK} typ=${COUNT_TYPICAL} deg=${COUNT_DEGRADED} hi=${COUNT_HIGH} nodata=${COUNT_NO_DATA}"
    exit "$EXIT_CODE"
fi

SEP=$(printf '%0.s═' {1..78})
echo "$SEP"
echo "  LAG SUMMARY  $(date -u '+%Y-%m-%dT%H:%M:%SZ')  →  ${TARGET}"
echo "$SEP"
echo
echo "  Per-engine source-to-receipt lag (latest heartbeat):"
echo
for row in "${TABLE_ROWS[@]}"; do
    echo "  $row"
done
echo
echo "  Fleet rollup:"
printf "    %-9s : %d engines\n" "OK"       "$COUNT_OK"
printf "    %-9s : %d engines\n" "TYPICAL"  "$COUNT_TYPICAL"
if [[ ${#DEGRADED_LIST[@]} -gt 0 ]]; then
    printf "    %-9s : %d engines (%s)\n" "DEGRADED" "$COUNT_DEGRADED" "${DEGRADED_LIST[*]}"
else
    printf "    %-9s : %d engines\n" "DEGRADED" "$COUNT_DEGRADED"
fi
if [[ ${#HIGH_LIST[@]} -gt 0 ]]; then
    printf "    %-9s : %d engines (%s)\n" "HIGH" "$COUNT_HIGH" "${HIGH_LIST[*]}"
else
    printf "    %-9s : %d engines\n" "HIGH" "$COUNT_HIGH"
fi
printf "    %-9s : %d engines\n" "NO_DATA"  "$COUNT_NO_DATA"
echo
if [[ "$MAX_P99" -ge 0 ]]; then
    echo "    Max p99 across fleet: ${MAX_P99}ms (${MAX_P99_SYM})"
    echo "    Median p99:           ${MEDIAN_P99}ms"
else
    echo "    Max p99 across fleet: n/a (no engines reported lag samples)"
fi
echo
case "$VERDICT" in
    HEALTHY)  echo "  >>> VERDICT: HEALTHY (no degraded engines)" ;;
    DEGRADED) echo "  >>> VERDICT: DEGRADED (${COUNT_DEGRADED} engine(s) lag_p99 > 15s — investigate)" ;;
    HIGH)     echo "  >>> VERDICT: HIGH (${COUNT_HIGH} engine(s) lag_p99 > 30s — severe degradation)" ;;
esac
echo "$SEP"

exit "$EXIT_CODE"
