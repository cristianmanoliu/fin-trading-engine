#!/usr/bin/env bash
# daily_status.sh — single paste-ready operator snapshot.
#
# Composes 4 existing tools into one consolidated view that's natural to
# paste into operator notes / Slack / email. Each section runs
# independently — a failure in one doesn't suppress the others — and
# the script's overall exit code reflects the LIMBO resolution + the
# lag tier so a cron wrapper can route by tier without re-parsing.
#
# Sections:
#   1. forward_paper_status     — per-cohort gates + LIMBO verdict + drift heartbeat
#   2. lag_summary --quiet      — fleet-wide source-to-receipt p99 one-liner
#   3. realized_cost_trajectory — per-trade fee/slip trend
#   4. forward_paper_trajectory — multi-snapshot trend (if ≥2 snapshots)
#   5. forward_paper_timeline   — STAGE_1 dual-projection (observed + baseline)
#   6. LIMBO resolution         — final go/no-go verdict
#   ── aggregate footer         — single-line verdict mirroring the LIMBO
#                                  rule + the lag tier + STAGE_1=Nd compact
#
# Lens-as-design-tool: every section invocation has explicit failure
# semantics. A section that fails to invoke (binary missing / ssh down)
# is rendered distinctly from a section that ran and returned an
# expected non-zero (drift fire, lag HIGH). Both routes the aggregate
# exit code; neither silently collapses to "OK".
#
# Exit codes:
#   0 OK         — LIMBO=CONTINUE/PROMOTE/WATCH AND lag ≤ TYPICAL AND
#                  every section invoked cleanly
#   1 WARN       — LIMBO=OPERATOR_REVIEW OR lag=DEGRADED OR ≥1 section
#                  failed to invoke (binary missing / ssh failure)
#   2 KILL       — LIMBO=KILL OR lag=HIGH OR LIMBO=INPUT_ERROR
#   3 INPUT_ERR  — bad flags / required helper missing / unknown invocation
#
# Telegram tier mapping (cron use, via scripts/lib/notify.sh):
#   0 → no alert
#   1 → WARN
#   2 → CRITICAL
#   3 → CRITICAL (operator misconfiguration is still actionable)
#
# Usage:
#   ./scripts/daily_status.sh                    # default VPS
#   ./scripts/daily_status.sh root@host          # alt VPS
#   ./scripts/daily_status.sh --local            # local journals
#   ./scripts/daily_status.sh --quiet            # one-line aggregate only
set -euo pipefail

# ─── Resolve paths ──────────────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# ─── Exit-code constants ────────────────────────────────────────────────────
readonly EXIT_OK=0
readonly EXIT_WARN=1
readonly EXIT_KILL=2
readonly EXIT_INPUT_ERR=3

# ─── Helpers (sourceable for tests) ─────────────────────────────────────────

# Translate a LIMBO resolution exit code → human label.
limbo_label() {
    case "$1" in
        0) echo "CONTINUE" ;;
        1) echo "PROMOTE" ;;
        2) echo "WATCH" ;;
        3) echo "OPERATOR_REVIEW" ;;
        4) echo "KILL" ;;
        5) echo "INPUT_ERROR" ;;
        *) echo "UNKNOWN(${1})" ;;
    esac
}

# Translate a lag_summary exit code → human label. The sentinel "-"
# means we deliberately skipped invocation (e.g., local mode where the
# fleet lag check is VPS-only) — distinct from a genuine UNKNOWN tier
# so the operator can tell at a glance the absence is by design.
lag_label() {
    case "$1" in
        0) echo "HEALTHY" ;;
        1) echo "DEGRADED" ;;
        2) echo "HIGH" ;;
        3) echo "SSH_FAILURE" ;;
        4) echo "INPUT_ERROR" ;;
        -) echo "SKIPPED" ;;
        *) echo "UNKNOWN(${1})" ;;
    esac
}

