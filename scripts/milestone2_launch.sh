#!/usr/bin/env bash
# milestone2_launch.sh — orchestrator for the milestone-2 candidate sweep.
#
# Implements the locked execution sequence from
# results/milestone2_runbook_decision_rule_2026-05-10.md:
#   Phase 1: A2 (ATR-targeted sizing)  → composes with everything else
#   Phase 2: A1 (vol-regime filter)
#   Phase 3: C1 (funding-extremum reversal)  + Cat X Bybit replication
#   Phase 4: B2 (BB squeeze release)
#   Phase 5: D1 (session filter)
#
# Per the backlog discipline contract, this script REFUSES to execute the
# sweep until milestone-2's trigger fires (paper→STAGE_1 promotion completed
# OR deployed strategy killed). The trigger gate is the FIRST check; without
# it, exit 2 (TRIGGER_NOT_DETECTED) is returned and no phase runs.
#
# Designed per docs/AUDIT_LENS.md lens-as-design-tool: distinct exit codes
# per failure shape, no `|| true` swallowing, noisy trigger failure, per-phase
# verdict-file gate before advancing. Each shape gets a Telegram tier.
#
# Exit codes:
#   0  ALL_VERDICTS_WRITTEN — every phase completed with a verdict artifact
#                              and no CONTRADICTION halt
#   1  PHASE_FAILED_VERDICT_INCONCLUSIVE — phase ran but verdict file
#                                            missing or unparseable
#   2  TRIGGER_NOT_DETECTED — milestone-2 trigger gate not satisfied
#                              (this is the expected pre-milestone-2 state)
#   3  ENV_OR_INPUT_ERROR — missing binary, missing data, missing helper
#                            script, malformed env override
#   4  PHASE_FAILED_RUNTIME_ERROR — phase runner exited non-zero
#   5  CONTRADICTION_HALTED — verdict CONTRADICTION written; sequence
#                              stopped per runbook §rollback-A
#
# Telegram tier mapping (per telegram_alert_design_decision_rule_2026-05-08):
#   exit 0 → INFO (sequence complete)
#   exit 1 → CRITICAL (verdict integrity compromised — operator must verify)
#   exit 2 → WARN (operator misconfiguration: ran without trigger)
#   exit 3 → CRITICAL (cannot proceed safely)
#   exit 4 → CRITICAL (phase runner crashed mid-sequence)
#   exit 5 → CRITICAL (mechanical halt; convene rule-update review)
#
# Usage:
#   bash scripts/milestone2_launch.sh                       # full sequence
#   bash scripts/milestone2_launch.sh --dry-run             # trigger check + scaffolding only
#   MILESTONE2_RESUME=phase3 bash scripts/milestone2_launch.sh
#                                                            # resume from phase 3
#   MILESTONE2_OVERRIDE=YES_I_UNDERSTAND \
#     MILESTONE2_OVERRIDE_REASON="reason text" \
#     bash scripts/milestone2_launch.sh                      # operator override
#
# Override audit: any invocation with MILESTONE2_OVERRIDE writes
# results/milestone2_override_<date>.md before any phase runs.

set -euo pipefail

# ── Resolve paths ────────────────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
RESULTS_DIR="${ROOT}/results"
DATE_TAG=$(date -u +%F)

# shellcheck disable=SC1091
source "${SCRIPT_DIR}/lib/notify.sh"

# ── Exit-code constants ──────────────────────────────────────────────────────
readonly EXIT_ALL_VERDICTS_WRITTEN=0
readonly EXIT_VERDICT_INCONCLUSIVE=1
readonly EXIT_TRIGGER_NOT_DETECTED=2
readonly EXIT_ENV_OR_INPUT_ERROR=3
readonly EXIT_PHASE_RUNTIME_ERROR=4
readonly EXIT_CONTRADICTION_HALTED=5

# ── Helpers ──────────────────────────────────────────────────────────────────

die() {
    # die <exit_code> <subject> <body>
    # Routes to Telegram tier per the mapping above and exits.
    local code="$1" subject="$2" body="${3:-}"
    local tier
    case "$code" in
        "$EXIT_ALL_VERDICTS_WRITTEN") tier="INFO" ;;
        "$EXIT_VERDICT_INCONCLUSIVE"|"$EXIT_ENV_OR_INPUT_ERROR"|"$EXIT_PHASE_RUNTIME_ERROR"|"$EXIT_CONTRADICTION_HALTED") tier="CRITICAL" ;;
        "$EXIT_TRIGGER_NOT_DETECTED") tier="WARN" ;;
        *) tier="WARN" ;;
    esac
    notify_telegram "$tier" "milestone2_launch: $subject" "$body"
    if [[ "$code" -ne 0 ]]; then
        echo "milestone2_launch: $subject" >&2
        [[ -n "$body" ]] && echo "$body" >&2
    fi
    exit "$code"
}

