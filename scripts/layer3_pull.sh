#!/usr/bin/env bash
# layer3_pull.sh — ad-hoc Layer 3 shadow-parity verdict runner.
#
# Two modes:
#
#   1. Default (SSH-and-run). Forwards all args to scripts/layer3_verdict.sh
#      on the VPS. No local cache. Fast and minimal — answers "what's the
#      Layer 3 state right now?" without disturbing local state.
#
#   2. --pull (local cache). Rsyncs Layer 3 testnet journals + matching
#      live-side journals to tmp/layer3_pull/{stub,testnet}/, then runs
#      the verdict locally against the cache. Useful for ad-hoc grep,
#      offline forensics, or pre-promotion local rehearsals.
#
# Exit codes propagate verbatim from scripts/layer3_verdict.sh (0/1/2/3/4).
# Operator/infra failures map to exit 3 (INPUT_ERROR), matching the
# verdict's dual-sense Telegram routing.
#
# Filename schema (single source of truth — fixtures must match):
#   <SYMBOL>-YYYY-MM.jsonl    e.g. KAVAUSDT-2026-05.jsonl
#
# Usage:
#   scripts/layer3_pull.sh                      # default SSH-and-run
#   scripts/layer3_pull.sh --verbose            # forwards --verbose to verdict
#   scripts/layer3_pull.sh --skip-min-days      # forwards --skip-min-days
#   scripts/layer3_pull.sh --pull               # local-cache mode
#   scripts/layer3_pull.sh --pull --verbose     # local-cache + forwarded flags
#
# Env overrides (for testing — production uses defaults):
#   LAYER3_PULL_HOST           SSH target (default: root@178.105.24.230)
#   LAYER3_PULL_VPS_BASE       Remote journal root (default: /var/log/paper-live/journal)
#   LAYER3_PULL_CACHE_DIR      Local cache (default: tmp/layer3_pull)
#   LAYER3_PULL_VERDICT_BIN    Verdict script path (default: scripts/layer3_verdict.sh)
set -uo pipefail

HOST="${LAYER3_PULL_HOST:-root@178.105.24.230}"
VPS_BASE="${LAYER3_PULL_VPS_BASE:-/var/log/paper-live/journal}"
CACHE_DIR="${LAYER3_PULL_CACHE_DIR:-tmp/layer3_pull}"
VERDICT_BIN="${LAYER3_PULL_VERDICT_BIN:-scripts/layer3_verdict.sh}"

PULL_MODE=0
FORWARD_ARGS=()
for arg in "$@"; do
    case "$arg" in
        --pull) PULL_MODE=1 ;;
        *)      FORWARD_ARGS+=("$arg") ;;
    esac
done