# Aggregate the section verdicts into a single overall exit code.
#
# Inputs (positional):
#   $1 limbo_exit       — exit code from forward_paper_resolution.py
#                         (or sentinel "-" if not invoked)
#   $2 lag_exit         — exit code from lag_summary.sh (or "-")
#   $3 cost_section_ok  — 1 if cost trajectory section invoked cleanly,
#                         0 if it failed to invoke (helper missing / ssh)
#   $4 traj_section_ok  — same for snapshot trajectory section
#
# Precedence: KILL (2) > WARN (1) > OK (0). A section that failed to
# invoke (cost_section_ok=0 or traj_section_ok=0) is treated as WARN
# — explicit-not-silent. Same lens shape as section-invocation gates
# in post_deploy_check + weekly_audit.
aggregate_exit() {
    local limbo="$1" lag="$2" cost_ok="$3" traj_ok="$4"
    local worst=0
    # KILL signals.
    case "$limbo" in
        4|5) worst=2 ;;
    esac
    if [[ "$lag" == "2" ]]; then worst=2; fi
    # WARN signals (only escalate if not already KILL).
    if [[ "$worst" -lt 2 ]]; then
        case "$limbo" in
            3) worst=1 ;;
        esac
        case "$lag" in
            1|3) [[ "$worst" -lt 1 ]] && worst=1 ;;
        esac
        if [[ "$cost_ok" != "1" ]]; then [[ "$worst" -lt 1 ]] && worst=1; fi
        if [[ "$traj_ok" != "1" ]]; then [[ "$worst" -lt 1 ]] && worst=1; fi
    fi
    echo "$worst"
}

# Extract the STAGE_1 days-remaining substring from a timeline --quiet
# line for the compact aggregate footer annotation. The quiet format is:
#   forward-paper: day 7 / 0 trades / rate=1.18/d (historical_fleet) / \
#     binding=trade-count / STAGE_1 earliest 2026-09-09 (120d) [/ baseline ...]
# We want just "STAGE_1=120d" — strip the date + match the (Nd) parens.
# Returns empty string if the line doesn't match (unexpected format).
#
# Pure string-helper — no I/O, no env state. Lives above the BASH_SOURCE
# guard so test_daily_status.sh can exercise it directly.
extract_stage_1_compact() {
    local quiet="$1"
    [[ -z "$quiet" ]] && return
    # Cut at "STAGE_1 earliest " then take the (Nd) parens that follow.
    # `${var#*pattern}` works on both BSD bash 3.2 (macOS) and GNU bash 4+
    # (Linux CI) so no GNU-only constructs needed. If "STAGE_1 earliest "
    # isn't present, the # operator leaves the string unchanged — which
    # means the regex sanity-check at the end catches the bad form.
    if [[ "$quiet" != *"STAGE_1 earliest "* ]]; then
        return
    fi
    local tail
    tail="${quiet#*STAGE_1 earliest }"
    # Now tail starts with "YYYY-MM-DD (Nd) ..." — extract the (Nd).
    # Drop everything before "(", drop trailing ")" + anything after.
    local in_parens="${tail#*\(}"
    local days="${in_parens%%\)*}"
    # Sanity-check: should be "Nd" form. Reject if it has whitespace
    # or doesn't end in 'd'.
    if [[ "$days" =~ ^[0-9]+d$ ]]; then
        echo "$days"
    fi
}

# Test entry-point guard: when sourced (BASH_SOURCE != $0), stop here so
# tests can call helpers directly without invoking the main flow.
if [[ "${BASH_SOURCE[0]}" != "${0}" ]]; then
    return 0
fi

# ─── Args ───────────────────────────────────────────────────────────────────
VPS_TARGET="root@178.105.24.230"
LOCAL_MODE=0
QUIET=0
while [[ $# -gt 0 ]]; do
    case "$1" in
        --local) LOCAL_MODE=1; shift ;;
        --quiet) QUIET=1; shift ;;
        --help|-h) sed -n '2,38p' "$0" | sed 's/^# \{0,1\}//'; exit "$EXIT_OK" ;;
        --*) echo "unknown flag: $1" >&2; exit "$EXIT_INPUT_ERR" ;;
        *) VPS_TARGET="$1"; shift ;;
    esac