# Detects milestone-2 trigger. Sets TRIGGER_KIND globally to one of:
#   stage1_promotion | strategy_kill | operator_override | ""
detect_trigger() {
    TRIGGER_KIND=""
    TRIGGER_ARTIFACT=""

    # Operator override path (audit-required)
    if [[ -n "${MILESTONE2_OVERRIDE:-}" ]]; then
        if [[ "${MILESTONE2_OVERRIDE}" != "YES_I_UNDERSTAND" ]]; then
            die "$EXIT_ENV_OR_INPUT_ERROR" \
                "override-malformed" \
                "MILESTONE2_OVERRIDE must equal 'YES_I_UNDERSTAND' literal; got '${MILESTONE2_OVERRIDE}'"
        fi
        if [[ -z "${MILESTONE2_OVERRIDE_REASON:-}" ]]; then
            die "$EXIT_ENV_OR_INPUT_ERROR" \
                "override-without-reason" \
                "MILESTONE2_OVERRIDE_REASON must be set with non-empty text when override is invoked"
        fi
        TRIGGER_KIND="operator_override"
        return 0
    fi

    # Trigger 1: paper → STAGE_1 promotion artifact with ALL_GREEN verdict.
    local promotion_artifacts
    promotion_artifacts=$(/bin/ls "${RESULTS_DIR}"/stage_promotion_*.md 2>/dev/null || true)
    if [[ -n "$promotion_artifacts" ]]; then
        while IFS= read -r artifact; do
            [[ -z "$artifact" ]] && continue
            if grep -q "^Composite verdict: ALL_GREEN" "$artifact" 2>/dev/null \
               && grep -q "paper.*STAGE_1\|paper→STAGE_1\|paper -> STAGE_1" "$artifact" 2>/dev/null; then
                TRIGGER_KIND="stage1_promotion"
                TRIGGER_ARTIFACT="$artifact"
                return 0
            fi
        done <<< "$promotion_artifacts"
    fi

    # Trigger 2: deployed strategy kill artifact.
    # Filter is intentionally strict — the results/ directory contains many
    # kill-related ANALYSIS docs (kill_bar_calibration_*, kill_bar_recal_*)
    # that are NOT actual kill events; they're decision rules ABOUT the kill
    # bar threshold. An actual kill event artifact follows the naming
    # convention from auto_kill_execution_decision_rule_2026-05-08.md Phase 4
    # (DOCUMENT) and contains the marker "HARD KILL" or "SOFT KILL" in its
    # body. We require BOTH the filename pattern (excludes _rule_/_verdict_/
    # _calibration_/_recal_) AND a content marker.
    local kill_artifacts
    kill_artifacts=$(/bin/ls "${RESULTS_DIR}"/kill_*.md 2>/dev/null \
                     | grep -vE "_(rule|verdict|calibration|recal|bar)_|kill_bar_" \
                     || true)
    if [[ -n "$kill_artifacts" ]]; then
        local artifact
        while IFS= read -r artifact; do
            [[ -z "$artifact" ]] && continue
            if grep -qE "HARD KILL|SOFT KILL|auto_kill_execution Phase" "$artifact" 2>/dev/null; then
                TRIGGER_KIND="strategy_kill"
                TRIGGER_ARTIFACT="$artifact"
                return 0
            fi
        done <<< "$kill_artifacts"
    fi

    # No trigger.
    return 1
}

