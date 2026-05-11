#!/usr/bin/env bash
# promotion_rehearsal.sh — single end-to-end "am I ready for STAGE_1?" dry-run.
#
# Composes the audited decision-grade tools into one pass across the FIVE
# phases of the STAGE_0 → STAGE_1 real-money promotion path, emitting a
# per-phase status table + an aggregate verdict the operator can act on
# without remembering 5 separate invocations. The natural extension of
# today's audit work — each underlying tool was made self-testing; this
# composes them into a single operator-facing check.
#
# Phases (mirror the locked promotion path in CLAUDE.md +
# real_money_protocol_decision_rule_2026-05-08.md):
#
#   1. Forward-paper resolution (LIMBO verdict)
#      → forward_paper_resolution.py
#   2. STAGE_0→STAGE_1 locked gates (9 criteria)
#      → stage_promotion_check.py --from-stage STAGE_0
#   3. Kill criteria absence (6 criteria)
#      → kill_protocol_check.py --stage STAGE_0
#   4. Layer 2 testnet readiness (smoke PASS within last 7d)
#      → inspect logs/layer2_smoke/ for recent PASS markers
#   5. Layer 3 shadow parity (7d dual-runner data + verdict PASS)
#      → inspect testnet journal dir + invoke layer3_verdict.sh if data present
#
# READ-ONLY across all underlying tools. The decision-grade evaluators
# (Phases 1-3) read journal state to produce verdicts but never write.
# Phases 4-5 inspect on-disk state markers without invoking the actual
# pre-flight (layer2_smoke takes 5 minutes + needs testnet creds;
# layer3_verdict builds journal_diff). The operator runs those separately
# when their phase moves from WAITING to actionable.
#
# Lens-as-design-tool:
#   - Each phase invocation has distinct failure semantics (helper-missing
#     vs returned-non-zero vs unexpected-exit vs no-state-yet)
#   - Aggregate verdict has explicit precedence: BLOCKED > INPUT_ERR > WAITING > READY
#   - N/A phases (e.g., Layer 3 with no testnet dir yet) do NOT influence
#     the aggregate — they're "not your turn yet," not blockers
#   - Every external state read pairs with a "we couldn't read this" path
#
# Exit codes:
#   0 READY      — all 5 phases READY (or N/A); operator may execute the
#                  STAGE_1 promotion via stage_promotion.sh
#   1 BLOCKED    — ≥1 phase BLOCKED (real problem requiring action)
#   2 WAITING    — phases incomplete due to data accumulation; no action
#                  required, just wait for the cron to fill in more data
#   3 INPUT_ERR  — bad flags / missing helper / configuration error
#
# Telegram tier mapping (when wrapped by weekly_audit or similar):
#   0 → INFO     (promotion-ready — operator confirmation gate)
#   1 → CRITICAL (real blocker; investigate before next attempt)
#   2 → silent   (still accumulating — routine)
#   3 → WARN     (config error)
#
# Usage:
#   ./scripts/promotion_rehearsal.sh                     # default VPS
#   ./scripts/promotion_rehearsal.sh root@host           # alt VPS
#   ./scripts/promotion_rehearsal.sh --local             # off-VPS check
#   ./scripts/promotion_rehearsal.sh --quiet             # 1-line aggregate
#   ./scripts/promotion_rehearsal.sh --phase 4           # single phase
#   ./scripts/promotion_rehearsal.sh --layer2-smoke-window-days 7
set -euo pipefail

# ─── Resolve paths ──────────────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# ─── Exit-code constants ────────────────────────────────────────────────────
readonly EXIT_READY=0
readonly EXIT_BLOCKED=1
readonly EXIT_WAITING=2
readonly EXIT_INPUT_ERR=3

# ─── Per-phase status sentinels ─────────────────────────────────────────────
# String labels emitted by each phase function. Aggregate consumes them
# precedence-ordered. Adding a new tier requires updating aggregate_tiers()
# AND the precedence chart.
readonly TIER_READY="READY"
readonly TIER_BLOCKED="BLOCKED"
readonly TIER_WAITING="WAITING"
readonly TIER_INPUT_ERR="INPUT_ERR"
readonly TIER_NA="N/A"