done

# Refuse to interpret --local + a positional VPS as anything sensible —
# the operator likely typo'd one or the other. Fail-loud per the
# missing-input → silent-success audit pattern.
if [[ "$LOCAL_MODE" == "1" ]] && [[ "$VPS_TARGET" != "root@178.105.24.230" ]]; then
    echo "ERROR: --local and an explicit VPS target are mutually exclusive." >&2
    echo "  Pick one: '--local' (uses ./logs/journal) OR a hostname (uses ssh)." >&2
    exit "$EXIT_INPUT_ERR"
fi

# ─── Tool presence checks ───────────────────────────────────────────────────
FORWARD_PAPER_STATUS="${SCRIPT_DIR}/forward_paper_status.sh"
LAG_SUMMARY="${SCRIPT_DIR}/lag_summary.sh"
RESOLUTION_PY="${SCRIPT_DIR}/forward_paper_resolution.py"
COST_TRAJECTORY_PY="${SCRIPT_DIR}/realized_cost_trajectory.py"
SNAPSHOT_TRAJECTORY_PY="${SCRIPT_DIR}/forward_paper_trajectory.py"
TIMELINE_PY="${SCRIPT_DIR}/forward_paper_timeline.py"

# Required helpers — refuse to start if missing. These are part of the
# script's source-of-truth contract; their absence is a packaging error
# (CI ran on a stripped checkout, operator nuked scripts/, etc.) that
# must not silently produce an empty-but-zero result.
for helper in "$FORWARD_PAPER_STATUS" "$RESOLUTION_PY"; do
    if [[ ! -f "$helper" ]]; then
        echo "ERROR: required helper not found: $helper" >&2
        exit "$EXIT_INPUT_ERR"
    fi
done

# ─── Section runners ────────────────────────────────────────────────────────

# Run forward_paper_status.sh and stream output. Captures exit code.
run_forward_paper_status() {
    if [[ "$LOCAL_MODE" == "1" ]]; then
        JOURNAL_DIR="./logs/journal" bash "$FORWARD_PAPER_STATUS" local || true
    else
        bash "$FORWARD_PAPER_STATUS" "$VPS_TARGET" || true
    fi
}

# Run lag_summary.sh --quiet. Captures BOTH output AND exit code so the
# aggregate can route by tier. Local mode skips this (lag is a fleet-
# scoped check that needs ssh into the VPS).
LAG_EXIT="-"
LAG_LINE=""
run_lag_summary() {
    if [[ "$LOCAL_MODE" == "1" ]]; then
        LAG_LINE="lag_summary: SKIPPED (local mode — fleet lag is VPS-only)"
        LAG_EXIT="-"
        return
    fi
    if [[ ! -f "$LAG_SUMMARY" ]]; then
        LAG_LINE="lag_summary: SKIPPED (helper missing)"
        LAG_EXIT="-"
        return
    fi
    set +e
    LAG_LINE=$(bash "$LAG_SUMMARY" --quiet "$VPS_TARGET" 2>&1)
    LAG_EXIT=$?
    set -e
}

# Run realized_cost_trajectory.py.
COST_SECTION_OK=1
run_cost_trajectory() {
    if [[ ! -f "$COST_TRAJECTORY_PY" ]]; then
        echo "(cost trajectory helper missing — skipping)"
        COST_SECTION_OK=0
        return
    fi
    local rc=0
    set +e
    if [[ "$LOCAL_MODE" == "1" ]]; then
        python3 "$COST_TRAJECTORY_PY" --live-source local --live-dir ./logs/journal
        rc=$?
    else
        python3 "$COST_TRAJECTORY_PY" --vps "$VPS_TARGET"
        rc=$?
    fi
    set -e
    # Exit 0 = rendered ≥1 row (good); exit 2 = no qualifying trades yet
    # (legitimate fresh-deploy state — surface but don't count as failure);
    # exit 3 = input error (operator path typo / ssh down — count as failure).
    case "$rc" in
        0|2) COST_SECTION_OK=1 ;;
        *)   COST_SECTION_OK=0 ;;
    esac
}