if [[ $PULL_MODE -eq 0 ]]; then
    # Default: SSH-and-run the verdict on the VPS.
    remote_cmd="cd /opt/trading-engine && bash scripts/layer3_verdict.sh"
    remote_cmd+=" --stub-dir $VPS_BASE"
    remote_cmd+=" --testnet-dir $VPS_BASE/layer3"
    if [[ ${#FORWARD_ARGS[@]} -gt 0 ]]; then
        for a in "${FORWARD_ARGS[@]}"; do
            # shell-quote each forwarded arg so spaces, quotes, and shell
            # metachars survive the remote bash re-parse
            remote_cmd+=" $(printf '%q' "$a")"
        done
    fi
    ssh -o BatchMode=yes -o ConnectTimeout=10 "$HOST" "$remote_cmd"
    rc=$?
    if [[ $rc -eq 255 ]]; then
        echo "ERR: ssh to $HOST failed (exit 255 — host unreachable / auth failed)" >&2
        exit 3
    fi
    exit $rc
else
    # --pull mode: full local-cache pipeline.

    # Concurrency guard — only one --pull in flight per cache dir.
    mkdir -p "$CACHE_DIR" 2>/dev/null || {
        echo "ERR: cannot create cache dir $CACHE_DIR" >&2
        exit 3
    }
    exec 9>"$CACHE_DIR/.lock"
    if ! flock -n 9; then
        echo "ERR: another layer3_pull --pull in progress (lock held)" >&2
        exit 3
    fi

    # Step 1: derive symbol set from VPS layer3/ listing.
    # Filename schema: <SYMBOL>-YYYY-MM.jsonl — strip the date suffix, dedupe.
    symbols=$(ssh -o BatchMode=yes -o ConnectTimeout=10 "$HOST" \
        "ls $VPS_BASE/layer3/*.jsonl 2>/dev/null" \
        | xargs -n1 basename 2>/dev/null \
        | sed 's/-[0-9]\{4\}-[0-9]\{2\}\.jsonl$//' \
        | sort -u)
    # PIPESTATUS[0] here reflects ssh's exit because set -o pipefail propagates
    # the rightmost nonzero through the command substitution, and downstream
    # stages (xargs/basename/sed/sort) cannot realistically fail on valid input.
    ssh_rc=${PIPESTATUS[0]}
    if [[ $ssh_rc -eq 255 ]]; then
        echo "ERR: ssh to $HOST failed (exit 255 — host unreachable / auth failed)" >&2
        exit 3
    fi
    if [[ -z "$symbols" ]]; then
        echo "ERR: no Layer 3 journals on VPS at $VPS_BASE/layer3/" >&2
        exit 3
    fi

    # Step 2: mid-write guard. Bash command substitution strips trailing
    # newlines, so $(tail -c 1 file) of a newline-terminated file returns
    # empty — that's our OK signal. A mid-write file returns the last
    # raw byte. We tag the SSH call so the test mock can distinguish it
    # from the ls call above.
    retry_sleep="${LAYER3_PULL_RETRY_SLEEP:-2}"
    # Build the remote one-liner. Test mocks recognize the "mid-write-check"
    # tag. The remote `for f in <files>; do ...; done` checks each file.
    check_files=""
    for s in $symbols; do
        check_files+=" $VPS_BASE/layer3/${s}-*.jsonl"
        check_files+=" $VPS_BASE/${s}-*.jsonl"
    done
    check_cmd="# mid-write-check
for f in${check_files}; do
    [[ -s \"\$f\" ]] || continue
    last=\$(tail -c 1 \"\$f\")
    if [[ -n \"\$last\" ]]; then
        echo \"MID:\$f\"
        exit 1
    fi
done
exit 0"

    # Run the ssh call outside `if !` so we can capture the real exit code.
    # `if ! ssh ...; then $?` would resolve to the negated test's status (0),
    # not ssh's, losing the ability to distinguish ssh-255 from mid-write.
    ssh -o BatchMode=yes -o ConnectTimeout=10 "$HOST" "$check_cmd" > /dev/null 2>/dev/null
    mid_rc=$?
    if [[ $mid_rc -ne 0 ]]; then
        sleep "$retry_sleep"
        ssh -o BatchMode=yes -o ConnectTimeout=10 "$HOST" "$check_cmd" > /dev/null 2>/dev/null
        mid_rc=$?
        if [[ $mid_rc -ne 0 ]]; then
            if [[ $mid_rc -eq 255 ]]; then
                echo "ERR: ssh to $HOST failed during mid-write check (exit 255 — network blip or auth)" >&2
            else
                echo "ERR: VPS journal mid-write detected after 2 attempts; operator should retry shortly" >&2
            fi
            exit 3
        fi
    fi

    # Step 3: wipe + recreate cache subdirs (no merge with prior runs).
    rm -rf "$CACHE_DIR/stub" "$CACHE_DIR/testnet"
    mkdir -p "$CACHE_DIR/stub" "$CACHE_DIR/testnet"

    # Step 4: parallel rsync. Both must succeed.
    pull_pids=()
    rsync_status=0
    {
        rsync -a "${HOST}:${VPS_BASE}/layer3/*.jsonl" "$CACHE_DIR/testnet/" 2>/dev/null
    } &
    pull_pids+=($!)
    for s in $symbols; do
        {
            rsync -a "${HOST}:${VPS_BASE}/${s}-"*.jsonl "$CACHE_DIR/stub/" 2>/dev/null
        } &
        pull_pids+=($!)
    done
    for pid in "${pull_pids[@]}"; do
        if ! wait "$pid"; then
            rsync_status=1
        fi
    done
    if [[ $rsync_status -ne 0 ]]; then
        echo "ERR: rsync failed during Layer 3 journal pull" >&2
        exit 3
    fi

    # Step 5: invoke verdict against the local cache.
    # exec replaces this process — verdict's exit code becomes the helper's.
    if [[ ${#FORWARD_ARGS[@]} -gt 0 ]]; then
        exec bash "$VERDICT_BIN" \
            --stub-dir "$CACHE_DIR/stub" \
            --testnet-dir "$CACHE_DIR/testnet" \
            "${FORWARD_ARGS[@]}"
    else
        exec bash "$VERDICT_BIN" \
            --stub-dir "$CACHE_DIR/stub" \
            --testnet-dir "$CACHE_DIR/testnet"
    fi
fi