# ─── Helpers (sourceable for tests) ─────────────────────────────────────────

# Translate forward_paper_resolution.py exit code → rehearsal tier.
# LIMBO contract (locked):
#   0 CONTINUE → WAITING (still accumulating, not yet PROMOTE)
#   1 PROMOTE  → READY
#   2 WATCH    → BLOCKED (soft signals fire; investigate)
#   3 OPERATOR_REVIEW → BLOCKED (manual cross-check needed)
#   4 KILL     → BLOCKED (locked kill criterion fired; STOP promotion)
#   5 INPUT_ERROR → INPUT_ERR (snapshot missing/stale)
resolution_tier() {
    case "$1" in
        0) echo "$TIER_WAITING" ;;
        1) echo "$TIER_READY" ;;
        2|3|4) echo "$TIER_BLOCKED" ;;
        5) echo "$TIER_INPUT_ERR" ;;
        *) echo "$TIER_INPUT_ERR" ;;
    esac
}

# Translate stage_promotion_check.py exit code → rehearsal tier.
#   0 PROMOTE → READY
#   1 BLOCKED → BLOCKED
#   2 WAITING → WAITING
#   3 ERROR   → INPUT_ERR
#   4 PROMOTE-CANDIDATE → BLOCKED (deferred verification gate)
promote_check_tier() {
    case "$1" in
        0) echo "$TIER_READY" ;;
        1|4) echo "$TIER_BLOCKED" ;;
        2) echo "$TIER_WAITING" ;;
        3) echo "$TIER_INPUT_ERR" ;;
        *) echo "$TIER_INPUT_ERR" ;;
    esac
}

# Translate kill_protocol_check.py exit code → rehearsal tier.
#   0 CONTINUE → READY
#   1 KILL     → BLOCKED
#   2 WAITING  → WAITING
#   3 ERROR    → INPUT_ERR
#   4 OPERATOR-VERIFY → BLOCKED (operator must check deferred)
kill_check_tier() {
    case "$1" in
        0) echo "$TIER_READY" ;;
        1|4) echo "$TIER_BLOCKED" ;;
        2) echo "$TIER_WAITING" ;;
        3) echo "$TIER_INPUT_ERR" ;;
        *) echo "$TIER_INPUT_ERR" ;;
    esac
}

# Aggregate per-phase tiers → overall rehearsal exit code.
# Precedence: BLOCKED > INPUT_ERR > WAITING > READY. N/A is non-influential
# (a phase that's not yet applicable shouldn't block promotion-readiness
# — it's "your turn hasn't come yet," not "you can't proceed").
#
# Args: variadic — one tier label per phase.
aggregate_tiers() {
    local has_blocked=0 has_input_err=0 has_waiting=0
    for tier in "$@"; do
        case "$tier" in
            "$TIER_BLOCKED")   has_blocked=1 ;;
            "$TIER_INPUT_ERR") has_input_err=1 ;;
            "$TIER_WAITING")   has_waiting=1 ;;
            "$TIER_READY"|"$TIER_NA") : ;;
            *) has_input_err=1 ;;  # unknown tier = config error
        esac
    done
    if [[ "$has_blocked" -eq 1 ]]; then echo "$EXIT_BLOCKED"
    elif [[ "$has_input_err" -eq 1 ]]; then echo "$EXIT_INPUT_ERR"
    elif [[ "$has_waiting" -eq 1 ]]; then echo "$EXIT_WAITING"
    else echo "$EXIT_READY"; fi
}

# Translate aggregate exit code → human verdict label.
aggregate_label() {
    case "$1" in
        0) echo "READY" ;;
        1) echo "BLOCKED" ;;
        2) echo "WAITING" ;;
        3) echo "INPUT_ERR" ;;
        *) echo "UNKNOWN($1)" ;;
    esac
}

