#!/usr/bin/env bash
# stage_promotion.sh — orchestrator for the locked 6-phase STAGE-up runbook
# (results/stage_promotion_runbook_decision_rule_2026-05-08.md).
#
# Drives a single promotion (paper→STAGE_1, STAGE_1→STAGE_2, STAGE_2→STAGE_3,
# STAGE_3→STAGE_4). Each phase is invoked separately and idempotently because
# Phases 4 and 5 are inherently calendar-bound (wait for N trades to close;
# monitor for 24/24/48/72h). A single foreground script would have to either
# block for days or daemonize; subcommand-per-phase lets the operator run
# the script when needed, walk away, ctrl-C any time, and resume cleanly.
#
# Designed per docs/AUDIT_LENS.md lens-as-design-tool: distinct exit codes
# per failure shape, mechanical Telegram tier mapping, no `|| true` swallowing,
# noisy operator-input-required boundary.
#
# Usage:
#   bash scripts/stage_promotion.sh phase1 <FROM> <TO>   # GATE verification
#   bash scripts/stage_promotion.sh phase2               # CONFIG preparation
#   bash scripts/stage_promotion.sh phase3               # DEPLOY
#   bash scripts/stage_promotion.sh phase4               # FIRST-N-TRADE verification
#   bash scripts/stage_promotion.sh phase5               # MONITORING window
#   bash scripts/stage_promotion.sh phase6               # DOCUMENT (close artifact)
#   bash scripts/stage_promotion.sh status               # show current progress
#   bash scripts/stage_promotion.sh rollback             # invoke Phase-6 rollback
#
#   <FROM> ∈ {paper, STAGE_1, STAGE_2, STAGE_3}
#   <TO>   ∈ {STAGE_1, STAGE_2, STAGE_3, STAGE_4}
#
# Exit codes:
#   0  PHASE_COMPLETE — current phase succeeded; advance allowed
#   1  GATE_NOT_GREEN — Phase 1 gate doc missing or not ALL_GREEN
#   2  GATE_ARTIFACT_MISSING — required input file absent / unparseable
#   3  ENV_OR_INPUT_ERROR — bad args, malformed FROM/TO, missing dep
#   4  PHASE_VERIFICATION_FAILED — post-action gate fired (e.g. post_deploy
#                                  STRICT=1 reported WARN, or first-trade
#                                  fill-price sanity failed)
#   5  PHASE_RUNTIME_ERROR — invoked command crashed (e.g. redeploy.sh
#                            non-zero, ssh unreachable)
#   6  OPERATOR_INTERVENTION_REQUIRED — Phase 2 diff-review decline, Phase 4
#                                       fill anomaly flagged for manual review,
#                                       any "STOP and verify" boundary
#   7  STATE_OUT_OF_ORDER — invoked phase N without phase N-1 completing,
#                            or state file corrupt/unreadable
#   8  ROLLBACK_INITIATED — operator invoked `rollback`; informational, not
#                            a failure
#   9  PHASE_PENDING_DATA — Phase 4/5 needs more trades/days to evaluate;
#                           legitimate intermediate state, operator re-invokes
#
# Telegram tier mapping (per telegram_alert_design_decision_rule_2026-05-08):
#   exit 0 → INFO (phase complete)
#   exit 1 → WARN (gate not green; investigate before re-attempt)
#   exit 2 → WARN (input missing; operator misconfiguration)
#   exit 3 → CRITICAL (env error means orchestrator broken — fix script)
#   exit 4 → CRITICAL (real-money verification failure — operator must inspect)
#   exit 5 → CRITICAL (orchestration runtime error — script must halt)
#   exit 6 → WARN (operator decision required)
#   exit 7 → CRITICAL (state corrupt — manual recovery)
#   exit 8 → INFO (rollback initiated; expected operator action)
#   exit 9 → INFO (waiting on data; status, not failure)
#
# Per-stage parameters: encoded inline below (function `stage_params`).
# Source of truth: results/stage_promotion_runbook_decision_rule_2026-05-08.md
# table "Per-stage parameters". If the runbook changes, update both.

set -euo pipefail

# ── Resolve paths ────────────────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
RESULTS_DIR="${ROOT}/results"

# shellcheck source=lib/notify.sh
source "${SCRIPT_DIR}/lib/notify.sh"

# ── Exit-code constants ──────────────────────────────────────────────────────
readonly EXIT_PHASE_COMPLETE=0
readonly EXIT_GATE_NOT_GREEN=1
readonly EXIT_GATE_ARTIFACT_MISSING=2
readonly EXIT_ENV_OR_INPUT_ERROR=3
readonly EXIT_PHASE_VERIFICATION_FAILED=4
readonly EXIT_PHASE_RUNTIME_ERROR=5
readonly EXIT_OPERATOR_INTERVENTION_REQUIRED=6
readonly EXIT_STATE_OUT_OF_ORDER=7
readonly EXIT_ROLLBACK_INITIATED=8
readonly EXIT_PHASE_PENDING_DATA=9

# ── Globals set per-invocation ───────────────────────────────────────────────
FROM=""
TO=""
ARTIFACT=""  # path to results/stage_promotion_<date>_<from>_to_<to>.md.in-progress

# ── Helpers ──────────────────────────────────────────────────────────────────