# Restores A2_VERDICT from the most recent phase-1 verdict artifact when
# resuming at phase ≥ 2. The runbook locks the baseline-inheritance contract:
# "If A2 ADOPT, every subsequent candidate is evaluated on TOP of A2-sized
# baseline." Resuming without restoring A2_VERDICT would silently violate
# this contract — downstream runners see empty A2_VERDICT and default to the
# constant-stake baseline regardless of the actual phase-1 verdict.
#
# Finds the latest m2_phase1_a2_verdict_*.md by mtime (the date suffix is
# whatever DATE_TAG was when phase 1 ran, not necessarily today). If no
# phase-1 verdict is present, exits with EXIT_ENV_OR_INPUT_ERROR — operator
# must either resume from phase1 or fabricate the verdict file first.
restore_a2_verdict_on_resume() {
    local artifacts
    artifacts=$(/bin/ls -t "${RESULTS_DIR}"/m2_phase1_a2_verdict_*.md 2>/dev/null || true)
    if [[ -z "$artifacts" ]]; then
        die "$EXIT_ENV_OR_INPUT_ERROR" \
            "resume-missing-phase1-verdict" \
            "MILESTONE2_RESUME=${RESUME_PHASE} requires the phase-1 verdict to restore A2_VERDICT (baseline-inheritance contract). No file matched ${RESULTS_DIR}/m2_phase1_a2_verdict_*.md. Either resume from phase1 or restore the artifact from VCS before retrying."
    fi
    local latest
    latest=$(printf '%s\n' "$artifacts" | head -n 1)
    local verdict_line
    verdict_line=$(grep -m 1 "^Mechanical verdict:" "$latest" 2>/dev/null || true)
    if [[ -z "$verdict_line" ]]; then
        die "$EXIT_VERDICT_INCONCLUSIVE" \
            "resume-phase1-verdict-unparseable" \
            "Phase-1 verdict artifact ${latest} lacks 'Mechanical verdict:' line. Cannot restore A2_VERDICT for resume. Reformat per runbook §per-phase-verdict-template."
    fi
    case "$verdict_line" in
        *ADOPT*)         A2_VERDICT="ADOPT" ;;
        *REJECT*)        A2_VERDICT="REJECT" ;;
        *HOLD*)          A2_VERDICT="HOLD" ;;
        *CONTRADICTION*)
            die "$EXIT_CONTRADICTION_HALTED" \
                "resume-on-contradicted-phase1" \
                "Phase-1 verdict ${latest} is CONTRADICTION. Per runbook §rollback-A, this sequence was already halted and requires a rule-update review before resuming. Do not bypass with MILESTONE2_RESUME."
            ;;
        *)
            die "$EXIT_VERDICT_INCONCLUSIVE" \
                "resume-phase1-verdict-unrecognized" \
                "Phase-1 verdict line '${verdict_line}' from ${latest} contains none of {ADOPT, REJECT, HOLD, CONTRADICTION}. Operator must use the locked four-state vocabulary."
            ;;
    esac
    export A2_VERDICT
    echo "→ Restored A2_VERDICT=${A2_VERDICT} from $(basename "$latest")"
}