# Run forward_paper_trajectory.py only if ≥2 snapshots exist.
TRAJ_SECTION_OK=1
run_snapshot_trajectory() {
    local snap_dir="${ROOT}/results/forward_paper_snapshots"
    if [[ ! -f "$SNAPSHOT_TRAJECTORY_PY" ]]; then
        echo "(snapshot trajectory helper missing — skipping)"
        TRAJ_SECTION_OK=0
        return
    fi
    if [[ ! -d "$snap_dir" ]]; then
        echo "(no snapshot directory yet — skipping)"
        return
    fi
    local n_snaps
    # Word-counting glob requires the fail-soft pattern. nullglob would
    # be nicer but we keep set -u compat with bash 3.2 macOS.
    n_snaps=$(find "$snap_dir" -maxdepth 1 -name '*.txt' -type f 2>/dev/null | wc -l | tr -d ' ')
    if [[ "$n_snaps" -lt 2 ]]; then
        echo "(${n_snaps} snapshot(s) — need ≥2 to render trajectory; skipping)"
        return
    fi
    local rc=0
    set +e
    python3 "$SNAPSHOT_TRAJECTORY_PY" --dir "$snap_dir"
    rc=$?
    set -e
    # Exit 0 = rendered (good); 2 = no snapshots (already filtered);
    # 3 = parse error on ≥1 file (surface but don't count as failure
    # since the script still renders what it can).
    case "$rc" in
        0|3) TRAJ_SECTION_OK=1 ;;
        *)   TRAJ_SECTION_OK=0 ;;
    esac
}

# Run forward_paper_timeline.py and capture both verbose output (for the
# section render) and the quiet one-liner (for the QUIET aggregate footer's
# STAGE_1=Nd annotation). Local mode passes --journal-dir; VPS mode passes
# --vps (the script defaults --journal-dir to /var/log/paper-live/journal,
# the canonical VPS path).
#
# Soft-fail: timeline is informational not a gate. If the helper is missing
# or the journal-dir is unreadable, the section renders a diagnostic but
# does NOT escalate aggregate_exit — distinct from the cost/traj sections
# which DO surface to aggregate_exit because their failure shapes are
# "helper missing" (packaging error) and "ssh down" (operator-fixable
# infra). Timeline failure on a fresh-deploy ("no journal yet") is a
# routine state, not an alarm.
TIMELINE_VERBOSE=""
TIMELINE_QUIET=""
run_timeline() {
    if [[ ! -f "$TIMELINE_PY" ]]; then
        TIMELINE_VERBOSE="(timeline helper missing — skipping projection)"
        TIMELINE_QUIET=""
        return
    fi
    local args=()
    if [[ "$LOCAL_MODE" == "1" ]]; then
        args=(--journal-dir ./logs/journal)
    else
        args=(--vps "$VPS_TARGET")
    fi
    local rc=0
    set +e
    TIMELINE_VERBOSE=$(python3 "$TIMELINE_PY" "${args[@]}" 2>&1)
    rc=$?
    set -e
    if [[ "$rc" != "0" ]]; then
        # Render exists but the projection couldn't be computed (input
        # error, no journals, etc.). Keep verbose for the section + clear
        # quiet so the aggregate footer skips the STAGE_1=Nd annotation
        # instead of appending a garbled value.
        TIMELINE_QUIET=""
        return
    fi
    # Quiet pass: same args plus --quiet for the one-liner.
    set +e
    TIMELINE_QUIET=$(python3 "$TIMELINE_PY" "${args[@]}" --quiet 2>/dev/null)
    rc=$?
    set -e
    if [[ "$rc" != "0" ]]; then
        TIMELINE_QUIET=""
    fi
}