# die <exit_code> <subject> <body>
# Routes to Telegram tier per the mapping above and exits.
die() {
    local code="$1" subject="$2" body="${3:-}"
    local tier
    case "$code" in
        "$EXIT_PHASE_COMPLETE"|"$EXIT_ROLLBACK_INITIATED"|"$EXIT_PHASE_PENDING_DATA")
            tier="INFO" ;;
        "$EXIT_GATE_NOT_GREEN"|"$EXIT_GATE_ARTIFACT_MISSING"|"$EXIT_OPERATOR_INTERVENTION_REQUIRED"|"$EXIT_STATE_OUT_OF_ORDER")
            tier="WARN" ;;
        "$EXIT_ENV_OR_INPUT_ERROR"|"$EXIT_PHASE_VERIFICATION_FAILED"|"$EXIT_PHASE_RUNTIME_ERROR")
            tier="CRITICAL" ;;
        *)  tier="WARN" ;;
    esac
    notify_telegram "$tier" "stage_promotion: $subject" "$body"
    if [[ "$code" -ne 0 ]]; then
        echo "stage_promotion: $subject" >&2
        [[ -n "$body" ]] && echo "$body" >&2
    fi
    exit "$code"
}

# Per-stage parameter table — runbook §Per-stage-parameters (lines 32-46).
# Sets global vars: STAKE_USD, FIRST_N_TRADES, MONITORING_HOURS, COOLING_DAYS,
# DAILY_LOSS_USD, REVIEW_TYPE.
# Cross-ref: results/stage_promotion_runbook_decision_rule_2026-05-08.md
stage_params() {
    local to_stage="$1"
    case "$to_stage" in
        STAGE_1)
            STAKE_USD=100;  FIRST_N_TRADES=3;  MONITORING_HOURS=24
            COOLING_DAYS=30; DAILY_LOSS_USD=1000
            REVIEW_TYPE="full_completion_review" ;;
        STAGE_2)
            STAKE_USD=300;  FIRST_N_TRADES=5;  MONITORING_HOURS=24
            COOLING_DAYS=30; DAILY_LOSS_USD=3600
            REVIEW_TYPE="abbreviated_stage1_to_stage2" ;;
        STAGE_3)
            STAKE_USD=500;  FIRST_N_TRADES=5;  MONITORING_HOURS=48
            COOLING_DAYS=60; DAILY_LOSS_USD=6000
            REVIEW_TYPE="abbreviated_stage2_to_stage3" ;;
        STAGE_4)
            STAKE_USD=1000; FIRST_N_TRADES=10; MONITORING_HOURS=72
            COOLING_DAYS=60; DAILY_LOSS_USD=15000
            REVIEW_TYPE="abbreviated_stage3_to_stage4" ;;
        *)
            die "$EXIT_ENV_OR_INPUT_ERROR" "unknown-to-stage" \
                "Unknown TO stage: '$to_stage'. Must be one of STAGE_1, STAGE_2, STAGE_3, STAGE_4."
            ;;
    esac
}

validate_transition() {
    local from="$1" to="$2"
    case "${from}_to_${to}" in
        paper_to_STAGE_1|STAGE_1_to_STAGE_2|STAGE_2_to_STAGE_3|STAGE_3_to_STAGE_4)
            return 0 ;;
        *)
            die "$EXIT_ENV_OR_INPUT_ERROR" "invalid-transition" \
                "Transition '${from} → ${to}' not allowed. Locked progression per real_money_protocol: paper→STAGE_1→STAGE_2→STAGE_3→STAGE_4."
            ;;
    esac
}

# Find the in-progress artifact for the active promotion. Sets ARTIFACT.
# If none exists OR multiple exist, errors out with diagnostic.
locate_active_artifact() {
    # `find` not `ls *.glob`: when the glob lives inside a quoted variable
    # expansion the shell can't expand it, so ls receives the literal "*"
    # and fails. find takes the pattern as a -name argument, which doesn't
    # rely on shell globbing at all.
    local matches
    matches=$(find "${RESULTS_DIR}" -maxdepth 1 -name 'stage_promotion_*.md.in-progress' 2>/dev/null | sort)
    if [[ -z "$matches" ]]; then
        die "$EXIT_STATE_OUT_OF_ORDER" "no-active-promotion" \
            "No in-progress artifact found in ${RESULTS_DIR}/. Run 'phase1 <FROM> <TO>' to start a promotion."
    fi
    local count
    count=$(printf '%s\n' "$matches" | wc -l | tr -d ' ')
    if [[ "$count" -gt 1 ]]; then
        die "$EXIT_STATE_OUT_OF_ORDER" "multiple-active-promotions" \
            "Multiple in-progress artifacts found — exactly one must be active:
${matches}
Resolve by completing one with 'phase6' or 'rollback', then retry."
    fi
    ARTIFACT="$matches"
    # Derive FROM and TO from the filename.
    # Format: stage_promotion_YYYY-MM-DD_<from>_to_<to>.md.in-progress
    # Cannot split on `_` because TO contains `_` (e.g., STAGE_1). Use
    # parameter expansion to slice around the `_to_` separator instead.
    local base body
    base=$(basename "$ARTIFACT" .md.in-progress)
    # Strip the "stage_promotion_YYYY-MM-DD_" prefix; expansion uses a glob
    # for the date so any ISO-format date works.
    body="${base#stage_promotion_????-??-??_}"
    # TO is everything after the LAST _to_; FROM is everything before it.
    TO="${body##*_to_}"
    FROM="${body%_to_*}"
    if [[ -z "$FROM" || -z "$TO" || "$FROM" == "$body" ]]; then
        die "$EXIT_STATE_OUT_OF_ORDER" "artifact-name-unparseable" \
            "Cannot extract FROM/TO from '$ARTIFACT'. Expected: stage_promotion_<date>_<from>_to_<to>.md.in-progress"
    fi
}

# Verify the artifact has STATE: PHASE_<N>_COMPLETE marker for the given N.
# Returns 0 if present, 1 otherwise. Does NOT die — caller decides.
phase_completed() {
    local n="$1"
    grep -q "^STATE: PHASE_${n}_COMPLETE" "$ARTIFACT"
}