# Locate most-recent layer2_smoke log + check whether it represents a PASS.
# `layer2_smoke.sh` writes one log per invocation to logs/layer2_smoke/
# named <SYMBOL>_<DATE>.log. A PASS run logs a `heartbeats=N` line at the
# end via stdout (NOT in the log file — the log captures engine output).
# What the LOG contains on success is the engine's stdout: backfill +
# heartbeats + executor-active line. So we can't reliably tell PASS vs
# FAIL by inspecting the log alone.
#
# Better signal: the wrapper exits 0 on PASS. Operators can either keep
# a state file or we just check the recency of any log + assume "smoke
# was run within last N days = readiness candidate." For production
# readiness this is weak — operator must verify smoke PASS separately.
#
# So the rehearsal reports Layer 2 as READY only if BOTH:
#   - logs/layer2_smoke/ contains a log file modified within last N days
#   - AND that log file's modified time is recent
#
# OR an explicit operator marker exists at
#   logs/layer2_smoke/.last_pass — touched by the operator after a real
# PASS to attest "I verified Layer 2 PASS at this timestamp."
#
# Defaults: window = 7 days.
layer2_tier() {
    local smoke_dir="$1"
    local window_days="${2:-7}"
    local now=$(date -u +%s)

    # Priority 1: operator-attested PASS marker.
    if [[ -f "${smoke_dir}/.last_pass" ]]; then
        local mtime
        mtime=$(stat -f %m "${smoke_dir}/.last_pass" 2>/dev/null || stat -c %Y "${smoke_dir}/.last_pass" 2>/dev/null || echo 0)
        local age_days=$(( (now - mtime) / 86400 ))
        if [[ "$age_days" -le "$window_days" ]]; then
            echo "$TIER_READY"
            return
        fi
        # Marker exists but is stale — operator's last verification has
        # aged out. Treat as WAITING (re-run the smoke), not READY.
        echo "$TIER_WAITING"
        return
    fi

    # No operator-attested marker → not READY regardless of how many
    # smoke logs the directory contains. Smoke logs alone can't be
    # distinguished from FAIL runs without parsing each one + applying
    # the PASS gates. Forcing the operator to touch .last_pass after a
    # confirmed PASS is explicit-not-implicit and prevents a stale-FAIL
    # log from looking like readiness.
    if [[ ! -d "$smoke_dir" ]]; then
        echo "$TIER_WAITING"
        return
    fi
    # Directory exists, no marker — smoke runs may have happened but
    # none attested. WAITING is the right tier (operator must run
    # smoke + attest); not BLOCKED (no failure observed).
    echo "$TIER_WAITING"
}

# Inspect Layer 3 testnet journal dir + decide tier.
# READY only if:
#   - testnet journal dir exists
#   - it contains ≥1 .jsonl file
#   - the oldest "open" event is ≥7 days old (locked rule)
#   - layer3_verdict.sh exit 0 (rehearsal does NOT invoke it — too expensive)
#
# In practice, the rehearsal can only check the FIRST 3 conditions
# without building journal_diff (which the operator does separately
# via layer3_verdict). So we report:
#   No dir / empty dir → N/A (Layer 2 must come first)
#   Dir exists but <7d data → WAITING
#   Dir exists with ≥7d data → WAITING with note "ready for
#       layer3_verdict.sh invocation"
# Operator runs the actual verdict separately and attests via
# .last_layer3_pass marker.
layer3_tier() {
    local testnet_dir="$1"
    local now=$(date -u +%s)

    # Priority 1: operator-attested PASS marker.
    if [[ -f "${testnet_dir}/.last_layer3_pass" ]]; then
        local mtime
        mtime=$(stat -f %m "${testnet_dir}/.last_layer3_pass" 2>/dev/null || stat -c %Y "${testnet_dir}/.last_layer3_pass" 2>/dev/null || echo 0)
        local age_days=$(( (now - mtime) / 86400 ))
        if [[ "$age_days" -le 7 ]]; then
            echo "$TIER_READY"
            return
        fi
        echo "$TIER_WAITING"
        return
    fi

    # No testnet dir at all → N/A (Layer 2 hasn't been done yet — this
    # phase isn't your turn).
    if [[ ! -d "$testnet_dir" ]]; then
        echo "$TIER_NA"
        return
    fi

    # Dir exists. Has any jsonl?
    if ! find "$testnet_dir" -maxdepth 3 -name '*.jsonl' -print -quit 2>/dev/null | grep -q .; then
        echo "$TIER_NA"
        return
    fi

    # Has data — operator must run layer3_verdict + attest. WAITING.
    echo "$TIER_WAITING"
}