# Records the override invocation as an audit artifact (per runbook).
write_override_audit() {
    local audit="${RESULTS_DIR}/milestone2_override_${DATE_TAG}.md"
    cat > "$audit" <<EOF
# Milestone-2 launch override — operator audit artifact

**Invoked:** $(date -u '+%Y-%m-%d %H:%M:%S UTC')
**Operator reason:** ${MILESTONE2_OVERRIDE_REASON}
**Commit hash:** $(git -C "$ROOT" rev-parse HEAD 2>/dev/null || echo "unknown")

This artifact documents an out-of-band milestone-2 launch under
\`MILESTONE2_OVERRIDE=YES_I_UNDERSTAND\`. The operator asserted the
launch is justified despite neither STAGE_1 promotion nor strategy-kill
artifact being present.

Per \`results/milestone2_runbook_decision_rule_2026-05-10.md\` §trigger,
this audit MUST exist for the override to proceed. Future-self reading
this should treat it as the gate decision: was the reason adequate?

If this artifact exists without a corresponding milestone-1-closure
explanation in the surrounding context, that's a discipline violation
worth investigating.
EOF
    echo "→ Override audit written: $audit"
}

# Pre-flight environment checks: binary, data, funding, helper scripts.
preflight() {
    local errors=()

    # Backtest binary
    if [[ ! -x "${ROOT}/bin/backtest" ]] && ! command -v go >/dev/null 2>&1; then
        errors+=("Neither bin/backtest nor 'go' available — cannot build/run")
    fi

    # Data — at minimum require deployed-symbol CSVs spanning the train/test split
    local sample_data="${ROOT}/data/BTCUSDT-1m-2024-01.csv"
    if [[ ! -f "$sample_data" ]]; then
        errors+=("Sample data file missing: $sample_data — verify data/ provisioning")
    fi

    # Funding (post-loader-fix)
    local sample_funding="${ROOT}/data/funding/BTCUSDT.csv"
    if [[ ! -f "$sample_funding" ]]; then
        errors+=("Funding CSV missing: $sample_funding — milestone-2 candidates A2/C1 require funding accrual")
    fi

    # Per-phase runner scripts. These are NOT yet implemented (per backlog
    # discipline: "do NOT execute before milestone 2 begins"). The orchestrator
    # checks for their presence to surface "you've reached milestone 2 — write
    # the per-phase runners first" as a clear distinct error rather than a
    # silent skip.
    # macOS ships bash 3.2 (no ${var^^}); use tr for portability. The candidate
    # IDs are short fixed strings so the subshell cost is negligible.
    local phase
    for phase in a2 a1 c1 b2 d1; do
        local runner="${SCRIPT_DIR}/m2_phase_${phase}.sh"
        if [[ ! -x "$runner" ]]; then
            local phase_upper
            phase_upper=$(printf '%s' "$phase" | tr '[:lower:]' '[:upper:]')
            errors+=("Phase runner not found or not executable: $runner — implement per backlog §${phase_upper}")
        fi
    done

    if [[ ${#errors[@]} -gt 0 ]]; then
        local body=""
        local err
        for err in "${errors[@]}"; do
            body="${body}- ${err}
"
        done
        die "$EXIT_ENV_OR_INPUT_ERROR" "preflight-failed" "$body"
    fi
}

# Run a single phase. Args: phase_num candidate
# Reads the verdict from results/m2_phase{N}_{candidate}_verdict_<date>.md
# and parses the "Mechanical verdict:" line for ADOPT/REJECT/HOLD/CONTRADICTION.
run_phase() {
    local phase_num="$1" candidate="$2"
    local runner="${SCRIPT_DIR}/m2_phase_${candidate}.sh"
    local verdict_artifact="${RESULTS_DIR}/m2_phase${phase_num}_${candidate}_verdict_${DATE_TAG}.md"

    local candidate_upper
    candidate_upper=$(printf '%s' "$candidate" | tr '[:lower:]' '[:upper:]')
    echo
    echo "── Phase ${phase_num}: ${candidate_upper} ──"
    echo "Runner: $runner"
    echo "Verdict artifact (expected): $verdict_artifact"

    # Capture exit code separately — no `|| true` swallowing.
    set +e
    "$runner"
    local rc=$?
    set -e

    if [[ $rc -ne 0 ]]; then
        die "$EXIT_PHASE_RUNTIME_ERROR" \
            "phase${phase_num}-${candidate}-crashed" \
            "Phase ${phase_num} runner ($runner) exited ${rc}. Verdict not written. Sequence halted."
    fi

    if [[ ! -f "$verdict_artifact" ]]; then
        die "$EXIT_VERDICT_INCONCLUSIVE" \
            "phase${phase_num}-${candidate}-no-verdict" \
            "Phase ${phase_num} runner returned 0 but verdict artifact ${verdict_artifact} was not written. Cannot advance — verdict integrity compromised."
    fi

    # Parse the mechanical verdict line.
    local verdict_line
    verdict_line=$(grep -m 1 "^Mechanical verdict:" "$verdict_artifact" 2>/dev/null || true)
    if [[ -z "$verdict_line" ]]; then
        die "$EXIT_VERDICT_INCONCLUSIVE" \
            "phase${phase_num}-${candidate}-unparseable" \
            "Phase ${phase_num} verdict artifact exists but lacks 'Mechanical verdict:' line. Operator must reformat per runbook §per-phase-verdict-template."
    fi

    PHASE_VERDICT=""
    case "$verdict_line" in
        *ADOPT*)         PHASE_VERDICT="ADOPT" ;;
        *REJECT*)        PHASE_VERDICT="REJECT" ;;
        *HOLD*)          PHASE_VERDICT="HOLD" ;;
        *CONTRADICTION*) PHASE_VERDICT="CONTRADICTION" ;;
        *)
            die "$EXIT_VERDICT_INCONCLUSIVE" \
                "phase${phase_num}-${candidate}-unrecognized-verdict" \
                "Verdict line '${verdict_line}' contains none of {ADOPT, REJECT, HOLD, CONTRADICTION}. Operator must use the locked four-state vocabulary."
            ;;
    esac

    echo "Phase ${phase_num} verdict: ${PHASE_VERDICT}"

    if [[ "$PHASE_VERDICT" == "CONTRADICTION" ]]; then
        die "$EXIT_CONTRADICTION_HALTED" \
            "phase${phase_num}-${candidate}-contradiction" \
            "Phase ${phase_num} produced CONTRADICTION verdict. Per runbook §rollback-A, sequence is halted. Convene rule-update review and document in results/milestone2_contradiction_${DATE_TAG}.md before resuming with MILESTONE2_RESUME=phase$((phase_num+1))."
    fi
}

# ── Main ─────────────────────────────────────────────────────────────────────

DRY_RUN=0
if [[ "${1:-}" == "--dry-run" ]]; then
    DRY_RUN=1
    shift
fi

echo "milestone2_launch — $(date -u '+%Y-%m-%d %H:%M UTC')"
echo "Trigger gate check…"

if ! detect_trigger; then
    die "$EXIT_TRIGGER_NOT_DETECTED" \
        "trigger-not-detected" \
        "Neither paper→STAGE_1 promotion artifact (results/stage_promotion_*.md with ALL_GREEN) nor strategy-kill artifact (results/kill_*.md) found. Per backlog discipline: do NOT execute milestone-2 candidates before milestone-2 begins. Use MILESTONE2_OVERRIDE=YES_I_UNDERSTAND with MILESTONE2_OVERRIDE_REASON=\"…\" only with explicit justification."
fi

echo "Trigger detected: ${TRIGGER_KIND}"
[[ -n "$TRIGGER_ARTIFACT" ]] && echo "Trigger artifact: ${TRIGGER_ARTIFACT}"

if [[ "$TRIGGER_KIND" == "operator_override" ]]; then
    write_override_audit
fi

echo "Pre-flight environment checks…"
preflight

if [[ "$DRY_RUN" -eq 1 ]]; then
    echo
    echo "── DRY RUN — trigger gate + preflight passed; phases NOT executed ──"
    echo "To run for real: re-invoke without --dry-run"
    notify_telegram INFO "milestone2_launch dry-run OK" \
        "Trigger: ${TRIGGER_KIND} | Preflight clean | Phases skipped"
    exit "$EXIT_ALL_VERDICTS_WRITTEN"
fi

# Resume support: skip phases below the resume point.
RESUME_PHASE="${MILESTONE2_RESUME:-phase1}"
case "$RESUME_PHASE" in
    phase1) START=1 ;;
    phase2) START=2 ;;
    phase3) START=3 ;;
    phase4) START=4 ;;
    phase5) START=5 ;;
    *)
        die "$EXIT_ENV_OR_INPUT_ERROR" "invalid-resume-point" \
            "MILESTONE2_RESUME must be one of phase1..phase5; got '${MILESTONE2_RESUME:-}'"
        ;;
esac

# Locked execution sequence per runbook §locked-execution-sequence.
PHASES=("a2" "a1" "c1" "b2" "d1")

A2_VERDICT=""
# Bash 3.2 (macOS default) lacks associative arrays, so verdicts are tracked
# via parallel indexed arrays VERDICTS[i] paired with PHASES[i]. Restoring
# A2_VERDICT on resume pre-fills the slot so the summary stays complete even
# when phase 1 is skipped.
VERDICTS=("" "" "" "" "")

# Resume baseline-inheritance: if we're skipping phase 1, the phase-1 verdict
# must be re-loaded from disk so downstream phases inherit the locked
# A2-sized vs constant-stake baseline. Skipping this would silently default
# every downstream phase to constant-stake regardless of the actual phase-1
# outcome — violating the locked baseline-inheritance contract.
if [[ "$START" -gt 1 ]]; then
    restore_a2_verdict_on_resume
    VERDICTS[0]="$A2_VERDICT"
fi

for i in "${!PHASES[@]}"; do
    PHASE_NUM=$((i + 1))
    if [[ "$PHASE_NUM" -lt "$START" ]]; then
        SKIPPED_UPPER=$(printf '%s' "${PHASES[$i]}" | tr '[:lower:]' '[:upper:]')
        echo "── Phase ${PHASE_NUM}: ${SKIPPED_UPPER} (skipped via MILESTONE2_RESUME) ──"
        continue
    fi
    run_phase "$PHASE_NUM" "${PHASES[$i]}"
    VERDICTS[i]="$PHASE_VERDICT"
    if [[ "$PHASE_NUM" -eq 1 ]]; then
        A2_VERDICT="$PHASE_VERDICT"
        # Export so per-phase runners 2-5 can inherit the A2-sized vs constant-stake
        # baseline decision (per runbook: "If A2 ADOPT, every subsequent candidate
        # is evaluated on TOP of A2-sized baseline").
        export A2_VERDICT
    fi
done

# Summary
echo
echo "── All phases complete ──"
SUMMARY=""
for i in "${!PHASES[@]}"; do
    SUMMARY="${SUMMARY}${PHASES[$i]}=${VERDICTS[$i]} "
done
echo "Verdicts: ${SUMMARY}"
notify_telegram INFO "milestone2_launch sequence complete" \
    "Trigger: ${TRIGGER_KIND}
Verdicts: ${SUMMARY}
Per runbook §rollback-B: ADOPT verdicts roll out via stage_promotion_runbook, one at a time, ≥30d apart."

exit "$EXIT_ALL_VERDICTS_WRITTEN"