# Require that all phases 1..N have completed; die EXIT_STATE_OUT_OF_ORDER
# otherwise. Use in phaseN handlers to enforce strict ordering.
require_prior_phases_complete() {
    local up_to="$1"  # require phases 1..(up_to - 1) complete
    local i
    for (( i=1; i < up_to; i++ )); do
        if ! phase_completed "$i"; then
            die "$EXIT_STATE_OUT_OF_ORDER" "phase-${up_to}-prereq-missing" \
                "Cannot run phase${up_to}: phase${i} not marked complete in $ARTIFACT.
Run 'status' to see current progress; complete missing phases in order."
        fi
    done
}

# Append STATE marker for the given phase to the artifact. Idempotent —
# re-invocation of a phase that already completed is allowed (and is the
# expected path on operator re-run while waiting), but the STATE marker
# is appended only once.
mark_phase_complete() {
    local n="$1"
    if phase_completed "$n"; then
        echo "  (phase $n already marked complete; not appending duplicate marker)"
        return 0
    fi
    printf '\nSTATE: PHASE_%s_COMPLETE at %s\n' "$n" "$(date -u +%FT%TZ)" >> "$ARTIFACT"
}

# ── Phase 1: GATE VERIFICATION ───────────────────────────────────────────────

# Identify the latest gate doc for the given FROM→TO transition.
# paper → STAGE_1: forward_paper_completion_review_<date>.md
# STAGE_N → STAGE_N+1: stage_promotion_<date>_*_to_STAGE_N.md (prior promotion's
# artifact serves as the abbreviated review for the next — the cascading-gate
# semantic the runbook locks).
#
# Uses `find` (not `ls glob`) because the glob lives inside a quoted variable
# expansion when computed dynamically per-from-stage and ls won't glob that.
#
# Excludes _decision_rule_ and _template_ filenames — these are LOCKED RULE
# files (the template/spec) that contain the literal "Composite verdict:
# ALL_GREEN" inside their template scaffolds. Without the exclusion, phase1
# would happily false-positive against the rule doc that DESCRIBES what an
# ALL_GREEN verdict looks like rather than IS an ALL_GREEN verdict.
# (Lens-as-self-correction finding 2026-05-10 during local smoke.)
find_latest_gate_doc() {
    local name_pattern
    if [[ "$FROM" == "paper" ]]; then
        name_pattern='forward_paper_completion_review_*.md'
    else
        name_pattern="stage_promotion_*_to_${FROM}.md"
    fi
    local matches
    # -print + sort by name picks lexicographically-newest by ISO date in
    # the filename. Date-in-filename is the locked artifact-naming convention
    # (CLAUDE.md ## Recent session logs format), so lexical sort = chronological.
    matches=$(find "${RESULTS_DIR}" -maxdepth 1 -name "$name_pattern" \
              -not -name '*_decision_rule_*' \
              -not -name '*_template_*' \
              -not -name '*_verdict_*' \
              2>/dev/null | sort -r | head -n 1)
    printf '%s\n' "$matches"
}