# Test entry-point guard.
if [[ "${BASH_SOURCE[0]}" != "${0}" ]]; then
    return 0
fi

# ─── Args ───────────────────────────────────────────────────────────────────
VPS_TARGET="root@178.105.24.230"
LOCAL_MODE=0
QUIET=0
SINGLE_PHASE=0
LAYER2_WINDOW_DAYS=7
LAYER2_SMOKE_DIR="${ROOT}/logs/layer2_smoke"
LAYER3_TESTNET_DIR="${ROOT}/logs/journal-layer3-testnet"

while [[ $# -gt 0 ]]; do
    case "$1" in
        --local) LOCAL_MODE=1; shift ;;
        --quiet) QUIET=1; shift ;;
        --phase) SINGLE_PHASE="$2"; shift 2 ;;
        --layer2-smoke-window-days) LAYER2_WINDOW_DAYS="$2"; shift 2 ;;
        --layer2-smoke-dir) LAYER2_SMOKE_DIR="$2"; shift 2 ;;
        --layer3-testnet-dir) LAYER3_TESTNET_DIR="$2"; shift 2 ;;
        --help|-h) sed -n '2,60p' "$0" | sed 's/^# \{0,1\}//'; exit "$EXIT_READY" ;;
        --*) echo "unknown flag: $1" >&2; exit "$EXIT_INPUT_ERR" ;;
        *) VPS_TARGET="$1"; shift ;;
    esac
done

if [[ "$LOCAL_MODE" == "1" ]] && [[ "$VPS_TARGET" != "root@178.105.24.230" ]]; then
    echo "ERROR: --local and an explicit VPS target are mutually exclusive." >&2
    exit "$EXIT_INPUT_ERR"
fi

if [[ "$SINGLE_PHASE" != "0" ]] && ! [[ "$SINGLE_PHASE" =~ ^[1-5]$ ]]; then
    echo "ERROR: --phase must be 1-5, got: $SINGLE_PHASE" >&2
    exit "$EXIT_INPUT_ERR"
fi

# ─── Tool presence checks ───────────────────────────────────────────────────
RESOLUTION_PY="${SCRIPT_DIR}/forward_paper_resolution.py"
PROMOTE_CHECK_PY="${SCRIPT_DIR}/stage_promotion_check.py"
KILL_CHECK_PY="${SCRIPT_DIR}/kill_protocol_check.py"

for helper in "$RESOLUTION_PY" "$PROMOTE_CHECK_PY" "$KILL_CHECK_PY"; do
    if [[ ! -f "$helper" ]]; then
        echo "ERROR: required helper not found: $helper" >&2
        exit "$EXIT_INPUT_ERR"
    fi
done

# ─── Phase runners ──────────────────────────────────────────────────────────

# Each phase: capture exit code + emit tier sentinel + cache human label.
PHASE_1_TIER=""; PHASE_1_DETAIL=""
PHASE_2_TIER=""; PHASE_2_DETAIL=""
PHASE_3_TIER=""; PHASE_3_DETAIL=""
PHASE_4_TIER=""; PHASE_4_DETAIL=""
PHASE_5_TIER=""; PHASE_5_DETAIL=""

# Build the --live-source arg block for the Python decision-grade tools.
LIVE_ARGS=()
if [[ "$LOCAL_MODE" == "1" ]]; then
    LIVE_ARGS=(--live-source local --live-dir "${ROOT}/logs/journal")
else
    LIVE_ARGS=(--live-source vps --vps "$VPS_TARGET")
fi

run_phase_1() {
    set +e
    local out rc
    out=$(python3 "$RESOLUTION_PY" --json 2>&1)
    rc=$?
    set -e
    PHASE_1_TIER=$(resolution_tier "$rc")
    PHASE_1_DETAIL="LIMBO exit ${rc} → $(_limbo_label "$rc")"
}