# Re-run forward_paper_resolution to get the LIMBO exit code directly
# (forward_paper_status invokes it internally but doesn't propagate the
# exit code; we need it for the aggregate footer + script exit code).
# Cached so we don't pay the ssh + sibling-script roundtrip twice.
LIMBO_EXIT=0
LIMBO_REASONS=""
run_resolution() {
    local rc=0
    local out=""
    set +e
    out=$(python3 "$RESOLUTION_PY" 2>&1)
    rc=$?
    set -e
    LIMBO_EXIT="$rc"
    # Surface the bullet-line reasons in the footer (resolution.py emits
    # them prefixed by `      •`).
    LIMBO_REASONS=$(echo "$out" | grep -E "^[[:space:]]+•" | head -3 || true)
}

# ─── Main flow ──────────────────────────────────────────────────────────────

# Quiet mode = aggregate footer only (cron-friendly).
if [[ "$QUIET" == "1" ]]; then
    run_lag_summary
    run_resolution
    run_timeline
    AGG=$(aggregate_exit "$LIMBO_EXIT" "$LAG_EXIT" 1 1)
    case "$AGG" in
        0) verdict="OK" ;;
        1) verdict="WARN" ;;
        2) verdict="KILL" ;;
        3) verdict="INPUT_ERR" ;;
    esac
    # Append STAGE_1=Nd when timeline produced a valid quiet line.
    stage_1_compact=$(extract_stage_1_compact "$TIMELINE_QUIET")
    stage_1_suffix=""
    [[ -n "$stage_1_compact" ]] && stage_1_suffix=" STAGE_1=${stage_1_compact}"
    echo "daily_status: ${verdict} — LIMBO=$(limbo_label "$LIMBO_EXIT") lag=$(lag_label "$LAG_EXIT")${stage_1_suffix}"
    exit "$AGG"
fi

# Verbose mode: full sectioned output.
SEP=$(printf '%0.s═' {1..78})
echo "$SEP"
echo "  DAILY STATUS  $(date -u '+%Y-%m-%dT%H:%M:%SZ')  →  ${VPS_TARGET}${LOCAL_MODE:+ (local)}"
echo "$SEP"

echo
echo "── 1. Forward-paper status ──"
run_forward_paper_status

echo
echo "── 2. Fleet lag rollup ──"
run_lag_summary
echo "  $LAG_LINE"
if [[ "$LAG_EXIT" != "-" ]]; then
    echo "  (tier: $(lag_label "$LAG_EXIT"))"
fi

echo
echo "── 3. Realized cost trajectory ──"
run_cost_trajectory

echo
echo "── 4. Multi-snapshot trajectory ──"
run_snapshot_trajectory

echo
echo "── 5. STAGE_1 timeline projection ──"
run_timeline
if [[ -n "$TIMELINE_VERBOSE" ]]; then
    echo "$TIMELINE_VERBOSE"
fi

echo
echo "── 6. LIMBO resolution ──"
run_resolution
echo "  Verdict: $(limbo_label "$LIMBO_EXIT")"
if [[ -n "$LIMBO_REASONS" ]]; then
    echo "$LIMBO_REASONS"
fi

# ─── Aggregate footer ───────────────────────────────────────────────────────
AGG=$(aggregate_exit "$LIMBO_EXIT" "$LAG_EXIT" "$COST_SECTION_OK" "$TRAJ_SECTION_OK")
case "$AGG" in
    0) overall="OK     — daily snapshot clean across all sections" ;;
    1) overall="WARN   — review surfaced concerns (one or more sections WARN-tier)" ;;
    2) overall="KILL   — locked criterion fired (LIMBO=KILL or lag=HIGH); investigate now" ;;
    3) overall="INPUT  — operator misconfiguration / missing helper" ;;
esac
echo
echo "$SEP"
echo "  >>> AGGREGATE: ${overall}"
echo "      LIMBO=$(limbo_label "$LIMBO_EXIT")   lag=$(lag_label "$LAG_EXIT")   "\
"cost_section_ok=${COST_SECTION_OK}   traj_section_ok=${TRAJ_SECTION_OK}"
echo "$SEP"

exit "$AGG"
