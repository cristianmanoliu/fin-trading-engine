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

    # Step 1: derive symbol set from VPS layer3/ listing.
    # Filename schema: <SYMBOL>-YYYY-MM.jsonl — strip the date suffix, dedupe.
    symbols=$(ssh -o BatchMode=yes -o ConnectTimeout=10 "$HOST" \
        "ls $VPS_BASE/layer3/*.jsonl 2>/dev/null" \
        | xargs -n1 basename 2>/dev/null \
        | sed 's/-[0-9]\{4\}-[0-9]\{2\}\.jsonl$//' \
        | sort -u)
    ssh_rc=${PIPESTATUS[0]}
    if [[ $ssh_rc -eq 255 ]]; then
        echo "ERR: ssh to $HOST failed (exit 255 — host unreachable / auth failed)" >&2
        exit 3
    fi
    if [[ -z "$symbols" ]]; then
        echo "ERR: no Layer 3 journals on VPS at $VPS_BASE/layer3/" >&2
        exit 3
    fi

    # TODO Task 5: mid-write guard
    # TODO Task 6: wipe + rsync
    # TODO Task 7: invoke verdict
    echo "ERR: --pull pipeline not yet complete (symbols: $symbols)" >&2
    exit 99
fi