_limbo_label() {
    case "$1" in
        0) echo "CONTINUE" ;;
        1) echo "PROMOTE" ;;
        2) echo "WATCH" ;;
        3) echo "OPERATOR_REVIEW" ;;
        4) echo "KILL" ;;
        5) echo "INPUT_ERROR" ;;
        *) echo "UNKNOWN" ;;
    esac
}

run_phase_2() {
    set +e
    local rc
    python3 "$PROMOTE_CHECK_PY" --from-stage STAGE_0 "${LIVE_ARGS[@]}" >/dev/null 2>&1
    rc=$?
    set -e
    PHASE_2_TIER=$(promote_check_tier "$rc")
    PHASE_2_DETAIL="stage_promotion_check exit ${rc}"
}

run_phase_3() {
    set +e
    local rc
    python3 "$KILL_CHECK_PY" --stage STAGE_0 "${LIVE_ARGS[@]}" >/dev/null 2>&1
    rc=$?
    set -e
    PHASE_3_TIER=$(kill_check_tier "$rc")
    PHASE_3_DETAIL="kill_protocol_check exit ${rc}"
}

run_phase_4() {
    PHASE_4_TIER=$(layer2_tier "$LAYER2_SMOKE_DIR" "$LAYER2_WINDOW_DAYS")
    if [[ "$PHASE_4_TIER" == "$TIER_READY" ]]; then
        PHASE_4_DETAIL="layer2 .last_pass within ${LAYER2_WINDOW_DAYS}d"
    elif [[ -f "${LAYER2_SMOKE_DIR}/.last_pass" ]]; then
        PHASE_4_DETAIL="layer2 .last_pass STALE (>${LAYER2_WINDOW_DAYS}d) — re-run smoke + touch marker"
    elif [[ -d "$LAYER2_SMOKE_DIR" ]]; then
        PHASE_4_DETAIL="layer2 smoke runs found, no .last_pass attestation"
    else
        PHASE_4_DETAIL="no layer2_smoke runs yet — operator must invoke + attest"
    fi
}

run_phase_5() {
    PHASE_5_TIER=$(layer3_tier "$LAYER3_TESTNET_DIR")
    if [[ "$PHASE_5_TIER" == "$TIER_READY" ]]; then
        PHASE_5_DETAIL="layer3 .last_layer3_pass within 7d"
    elif [[ ! -d "$LAYER3_TESTNET_DIR" ]]; then
        PHASE_5_DETAIL="no testnet journal dir — Layer 2 must complete first"
    else
        PHASE_5_DETAIL="testnet data exists; operator must run layer3_verdict + attest"
    fi
}

# ─── Run phases ─────────────────────────────────────────────────────────────

if [[ "$SINGLE_PHASE" == "0" ]] || [[ "$SINGLE_PHASE" == "1" ]]; then run_phase_1; fi
if [[ "$SINGLE_PHASE" == "0" ]] || [[ "$SINGLE_PHASE" == "2" ]]; then run_phase_2; fi
if [[ "$SINGLE_PHASE" == "0" ]] || [[ "$SINGLE_PHASE" == "3" ]]; then run_phase_3; fi
if [[ "$SINGLE_PHASE" == "0" ]] || [[ "$SINGLE_PHASE" == "4" ]]; then run_phase_4; fi
if [[ "$SINGLE_PHASE" == "0" ]] || [[ "$SINGLE_PHASE" == "5" ]]; then run_phase_5; fi

# ─── Aggregate ──────────────────────────────────────────────────────────────