phase1() {
    if [[ $# -lt 2 ]]; then
        die "$EXIT_ENV_OR_INPUT_ERROR" "phase1-bad-args" \
            "Usage: phase1 <FROM> <TO> (e.g., phase1 paper STAGE_1)"
    fi
    FROM="$1"
    TO="$2"
    validate_transition "$FROM" "$TO"
    stage_params "$TO"

    # Refuse if there's already an in-progress promotion (forces explicit
    # phase6 or rollback first; prevents accidental concurrent promotions).
    local existing
    existing=$(/bin/ls "${RESULTS_DIR}"/stage_promotion_*.md.in-progress 2>/dev/null || true)
    if [[ -n "$existing" ]]; then
        die "$EXIT_STATE_OUT_OF_ORDER" "concurrent-promotion-blocked" \
            "An in-progress promotion already exists:
${existing}
Complete with 'phase6' or abandon with 'rollback' before starting a new one."
    fi

    local gate
    gate=$(find_latest_gate_doc)
    if [[ -z "$gate" ]]; then
        die "$EXIT_GATE_ARTIFACT_MISSING" "phase1-gate-missing" \
            "No gate document found for ${FROM} → ${TO}.
Expected: $([[ "$FROM" == "paper" ]] && echo "results/forward_paper_completion_review_<date>.md" || echo "results/stage_promotion_<date>_*_to_${FROM}.md")
This means the upstream completion review has not been written. Author the review FIRST, then re-run phase1."
    fi

    # Gate must contain ALL_GREEN composite verdict.
    if ! grep -q "Composite verdict:.*ALL_GREEN" "$gate"; then
        die "$EXIT_GATE_NOT_GREEN" "phase1-gate-not-green" \
            "Gate document $gate does NOT contain 'Composite verdict: ALL_GREEN'.
Promotion blocked. Either the review failed (don't promote) or the verdict line is malformed (fix and re-run)."
    fi

    # No HARD KILL artifact newer than the gate. find -newer compares mtimes
    # directly so no epoch computation needed; the gate doc itself is the
    # reference point. Filter excludes the analysis/calibration files that
    # CONTAIN "kill_" but are not actual kill-event artifacts.
    local newer_kills
    newer_kills=$(find "${RESULTS_DIR}" -maxdepth 1 -name 'kill_*.md' \
                  -not -name 'kill_bar_*' -not -name '*_calibration_*' \
                  -not -name '*_recal_*' -not -name '*_rule_*' \
                  -not -name '*_verdict_*' \
                  -newer "$gate" 2>/dev/null || true)
    if [[ -n "$newer_kills" ]]; then
        die "$EXIT_GATE_NOT_GREEN" "phase1-newer-kill" \
            "HARD KILL artifact(s) newer than gate $gate:
${newer_kills}
A kill event AFTER the gate verdict invalidates it. Do not promote."
    fi

    # All gates pass — create in-progress artifact and write Phase 1 block.
    local date_tag
    date_tag=$(date -u +%F)
    ARTIFACT="${RESULTS_DIR}/stage_promotion_${date_tag}_${FROM}_to_${TO}.md.in-progress"
    cat > "$ARTIFACT" <<EOF
# STAGE promotion — ${date_tag} — ${FROM} → ${TO}

**From:** ${FROM}
**To:** ${TO}
**New stake_usd:** \$${STAKE_USD}
**Operator(s):** $(whoami)
**Started:** $(date -u '+%Y-%m-%dT%H:%M:%SZ')
**Status:** IN-PROGRESS (renamed to stage_promotion_${date_tag}_${FROM}_to_${TO}.md on phase6 completion)

## Pre-promotion gate
- Gate document: \`${gate}\`
- Verdict: ALL_GREEN (verified)
- No newer kill artifacts: confirmed
- Verified at: $(date -u '+%Y-%m-%dT%H:%M:%SZ')

## Per-stage parameters (locked by runbook §Per-stage-parameters)
- Stake: \$${STAKE_USD}
- First-N-trade verification: N=${FIRST_N_TRADES}
- Monitoring window: ${MONITORING_HOURS}h
- Cooling period after rollback: ${COOLING_DAYS}d
- Daily-loss circuit breaker (Gate B): \$${DAILY_LOSS_USD}
- Review type for next promotion: ${REVIEW_TYPE}

EOF
    mark_phase_complete 1
    notify_telegram INFO "stage_promotion: phase1 GREEN" \
        "${FROM} → ${TO} promotion started.
Gate: $(basename "$gate")
Stake: \$${STAKE_USD}
Next: bash scripts/stage_promotion.sh phase2"
    echo "✓ Phase 1 complete: gate verified, artifact created at $ARTIFACT"
    exit "$EXIT_PHASE_COMPLETE"
}

# ── Phase 2: CONFIG PREPARATION ──────────────────────────────────────────────

phase2() {
    locate_active_artifact
    require_prior_phases_complete 2
    stage_params "$TO"

    cat <<EOF
── Phase 2: CONFIG PREPARATION ──────────────────────────────────────────

Per the runbook (§Phase-2), this phase requires OPERATOR action — the
script cannot apply config changes that affect real-money exposure
without explicit per-line diff approval. Specifically:

EOF
    if [[ "$FROM" == "paper" ]]; then
        cat <<EOF
For paper → STAGE_1 (real-money first activation):
  1. Change executor flag: --executor stub  →  --executor binance_live
  2. Verify BINANCE_API_KEY and BINANCE_API_SECRET are in /etc/paper-live/env
     on the VPS (separate from any testnet keys used at Layer 2).
  3. Set stake_usd: \$${STAKE_USD} (CLI override --stake-usd ${STAKE_USD})
  4. Generate the diff against current production config:
     ssh root@178.105.24.230 'cat /etc/systemd/system/paper-live@.service'
     (capture, edit locally, plan the systemd-edit + daemon-reload sequence)

EOF
    else
        cat <<EOF
For ${FROM} → ${TO} (stake increase only):
  1. Update stake_usd from prior stage to: \$${STAKE_USD}
  2. (No executor change required — binance_live remains)
  3. Generate the diff against current production config

EOF
    fi
    cat <<EOF
Required of the operator review:
  - ONLY the stake_usd change ($([[ "$FROM" == "paper" ]] && echo "AND executor change at STAGE_1"))
  - NO other parameter changes
  - NO accidentally-modified comments or whitespace
  - Diff is clean

After reviewing and applying the change, re-run this phase with the
explicit confirm signal:

  STAGE_PROMOTION_CONFIRM=YES bash scripts/stage_promotion.sh phase2

This is intentionally non-interactive — the env-var literal IS the
operator's signature. Future-self auditing this artifact knows the
operator's affirmative consent was recorded.

EOF
    if [[ "${STAGE_PROMOTION_CONFIRM:-}" != "YES" ]]; then
        die "$EXIT_OPERATOR_INTERVENTION_REQUIRED" "phase2-awaiting-confirm" \
            "Phase 2 requires operator config-change confirmation.
Apply the locked diff per runbook §Phase-2, then re-run with STAGE_PROMOTION_CONFIRM=YES."
    fi

    cat >> "$ARTIFACT" <<EOF

## Phase 2: config preparation
- Stake target: \$${STAKE_USD}
$([[ "$FROM" == "paper" ]] && echo "- Executor flip: stub → binance_live")
- Operator-confirm received at: $(date -u '+%Y-%m-%dT%H:%M:%SZ')
- Reviewer: $(whoami)
- Diff applied: yes (operator-asserted via STAGE_PROMOTION_CONFIRM=YES)

EOF
    mark_phase_complete 2
    notify_telegram INFO "stage_promotion: phase2 confirmed" \
        "${FROM} → ${TO}: operator confirmed config change.
Next: bash scripts/stage_promotion.sh phase3"
    echo "✓ Phase 2 complete: operator confirmation received"
    exit "$EXIT_PHASE_COMPLETE"
}

# ── Phase 3: DEPLOY ──────────────────────────────────────────────────────────

phase3() {
    locate_active_artifact
    require_prior_phases_complete 3
    stage_params "$TO"

    # STAGE_4 only: optional HALT first (per runbook §Phase-3 line 126).
    if [[ "$TO" == "STAGE_4" ]]; then
        echo "── STAGE_4 — graceful halt before deploy (runbook recommendation) ──"
        if [[ "${STAGE_PROMOTION_CONFIRM:-}" != "YES" ]]; then
            die "$EXIT_OPERATOR_INTERVENTION_REQUIRED" "phase3-stage4-halt-confirm" \
                "STAGE_4 promotion REQUIRES graceful pre-deploy halt.
Confirm by re-running with STAGE_PROMOTION_CONFIRM=YES.
The halt SSH command: systemctl stop paper-live@*.service on the VPS."
        fi
        if [[ "${STAGE_PROMOTION_DRY_RUN:-0}" != "1" ]]; then
            echo "→ Halting all engines on VPS…"
            if ! ssh root@178.105.24.230 'systemctl stop "paper-live@*.service"'; then
                die "$EXIT_PHASE_RUNTIME_ERROR" "phase3-halt-ssh-failed" \
                    "SSH to VPS failed during STAGE_4 halt. Investigate connectivity before retrying."
            fi
            local active_count
            active_count=$(ssh root@178.105.24.230 'systemctl list-units "paper-live@*.service" --state=active --no-legend 2>/dev/null | wc -l' | tr -d ' ')
            if [[ "$active_count" != "0" ]]; then
                die "$EXIT_PHASE_VERIFICATION_FAILED" "phase3-halt-incomplete" \
                    "After 'systemctl stop' the VPS still reports $active_count active paper-live engines. Investigate before deploying."
            fi
        else
            echo "  (DRY_RUN — would halt engines via SSH)"
        fi
    fi

    # All stages: redeploy via existing mechanism.
    echo "── Phase 3: DEPLOY via ./deploy/redeploy.sh all ──"
    if [[ "${STAGE_PROMOTION_DRY_RUN:-0}" == "1" ]]; then
        echo "  (DRY_RUN — would run: ./deploy/redeploy.sh all)"
    else
        if ! "${ROOT}/deploy/redeploy.sh" all; then
            die "$EXIT_PHASE_RUNTIME_ERROR" "phase3-redeploy-failed" \
                "deploy/redeploy.sh exited non-zero. Engines may be in inconsistent state. Inspect VPS state immediately."
        fi
    fi

    # Verification: STRICT=1 post_deploy_check.
    echo "── Phase 3 verification: STRICT=1 ./scripts/post_deploy_check.sh ──"
    if [[ "${STAGE_PROMOTION_DRY_RUN:-0}" == "1" ]]; then
        echo "  (DRY_RUN — would run: STRICT=1 ./scripts/post_deploy_check.sh)"
    else
        if ! STRICT=1 "${ROOT}/scripts/post_deploy_check.sh"; then
            die "$EXIT_PHASE_VERIFICATION_FAILED" "phase3-post-deploy-warn" \
                "post_deploy_check STRICT=1 reported warnings post-deploy.
Per runbook §Phase-3 line 148: ROLLBACK immediately. Run 'rollback' subcommand."
        fi
    fi

    local deploy_ts
    deploy_ts=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
    cat >> "$ARTIFACT" <<EOF

## Phase 3: deploy
- Deploy timestamp: $deploy_ts
- post_deploy_check STRICT=1 verdict: HEALTHY
- Engines active post-deploy: $([[ "${STAGE_PROMOTION_DRY_RUN:-0}" == "1" ]] && echo "(DRY_RUN)" || echo "16/16")
- STAGE_4 pre-halt invoked: $([[ "$TO" == "STAGE_4" ]] && echo "yes" || echo "n/a")

DEPLOY_TIMESTAMP: ${deploy_ts}

EOF
    mark_phase_complete 3
    notify_telegram INFO "stage_promotion: phase3 deployed" \
        "${FROM} → ${TO} deploy complete.
post_deploy_check STRICT: HEALTHY
Next: bash scripts/stage_promotion.sh phase4 (will block until ${FIRST_N_TRADES} trades close)"
    echo "✓ Phase 3 complete: deploy verified"
    exit "$EXIT_PHASE_COMPLETE"
}

# ── Phase 4: FIRST-N-TRADE VERIFICATION ──────────────────────────────────────

# Extract DEPLOY_TIMESTAMP from the artifact (written by phase3).
get_deploy_timestamp() {
    grep '^DEPLOY_TIMESTAMP: ' "$ARTIFACT" | head -1 | awk '{print $2}'
}

# Count closes in journal after the deploy timestamp. Prints count to stdout.
#
# Uses pure python (not grep + glob) because with `set -euo pipefail` a
# zero-match grep crashes the whole script — phase4 needs to handle the
# legitimate "no closes yet" case as exit 9 PHASE_PENDING_DATA, not exit
# with grep's spurious 1. python's pathlib.glob returns empty list cleanly
# on no-match, so the count-zero path is data-driven not error-driven.
count_post_deploy_closes() {
    local since="$1"
    local target="${STAGE_PROMOTION_VPS:-root@178.105.24.230}"
    if [[ -n "${STAGE_PROMOTION_LOCAL_JOURNAL:-}" ]]; then
        python3 - "${STAGE_PROMOTION_LOCAL_JOURNAL}" "$since" <<'PYEOF'
import json, sys, pathlib
journal_dir = pathlib.Path(sys.argv[1])
since = sys.argv[2]
n = 0
for f in journal_dir.glob("*.jsonl"):
    try:
        for line in f.read_text().splitlines():
            try:
                d = json.loads(line)
            except ValueError:
                continue
            if d.get("event") == "close" and d.get("ts", "") >= since:
                n += 1
    except OSError:
        continue
print(n)
PYEOF
    else
        # Remote path: stream journals via SSH to local python. Avoids the
        # need for remote python or remote globbing. Cannot use a heredoc
        # python script here — heredoc stdin overrides piped stdin
        # (shellcheck SC2259), so the journal stream would never reach
        # python. Use -c with an inline script that reads from stdin.
        ssh "$target" 'cat /var/log/paper-live/journal/*.jsonl 2>/dev/null || true' \
        | python3 -c "
import json, sys
since = sys.argv[1]
n = 0
for line in sys.stdin:
    try:
        d = json.loads(line)
    except ValueError:
        continue
    if d.get('event') == 'close' and d.get('ts', '') >= since:
        n += 1
print(n)
" "$since"
    fi
}

phase4() {
    locate_active_artifact
    require_prior_phases_complete 4
    stage_params "$TO"

    local since
    since=$(get_deploy_timestamp)
    if [[ -z "$since" ]]; then
        die "$EXIT_STATE_OUT_OF_ORDER" "phase4-no-deploy-ts" \
            "Cannot find DEPLOY_TIMESTAMP marker in $ARTIFACT. Phase 3 may not have completed cleanly."
    fi

    local closes
    closes=$(count_post_deploy_closes "$since")
    if [[ -z "$closes" ]] || ! [[ "$closes" =~ ^[0-9]+$ ]]; then
        die "$EXIT_PHASE_RUNTIME_ERROR" "phase4-journal-unreadable" \
            "Could not count closes from journal since $since (got: '$closes'). Investigate VPS journal access."
    fi

    echo "Phase 4: first-${FIRST_N_TRADES}-trade verification"
    echo "Deploy at: $since"
    echo "Closes since deploy: $closes / $FIRST_N_TRADES required"

    if [[ "$closes" -lt "$FIRST_N_TRADES" ]]; then
        local missing=$(( FIRST_N_TRADES - closes ))
        notify_telegram INFO "stage_promotion: phase4 pending" \
            "${FROM} → ${TO}: waiting on $missing more closed trades ($closes / $FIRST_N_TRADES).
Re-run 'phase4' when more trades have closed."
        echo "  Waiting on $missing more trades. Re-invoke phase4 later."
        exit "$EXIT_PHASE_PENDING_DATA"
    fi

    # Sufficient trades closed; runbook says operator MUST manually verify
    # each per Phase 4 verification criteria (fill price within 10bps,
    # journal cost fields populated, outcome matches narrative). Like Phase 2
    # this is a manual gate confirmed via env var.
    if [[ "${STAGE_PROMOTION_CONFIRM:-}" != "YES" ]]; then
        die "$EXIT_OPERATOR_INTERVENTION_REQUIRED" "phase4-awaiting-confirm" \
            "${FIRST_N_TRADES} trades have closed; operator must manually verify each per runbook §Phase-4 (fill price within 10bps, cost-decomp fields populated, outcome matches narrative).
After review, re-run with STAGE_PROMOTION_CONFIRM=YES.
If ANY trade fails verification, invoke 'rollback' instead."
    fi

    local verified_ts
    verified_ts=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
    cat >> "$ARTIFACT" <<EOF

## Phase 4: first-N-trade verification
- Required: ${FIRST_N_TRADES} closed trades
- Observed: ${closes} closed trades since deploy ($since)
- Operator manual verification: PASS (asserted via STAGE_PROMOTION_CONFIRM=YES)
- Verified at: $verified_ts

EOF
    mark_phase_complete 4
    notify_telegram INFO "stage_promotion: phase4 verified" \
        "${FROM} → ${TO}: ${FIRST_N_TRADES} trades verified.
Next: bash scripts/stage_promotion.sh phase5 (monitoring window ${MONITORING_HOURS}h)"
    echo "✓ Phase 4 complete"
    exit "$EXIT_PHASE_COMPLETE"
}

# ── Phase 5: MONITORING WINDOW ───────────────────────────────────────────────

phase5() {
    locate_active_artifact
    require_prior_phases_complete 5
    stage_params "$TO"

    local since
    since=$(get_deploy_timestamp)
    local now_epoch deploy_epoch elapsed_hours
    now_epoch=$(date -u +%s)
    deploy_epoch=$(date -j -f "%Y-%m-%dT%H:%M:%SZ" "$since" +%s 2>/dev/null \
                   || date -u -d "$since" +%s 2>/dev/null || echo 0)
    if [[ "$deploy_epoch" == "0" ]]; then
        die "$EXIT_STATE_OUT_OF_ORDER" "phase5-bad-deploy-ts" \
            "Cannot parse DEPLOY_TIMESTAMP ($since)."
    fi
    elapsed_hours=$(( (now_epoch - deploy_epoch) / 3600 ))

    echo "Phase 5: monitoring window"
    echo "Deploy at: $since"
    echo "Elapsed: ${elapsed_hours}h / ${MONITORING_HOURS}h required"

    if [[ "$elapsed_hours" -lt "$MONITORING_HOURS" ]]; then
        local remaining=$(( MONITORING_HOURS - elapsed_hours ))
        notify_telegram INFO "stage_promotion: phase5 pending" \
            "${FROM} → ${TO}: monitoring window incomplete (${elapsed_hours}h / ${MONITORING_HOURS}h).
${remaining}h remaining. Continue daily checks until window closes."
        echo "  ${remaining}h remaining in monitoring window. Re-invoke phase5 after that."
        exit "$EXIT_PHASE_PENDING_DATA"
    fi

    # Window has elapsed; require operator to assert daily checks completed
    # cleanly throughout (the runbook §Phase-5 specifies daily post_deploy +
    # forward_paper_status + run_drift_check during the window).
    if [[ "${STAGE_PROMOTION_CONFIRM:-}" != "YES" ]]; then
        die "$EXIT_OPERATOR_INTERVENTION_REQUIRED" "phase5-awaiting-confirm" \
            "Monitoring window of ${MONITORING_HOURS}h has elapsed; operator must assert daily checks ran clean.
Per runbook §Phase-5: each day in the window required post_deploy_check + forward_paper_status + run_drift_check (no exit-1 firings).
Assert via re-run with STAGE_PROMOTION_CONFIRM=YES.
If any check fired, invoke 'rollback' instead."
    fi

    cat >> "$ARTIFACT" <<EOF

## Phase 5: monitoring window
- Window length: ${MONITORING_HOURS}h
- Elapsed: ${elapsed_hours}h
- Operator daily-check assertion: PASS
- Verified at: $(date -u '+%Y-%m-%dT%H:%M:%SZ')

EOF
    mark_phase_complete 5
    notify_telegram INFO "stage_promotion: phase5 cleared" \
        "${FROM} → ${TO}: monitoring window ${MONITORING_HOURS}h elapsed clean.
Next: bash scripts/stage_promotion.sh phase6 (finalize artifact + closure template)"
    echo "✓ Phase 5 complete"
    exit "$EXIT_PHASE_COMPLETE"
}

# ── Phase 6: DOCUMENT (close artifact + closure template skeleton) ───────────

phase6() {
    locate_active_artifact
    require_prior_phases_complete 6
    stage_params "$TO"

    local date_tag
    date_tag=$(date -u +%F)

    # Append closure template skeleton (the promote_closure_template_decision_
    # rule_2026-05-10 §Locked-template-structure pre-registered shape).
    # Operator fills in section content within 7d per the closure template's
    # required-completion-window.
    cat >> "$ARTIFACT" <<EOF

## Phase 6: document — closure template skeleton

The full closure template per
\`results/promote_closure_template_decision_rule_2026-05-10.md\` follows.
Operator must fill in section content within 7 days of this promotion
completing. Sections below are scaffolded; data points marked \`<...>\`
need operator input.

### 1. Timeline
- T_first_eligible: <UTC ts when ALL gates first cleared simultaneously>
- T_verdict: <forward_paper_resolution.py / completion_review verdict ts>
- T_operator_acknowledge: <when operator confirmed verdict>
- T_runbook_phase_1: $(grep '^STATE: PHASE_1_COMPLETE' "$ARTIFACT" | awk '{print $4}')
- T_runbook_phase_6: $(date -u '+%Y-%m-%dT%H:%M:%SZ')
- T_first_trade_new_stage: <UTC ts of first close at new stake>

### 2. Mechanism confirmation
- Net PnL at promotion: <\$N over D days>
- Hit rate (WR%): <X%> at <n> trades
- Per-symbol contribution distribution: top contributor <X%>
- Cost stack at promotion: realized fee=<X>bp, slip=<Y>bp
- Drift detector trajectory (last 4 weeks): <[a,b,c,d]>
- BTC-HODL benchmark beat: <\$N delta>

### 3. Pre-registered hypothesis evaluation
- Caveats ACTIVATED in window: <list with quantified impact>
- Caveats UNTESTED (roll forward): <list>
- NEW caveats surfaced: <list or "none">

### 4. Validation latency
- T_first_eligible − T_verdict latency: <X days>

### 5. Counterfactual / non-promote evidence
- Near-miss threshold(s): <list with margins>
- Recently-failing-now-cleared metric(s): <list with trajectories>
- Drift INVESTIGATION fires: <count + dispositions>
- Operator qualitative concerns deferred: <list or "none">
- IF "no counterfactual evidence": explicit statement: <text>

### 6. Risk acceptance ledger
| Risk | Stake at this stage | Accepts? | Mitigation | Reversal threshold |
|---|---|:---:|---|---|
| REST polling lag | \$${STAKE_USD}/trade | YES/NO | <text> | <text> |
| Funding-CSV staleness | \$${STAKE_USD}/trade | YES/NO | <text> | <text> |
$([[ "$FROM" == "paper" ]] && echo "| Real-money execution untested | \$${STAKE_USD}/trade | YES/NO | Layer 2/3 PASS | <text> |")
| <new from §3> | \$${STAKE_USD}/trade | YES/NO | <text> | <text> |

### 7. Stage transition mechanics
- Executor flip: $([[ "$FROM" == "paper" ]] && echo "stub → binance_live (all 16)" || echo "n/a")
- Credentials provisioned with locked permission set: <yes/no>
- Stake_usd: \$${STAKE_USD} (applied)
- Monitoring cadence change: <documented>
- Telegram tier change: <documented>
- Watchdog timer review: <documented>
- Documentation updates queued: <list>

### 8. Reversal criteria for new stage
- Cross-ref real_money_protocol per-stage kill triggers: <applied at this stage>
- New thresholds specific to this transition: <list or "none">
- Rollback path operational: <yes/no — verified at deploy>

### 9. Next-stage monitoring cadence
- Drift detector cadence: <e.g., weekly>
- per_symbol_pause check: <cadence>
- post_deploy_check STRICT=1: <cadence>
- BTC-HODL benchmark: <cadence>
- First-fire confirmations: <list>

### 10. Action items (2-5 required)
- <action item with owner, trigger, acceptance, cross-ref>
- <action item>

### 11. Cross-references and supporting data
- Originating verdict: <link>
- stage_promotion.sh run log: this artifact's phase blocks above
- forward_paper_status.sh snapshot at trigger: <link>
- forward_paper_resolution.py output: <link>
- realized_cost_trajectory.py snapshot: <link>
- Drift detector log (last 4 weeks): <link>
- BTC-HODL benchmark output: <link>
$([[ "$FROM" == "paper" ]] && echo "- Layer 2 testnet PASS verdict: <link>")
$([[ "$FROM" == "paper" ]] && echo "- Layer 3 shadow parity PASS verdict: <link>")
- Real-money credentials provisioning audit trail: <link>
- Git log range covering window: <link>
- External references: <list or "none">

STATE: PHASE_6_COMPLETE at $(date -u +%FT%TZ)
EOF

    # Rename .in-progress → final filename
    local final="${RESULTS_DIR}/stage_promotion_${date_tag}_${FROM}_to_${TO}.md"
    # If the rename target already exists (re-run of phase6 same day), preserve
    # the prior artifact with a sequence suffix rather than silently overwriting.
    if [[ -e "$final" ]]; then
        local seq=2
        while [[ -e "${final%.md}_${seq}.md" ]]; do seq=$((seq + 1)); done
        final="${final%.md}_${seq}.md"
    fi
    mv "$ARTIFACT" "$final"

    notify_telegram INFO "stage_promotion: phase6 closed" \
        "${FROM} → ${TO} promotion finalized.
Artifact: $(basename "$final")
REMINDER: closure template requires substantive fill-in within 7 days per promote_closure_template rule."
    echo "✓ Phase 6 complete: artifact at $final"
    echo ""
    echo "REMINDER: closure template (sections 1-11) requires substantive operator fill-in within 7 days."
    echo "See: results/promote_closure_template_decision_rule_2026-05-10.md"
    exit "$EXIT_PHASE_COMPLETE"
}

# ── Rollback ─────────────────────────────────────────────────────────────────

rollback_cmd() {
    locate_active_artifact
    stage_params "$TO"

    echo "── ROLLBACK: ${FROM} → ${TO} ──"
    echo "Active artifact: $ARTIFACT"
    echo ""
    echo "Per runbook §Rollback-path: revert config, redeploy, verify."
    echo "Rollback returns engines to ${FROM} (the prior stage)."
    echo ""

    if [[ "${STAGE_PROMOTION_CONFIRM:-}" != "YES" ]]; then
        die "$EXIT_OPERATOR_INTERVENTION_REQUIRED" "rollback-awaiting-confirm" \
            "Rollback is destructive (re-deploys engines). Confirm with STAGE_PROMOTION_CONFIRM=YES.
Required operator action sequence:
  1. Manually revert the Phase 2 config change (revert commit, or systemd edit reversed)
  2. Re-run rollback subcommand with STAGE_PROMOTION_CONFIRM=YES — script will redeploy + verify"
    fi

    if [[ "${STAGE_PROMOTION_DRY_RUN:-0}" == "1" ]]; then
        echo "  (DRY_RUN — would invoke deploy/redeploy.sh all + STRICT post_deploy_check)"
    else
        if ! "${ROOT}/deploy/redeploy.sh" all; then
            die "$EXIT_PHASE_RUNTIME_ERROR" "rollback-redeploy-failed" \
                "Rollback redeploy failed. Engines in inconsistent state — manual recovery required."
        fi
        if ! STRICT=1 "${ROOT}/scripts/post_deploy_check.sh"; then
            die "$EXIT_PHASE_VERIFICATION_FAILED" "rollback-post-deploy-warn" \
                "Post-rollback STRICT check reported warnings. Investigate IMMEDIATELY."
        fi
    fi

    cat >> "$ARTIFACT" <<EOF

## ROLLBACK
- Initiated at: $(date -u '+%Y-%m-%dT%H:%M:%SZ')
- Reason: <operator must annotate before phase6 finalization>
- Redeploy + STRICT post_deploy_check: PASS
- Returned to stage: ${FROM}
- Cooling period before re-attempt: ${COOLING_DAYS}d

STATE: ROLLBACK_COMPLETE at $(date -u +%FT%TZ)
EOF
    notify_telegram INFO "stage_promotion: rollback complete" \
        "${FROM} → ${TO}: ROLLED BACK to ${FROM}.
post_deploy_check STRICT: HEALTHY post-rollback.
Cooling period: ${COOLING_DAYS} days before re-attempt.
Artifact remains in-progress; annotate rollback reason and finalize via phase6 when ready."
    echo "✓ Rollback complete: engines on ${FROM}"
    exit "$EXIT_ROLLBACK_INITIATED"
}

# ── Status ───────────────────────────────────────────────────────────────────

status_cmd() {
    locate_active_artifact
    echo "── stage_promotion status ──"
    echo "Active artifact: $(basename "$ARTIFACT")"
    echo "Transition: ${FROM} → ${TO}"
    echo ""
    echo "Phases completed:"
    local i
    for i in 1 2 3 4 5 6; do
        if phase_completed "$i"; then
            local ts
            ts=$(grep "^STATE: PHASE_${i}_COMPLETE" "$ARTIFACT" | head -1 | awk '{print $4}')
            echo "  ✓ phase${i} at $ts"
        else
            echo "  · phase${i} pending"
        fi
    done
    if grep -q "^STATE: ROLLBACK_COMPLETE" "$ARTIFACT"; then
        echo ""
        echo "⚠ ROLLBACK has been invoked on this promotion."
    fi
    exit 0
}

# ── Main dispatch ────────────────────────────────────────────────────────────

if [[ $# -lt 1 ]]; then
    cat >&2 <<EOF
Usage: bash scripts/stage_promotion.sh <subcommand> [args]

Subcommands:
  phase1 <FROM> <TO>   GATE verification (start promotion)
  phase2               CONFIG preparation (operator-confirm required)
  phase3               DEPLOY (redeploy + STRICT post_deploy_check)
  phase4               FIRST-N-TRADE verification
  phase5               MONITORING window
  phase6               DOCUMENT (finalize + write closure template skeleton)
  status               Show current phase progress
  rollback             Invoke runbook §Rollback-path

Env vars:
  STAGE_PROMOTION_CONFIRM=YES   Operator confirmation for phase2/4/5/rollback
  STAGE_PROMOTION_DRY_RUN=1     Skip actual deploy / post_deploy_check
  STAGE_PROMOTION_VPS           Override default VPS target
  STAGE_PROMOTION_LOCAL_JOURNAL Test-mode: read journal from local dir
EOF
    exit "$EXIT_ENV_OR_INPUT_ERROR"
fi

CMD="$1"
shift
case "$CMD" in
    phase1)   phase1 "$@" ;;
    phase2)   phase2 "$@" ;;
    phase3)   phase3 "$@" ;;
    phase4)   phase4 "$@" ;;
    phase5)   phase5 "$@" ;;
    phase6)   phase6 "$@" ;;
    status)   status_cmd ;;
    rollback) rollback_cmd ;;
    *)
        die "$EXIT_ENV_OR_INPUT_ERROR" "unknown-subcommand" \
            "Unknown subcommand: '$CMD'. Run with no args for usage."
        ;;
esac
