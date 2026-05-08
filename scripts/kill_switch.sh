#!/usr/bin/env bash
# scripts/kill_switch.sh — operator wrapper for cmd/kill_switch.
#
# Sources /etc/paper-live/env (BINANCE_API_KEY/SECRET, TELEGRAM_*) and
# invokes the kill_switch binary with whatever args you pass through.
# Implements Phase 2 Path C "real-money market close" of the locked
# auto-kill execution rule
# (results/auto_kill_execution_decision_rule_2026-05-08.md), which
# references this script by name.
#
# USAGE
#   ./scripts/kill_switch.sh --reason <slug> --positions "<spec>"           # dry-run
#   ./scripts/kill_switch.sh --reason <slug> --positions "<spec>" CONFIRM   # live
#
# DRY-RUN
#   Default. Prints what would close, exits 0 without contacting Binance.
#   Run this BEFORE the live invocation to verify the position spec is
#   what you intended (typo in side, qty, or symbol = lost money under
#   CONFIRM).
#
# LIVE
#   Append CONFIRM as the FINAL positional arg. Sends real reduceOnly
#   MARKET orders. Pre-fire + post-fire CRITICAL Telegram alerts.
#
# OVERRIDES (for testnet runs / non-VPS testing)
#   PAPER_LIVE_ENV_FILE  Path to env file (default /etc/paper-live/env)
#   KILL_SWITCH_BIN      Path to kill_switch binary (default
#                        /opt/trading-engine/bin/kill_switch)
#   BINANCE_API_BASE     Override (testnet:
#                        https://testnet.binancefuture.com). Forwarded
#                        to the binary via env.
#
# OPERATOR PRECONDITION (per the locked rule's Phase 2 verification gate)
#   Run scripts/forward_paper_status.sh FIRST to enumerate currently-open
#   positions per cohort. The locked rule rejects auto-discovery — the
#   operator's explicit list IS the human-in-the-loop safety gate.

set -euo pipefail

ENV_FILE="${PAPER_LIVE_ENV_FILE:-/etc/paper-live/env}"
BIN="${KILL_SWITCH_BIN:-/opt/trading-engine/bin/kill_switch}"

if [[ ! -f "$ENV_FILE" ]]; then
    echo "ERROR: env file $ENV_FILE not found" >&2
    echo "       (set PAPER_LIVE_ENV_FILE=<path> to override)" >&2
    exit 1
fi
if [[ ! -x "$BIN" ]]; then
    echo "ERROR: kill_switch binary $BIN not executable" >&2
    echo "       (build via: go build -o $BIN ./cmd/kill_switch)" >&2
    echo "       (set KILL_SWITCH_BIN=<path> to override)" >&2
    exit 1
fi

# Source env file with auto-export so child binary inherits.
set -a
# shellcheck disable=SC1090
source "$ENV_FILE"
set +a

# Detect CONFIRM mode by checking the trailing positional arg. We don't
# parse flags here — let the binary do it — but we do validate the env
# precondition early when the operator IS firing for real, so the failure
# mode is "no order sent" rather than "alert sent then order sent without
# creds".
LAST_ARG=""
if (( $# > 0 )); then
    for arg in "$@"; do LAST_ARG="$arg"; done
fi

if [[ "$LAST_ARG" == "CONFIRM" ]]; then
    if [[ -z "${BINANCE_API_KEY:-}" || -z "${BINANCE_API_SECRET:-}" ]]; then
        echo "ERROR: CONFIRM mode requires BINANCE_API_KEY + BINANCE_API_SECRET in $ENV_FILE" >&2
        echo "       Edit $ENV_FILE and re-run." >&2
        exit 1
    fi
    echo "→ CONFIRM mode: about to fire kill_switch — pre-fire alert imminent." >&2
fi

# exec replaces the shell with the binary so Ctrl-C + signals propagate
# directly, and the exit status is the binary's exit status (used by
# callers who chain on success/failure).
exec "$BIN" "$@"