if [[ "$SINGLE_PHASE" != "0" ]]; then
    # Single-phase mode: report just that phase + exit on its tier directly.
    eval "tier=\$PHASE_${SINGLE_PHASE}_TIER"
    eval "detail=\$PHASE_${SINGLE_PHASE}_DETAIL"
    case "$tier" in
        "$TIER_READY")     phase_exit=0 ;;
        "$TIER_BLOCKED")   phase_exit=1 ;;
        "$TIER_WAITING")   phase_exit=2 ;;
        "$TIER_INPUT_ERR") phase_exit=3 ;;
        "$TIER_NA")        phase_exit=2 ;;  # treat N/A like WAITING for cron-tier mapping
        *)                 phase_exit=3 ;;
    esac
    if [[ "$QUIET" == "1" ]]; then
        echo "promotion_rehearsal phase=${SINGLE_PHASE}: ${tier} — ${detail}"
    else
        echo "Phase ${SINGLE_PHASE}: [${tier}]  ${detail}"
    fi
    exit "$phase_exit"
fi

AGG=$(aggregate_tiers "$PHASE_1_TIER" "$PHASE_2_TIER" "$PHASE_3_TIER" \
                      "$PHASE_4_TIER" "$PHASE_5_TIER")
AGG_LABEL=$(aggregate_label "$AGG")

if [[ "$QUIET" == "1" ]]; then
    echo "promotion_rehearsal: ${AGG_LABEL} — p1=${PHASE_1_TIER} p2=${PHASE_2_TIER} p3=${PHASE_3_TIER} p4=${PHASE_4_TIER} p5=${PHASE_5_TIER}"
    exit "$AGG"
fi

# Verbose mode: full output.
SEP=$(printf '%0.s═' {1..78})
echo "$SEP"
echo "  PROMOTION REHEARSAL  STAGE_0 → STAGE_1"
echo "  $(date -u '+%Y-%m-%dT%H:%M:%SZ')  →  ${VPS_TARGET}${LOCAL_MODE:+ (local)}"
echo "  Locked path: real_money_protocol_decision_rule_2026-05-08.md"
echo "$SEP"
echo
echo "  Phase  Label                                  Status      Detail"
echo "  ─────  ─────────────────────────────────────  ──────────  ─────────────────────────"
printf "  %-5s  %-37s  [%-8s]  %s\n" "1" "Forward-paper resolution (LIMBO)" "$PHASE_1_TIER" "$PHASE_1_DETAIL"
printf "  %-5s  %-37s  [%-8s]  %s\n" "2" "STAGE_0 → STAGE_1 locked gates (9)" "$PHASE_2_TIER" "$PHASE_2_DETAIL"
printf "  %-5s  %-37s  [%-8s]  %s\n" "3" "Kill criteria absence (6)" "$PHASE_3_TIER" "$PHASE_3_DETAIL"
printf "  %-5s  %-37s  [%-8s]  %s\n" "4" "Layer 2 testnet readiness" "$PHASE_4_TIER" "$PHASE_4_DETAIL"
printf "  %-5s  %-37s  [%-8s]  %s\n" "5" "Layer 3 shadow parity" "$PHASE_5_TIER" "$PHASE_5_DETAIL"
echo

echo "$SEP"
case "$AGG" in
    0) echo "  >>> AGGREGATE: READY — operator may execute stage_promotion.sh" ;;
    1) echo "  >>> AGGREGATE: BLOCKED — ≥1 phase has a real problem; DO NOT promote" ;;
    2) echo "  >>> AGGREGATE: WAITING — phases incomplete; no action required" ;;
    3) echo "  >>> AGGREGATE: INPUT_ERR — config/helper issue; investigate before re-run" ;;
esac
echo "$SEP"
echo
echo "  Notes:"
echo "  - This script never mutates state. Phases 1-3 invoke read-only Python"
echo "    decision-grade evaluators; phases 4-5 inspect on-disk attestation markers."
echo "  - Layer 2 READY requires operator-touched ${LAYER2_SMOKE_DIR}/.last_pass"
echo "    within ${LAYER2_WINDOW_DAYS} days. Touch after manually verifying"
echo "    layer2_smoke.sh PASS against real testnet credentials."
echo "  - Layer 3 READY requires operator-touched ${LAYER3_TESTNET_DIR}/.last_layer3_pass"
echo "    within 7 days. Touch after manually verifying layer3_verdict.sh PASS."
echo "  - The attestation-marker pattern is explicit-not-implicit: a smoke log"
echo "    in the dir could be a FAIL run; the marker is the operator's signature."

exit "$AGG"
