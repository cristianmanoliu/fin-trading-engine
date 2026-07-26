#!/usr/bin/env bash
# weekly_audit.sh — operational wrapper combining the two artifacts the
# Sunday-09:00 launchd job produces:
#
#   1. run_drift_check.sh — decision-grade kill signal (exit 0/1/2/3/4/5)
#   2. forward_paper_status.sh — operational snapshot, dated under
#      results/forward_paper_snapshots/<YYYY-MM-DD>.txt for longitudinal
#      diffing
# Additional stages 3-8 layer in journal validation, kill_protocol_check,
# stage_promotion_check, forward_paper_resolution (LIMBO rule synthesis),
# lag_summary (REST-poll-lag aggregator added 2026-05-11), and
# promotion_rehearsal (end-to-end STAGE_1 readiness composer added 2026-05-11 PM).
#
# Drift check exits with the decision-grade severity; this wrapper
# preserves that as its OWN exit code so launchd's last-exit-code column
# (visible via `launchctl list`) reflects the kill signal, not snapshot
# success.
#
# Snapshot failure is non-fatal — a transient SSH or jq error at snapshot
# time should not mask a real drift verdict. The drift check ran first
# anyway, so its signal is already persisted regardless of what happens here.
#
# History file: this wrapper writes results/drift_check_history_CRON.jsonl,
# NOT the decision-grade results/drift_check_history.jsonl that
# kill_protocol_check criterion #1 evaluates. Only a deliberate operator run of
# run_drift_check.sh writes the latter. Override with DRIFT_CHECK_HISTORY if you
# genuinely need the cron to write elsewhere. See the stage-1 comment below.
#
# Usage:
#   ./scripts/weekly_audit.sh              # default — invoked by launchd
#   ./scripts/weekly_audit.sh --no-snapshot  # only run drift check
#
# Composable with deploy/drift-check.launchd.plist; the plist's
# ProgramArguments points at this wrapper instead of run_drift_check.sh
# directly.
set -uo pipefail

# ─────────────────────────────────────────────────────────────────────
# Internal classifiers (extracted for testability — see scripts/test_weekly_audit.sh)
# ─────────────────────────────────────────────────────────────────────

# Map a journal_validate-via-ssh exit code to a Telegram routing tier.
# Distinguishes ssh-level failure (connection refused, auth, signal, command
# not found on remote — all conventionally non-zero ssh exits) from
# journal_validate's documented contract (0=clean / 1=warn / 2=error /
# 3=usage). Without this distinction a network blip routes to CRITICAL
# "journal_validate found errors" — wrong tier, wrong text. Telegram-tier
# dual sense pathology, same shape as f87042e + ed1f360.
_classify_validate_exit() {
    case "$1" in
        0|1)                 echo "OK" ;;
        2|3)                 echo "CORRUPTION" ;;
        126|127|130|137|255) echo "SSH_FAILURE" ;;
        *)                   echo "UNEXPECTED" ;;
    esac
}

# Map a python-helper exit code to a Telegram routing tier. The scripts
# (kill_protocol_check, stage_promotion_check, forward_paper_resolution)
# have varying documented contracts — the ALERT_MODES arg encodes which
# exit codes correspond to which alert tier. Codes outside the contract
# fall to UNEXPECTED so a python crash never silently maps to "no alert"
# (the F3-F5 fail-open pattern).
#
# ALERT_MODES syntax: "code:tier,code:tier,..." e.g. "1:CRITICAL,3:WARN,4:WARN".
# Codes not listed AND not in CONTINUE_CODES route to UNEXPECTED.
# CONTINUE_CODES is a comma-separated allowlist of "no-alert" exits
# (e.g. 0=CONTINUE, 2=WAITING).
_classify_python_exit() {
    local exit_code="$1" alert_modes="$2" continue_codes="$3"
    # Fast-path: explicit no-alert allowlist.
    local cc
    IFS=',' read -ra cc_arr <<< "$continue_codes"
    for cc in "${cc_arr[@]}"; do
        [[ "$exit_code" == "$cc" ]] && echo "CONTINUE" && return 0
    done
    # Walk the alert-mode pairs.
    local pair code tier
    IFS=',' read -ra pair_arr <<< "$alert_modes"
    for pair in "${pair_arr[@]}"; do
        code="${pair%%:*}"
        tier="${pair##*:}"
        if [[ "$exit_code" == "$code" ]]; then
            echo "$tier"
            return 0
        fi
    done
    echo "UNEXPECTED"
}

# Classify Layer 3 reconcile liveness from a 24h `positionRisk 418` ban count.
# The Layer 3 testnet reconciler (pkg/execution/binance_live.go) polls testnet
# positionRisk; a shared-IP rate-limit can trigger a Binance IP ban that — pre
# the 2026-05-29 backoff fix — the reconciler held open for 10 days, silently
# starving the STAGE_1 Layer 3 parity gate. This stage makes a recurring ban
# visible even with no redeploy.
#   count <  threshold (10) → OK          (occasional 418 absorbed by backoff)
#   count >= threshold      → DEGRADED    (ban active despite backoff — alert)
#   count == -1 (sentinel)  → SSH_FAILURE (ssh/grep could not run; do NOT
#                                          misreport a network blip as healthy —
#                                          same ssh-failure-vs-data-failure
#                                          discipline as _classify_validate_exit)
#   non-numeric / empty     → UNEXPECTED  (fail-closed; never silently OK)
_classify_layer3_health() {
    local count="$1"
    local threshold="${LAYER3_BAN_THRESHOLD:-10}"
    if [[ "$count" == "-1" ]]; then
        echo "SSH_FAILURE"
        return 0
    fi
    # Reject anything that isn't a non-negative integer.
    if ! [[ "$count" =~ ^[0-9]+$ ]]; then
        echo "UNEXPECTED"
        return 0
    fi
    if (( count >= threshold )); then
        echo "DEGRADED"
    else
        echo "OK"
    fi
}

# Test entry-point: when this file is `source`d (instead of executed
# directly), stop here so consumers get only the function definitions
# without triggering the main orchestration flow.
if [[ "${BASH_SOURCE[0]}" != "${0}" ]]; then
    return 0
fi

# ─────────────────────────────────────────────────────────────────────
# Main orchestration flow
# ─────────────────────────────────────────────────────────────────────

WITH_SNAPSHOT=1
while [[ $# -gt 0 ]]; do
    case "$1" in
        --no-snapshot) WITH_SNAPSHOT=0; shift ;;
        --help|-h) sed -n '2,28p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
        *) echo "unknown arg: $1" >&2; exit 1 ;;
    esac
done

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT" || { echo "weekly_audit: cd to $REPO_ROOT failed" >&2; exit 1; }

# --- 1. Drift check (decision-grade signal) ---
# History file is CRON-SCOPED, not the operator's decision-grade history.
#
# WHY (2026-07-26): run_drift_check.sh appends a row to its history file and
# then evaluates the locked "two firings >= 7 days apart" rule across the whole
# file. When this weekly cron shares one file with operator-initiated runs, an
# unattended schedule silently accumulates rows that feed a KILL criterion —
# kill_protocol_check #1 reads exactly that file. The manual cross-check
# protocol therefore forbids running this wrapper by hand
# (results/drift_firings/2026-06-07-crosscheck9-disposition.md).
#
# Splitting the files keeps both instruments honest: the cron still detects and
# alerts on drift every Sunday (its exit code is preserved below and drives the
# Telegram tiers), but the decision-grade history that criterion #1 evaluates is
# only ever written by a deliberate operator run.
#
# NOTE this does NOT retroactively change the 2026-07-26 KILL verdict. Auditing
# the existing history by timestamp shows the 05-27 <-> 06-07 firing pair
# includes MANUAL runs and predates the prohibition text (first committed
# 2026-07-25), so criterion #1 fired on real signal. See
# docs/findings/2026-07-26-forward-paper-KILL.md.
DRIFT_HISTORY_CRON="${DRIFT_CHECK_HISTORY:-${REPO_ROOT}/results/drift_check_history_cron.jsonl}"
DRIFT_CHECK_HISTORY="$DRIFT_HISTORY_CRON" \
    "${REPO_ROOT}/scripts/run_drift_check.sh" --quiet
DRIFT_EXIT=$?

# --- 2. Forward-paper snapshot (operational telemetry) ---
if [[ "$WITH_SNAPSHOT" -eq 1 ]]; then
    SNAPSHOT_DIR="${REPO_ROOT}/results/forward_paper_snapshots"
    mkdir -p "$SNAPSHOT_DIR"
    SNAPSHOT_FILE="${SNAPSHOT_DIR}/$(date -u +%Y-%m-%d).txt"
    if "${REPO_ROOT}/scripts/forward_paper_status.sh" > "$SNAPSHOT_FILE" 2>&1; then
        echo "snapshot: $SNAPSHOT_FILE"
    else
        echo "snapshot FAILED (non-fatal — drift signal preserved): see $SNAPSHOT_FILE" >&2
    fi
fi

# --- 3. Journal self-consistency validation ---
# Catches duplicate opens / out-of-order timestamps / mid-file malformed
# JSON in the live journals. The drift detector + forward_paper_status
# both READ these journals to produce decision-grade signals — silent
# corruption would poison both. Excludes the frozen pre-Bug-6 archive
# whose pre-fix issues are known and immutable. Runs on VPS via ssh
# (binary is at /opt/trading-engine/bin/journal_validate post-sync).
# shellcheck source=lib/notify.sh
source "${REPO_ROOT}/scripts/lib/notify.sh"

# NOTE: do not append `|| true` to the command substitution. With `|| true`
# inside $(...), the substitution's exit code is always 0, so $? after the
# assignment reads 0 regardless of whether journal_validate found errors —
# silently disarming the CRITICAL Telegram alert below. set -e is NOT active
# in this script (only -uo pipefail), so a non-zero exit here does NOT abort.
#
# BatchMode=yes + ConnectTimeout=10: fail fast on dead/missing connections
# instead of hanging cron on a password prompt. Combined with the
# _classify_validate_exit helper below, ssh-level failure (255 etc.) is
# routed to a SSH_FAILURE WARN tier rather than the CRITICAL "found errors"
# misclassification a transient network blip would otherwise produce.
# WEEKLY_AUDIT_VPS env override: previously the script hardcoded the VPS
# target for the journal_validate ssh + diagnostic messages. Test
# scenarios + alt-VPS-pointing all required editing the file. T9 ssh-
# target-consistency sweep 2026-05-11 PM aligns this with the other ssh-
# using scripts that already accept positional/env overrides.
WEEKLY_AUDIT_VPS="${WEEKLY_AUDIT_VPS:-root@178.105.24.230}"
VALIDATE_OUTPUT=$(ssh -o BatchMode=yes -o ConnectTimeout=10 "$WEEKLY_AUDIT_VPS" \
    '/opt/trading-engine/bin/journal_validate --dir /var/log/paper-live/journal --exclude archive 2>&1')
VALIDATE_EXIT=$?
VALIDATE_CLASS=$(_classify_validate_exit "$VALIDATE_EXIT")
echo "validate: exit=$VALIDATE_EXIT class=$VALIDATE_CLASS"
echo "$VALIDATE_OUTPUT" | tail -1
case "$VALIDATE_CLASS" in
    OK)
        # 0=clean / 1=warn — non-blocking, logged but not alerted.
        if [[ "$VALIDATE_EXIT" -eq 1 ]]; then
            echo "validate: warnings present (non-blocking)"
        fi
        ;;
    CORRUPTION)
        # journal_validate ERROR/USAGE — corruption detected. CRITICAL —
        # operator must investigate before next forward-paper analysis.
        notify_telegram CRITICAL "weekly_audit on $(hostname)" \
"journal_validate found errors in live journals
exit=$VALIDATE_EXIT
last line: $(echo "$VALIDATE_OUTPUT" | tail -1)
Run: ssh $WEEKLY_AUDIT_VPS /opt/trading-engine/bin/journal_validate --dir /var/log/paper-live/journal --exclude archive"
        ;;
    SSH_FAILURE)
        # ssh-level failure (connection refused / auth / signal / cmd-not-found).
        # NOT a corruption finding — operational/transient. WARN tier with
        # explicit "ssh issue" text so the operator doesn't waste time
        # investigating non-existent journal corruption at 3am.
        notify_telegram WARN "weekly_audit on $(hostname)" \
"ssh/remote-command failure during journal_validate (NOT a corruption finding)
ssh exit=$VALIDATE_EXIT
output: $(echo "$VALIDATE_OUTPUT" | tail -3)
Investigate VPS connectivity, then re-run manually.
Run: ssh $WEEKLY_AUDIT_VPS /opt/trading-engine/bin/journal_validate --dir /var/log/paper-live/journal --exclude archive"
        ;;
    UNEXPECTED|*)
        # Unexpected exit — fall-open guard. Don't silently swallow.
        notify_telegram WARN "weekly_audit on $(hostname)" \
"journal_validate UNEXPECTED EXIT (outside documented contract 0-3)
exit=$VALIDATE_EXIT
output: $(echo "$VALIDATE_OUTPUT" | tail -3)"
        ;;
esac

# --- 4. Kill-protocol check (decision-grade STOP signal) ---
# The kill_protocol_check.py mechanizes 4 of 6 locked kill criteria from
# real_money_protocol_decision_rule_2026-05-08.md §Kill criteria. Exit 1
# = at least one criterion fires; operator MUST stop the protocol.
# Cadence-aligned with drift detector (weekly) — kill criteria use the
# same drift_check_history that the drift cron writes.
KILL_OUTPUT=$(python3 "${REPO_ROOT}/scripts/kill_protocol_check.py" 2>&1)
KILL_EXIT=$?
KILL_CLASS=$(_classify_python_exit "$KILL_EXIT" "1:KILL_FIRES,3:ERROR,4:OPERATOR_VERIFY" "0,2")
echo "kill_check: exit=$KILL_EXIT class=$KILL_CLASS"
case "$KILL_CLASS" in
    CONTINUE)
        # 0 CONTINUE / 2 WAITING — documented status quo, no alert.
        :
        ;;
    KILL_FIRES)
        # KILL fires: highest-stakes alert. CRITICAL bypasses rate limit
        # and mute hours. Operator must act per pre-reg §"When kill fires".
        notify_telegram CRITICAL "weekly_audit KILL on $(hostname)" \
"kill_protocol_check returned exit 1 — at least one locked kill criterion fires.
Stop the protocol per real_money_protocol_decision_rule_2026-05-08.md §'When kill fires':
  1. Cease new positions (disable signal generation)
  2. Let existing positions close at stops/targets (do NOT panic-close)
  3. Mark milestone KILLED
  4. No automatic resumption — fresh next-milestone pre-reg required

Run: python3 scripts/kill_protocol_check.py
Full output snapshot: results/decision_snapshots/$(date -u +%Y-%m-%d)-kill.txt"
        ;;
    OPERATOR_VERIFY)
        # OPERATOR-VERIFY: mechanical pass + #3 stake-assumption or
        # #5 unrecoverable-error require operator review. WARN tier.
        notify_telegram WARN "weekly_audit kill: operator-verify needed" \
"kill_protocol_check returned exit 4 — mechanical criteria pass but ≥1 deferred
criterion requires operator confirmation (e.g., paper-money stake interpretation,
unrecoverable-error log scan). Default action: CONTINUE if those check out.

Run: python3 scripts/kill_protocol_check.py"
        ;;
    ERROR)
        # ERROR: input/env failure. WARN — operator should investigate.
        notify_telegram WARN "weekly_audit kill: ERROR" \
"kill_protocol_check returned exit 3 (input/env failure). Operator should
investigate the cron environment.
$(echo "$KILL_OUTPUT" | tail -3)"
        ;;
    UNEXPECTED|*)
        # Crash, OOM, env corruption — outside the documented contract.
        # Without this branch the previous `*) :` swallowed any unexpected
        # exit, leaving the operator with zero notification of a broken
        # decision-grade gate. Same shape as F3 of the script-layer audit.
        notify_telegram WARN "weekly_audit kill: UNEXPECTED EXIT" \
"kill_protocol_check returned exit $KILL_EXIT (outside documented contract 0-4).
Likely script crash, OOM, or env failure. Investigate before next cron firing.
$(echo "$KILL_OUTPUT" | tail -3)"
        ;;
esac

# --- 5. Stage promotion check (decision-grade GO signal) ---
# stage_promotion_check.py mechanizes 8 of 9 locked STAGE_0→STAGE_1 gates.
# Exit 0 = ALL gates pass; operator may flip --executor=binance_live (with
# the BTC-HODL deferred check manually verified). This is a high-stakes
# transition — first time the system says "ready to go real-money" the
# operator should know within hours of the data crossing the line.
PROMOTE_OUTPUT=$(python3 "${REPO_ROOT}/scripts/stage_promotion_check.py" 2>&1)
PROMOTE_EXIT=$?
PROMOTE_CLASS=$(_classify_python_exit "$PROMOTE_EXIT" "0:PROMOTE_READY,1:BLOCKED,3:ERROR,4:CANDIDATE" "2")
echo "promotion_check: exit=$PROMOTE_EXIT class=$PROMOTE_CLASS"
case "$PROMOTE_CLASS" in
    CONTINUE)
        # 2 WAITING — status quo while data accumulates.
        :
        ;;
    PROMOTE_READY)
        # PROMOTE: all locked gates mechanically pass. Rare event;
        # CRITICAL tier so the operator does not miss the moment
        # forward-paper crosses the promotion line. Weekly cadence means
        # worst-case 7d delay between data-crosses and operator-knowing —
        # acceptable for a "first time ready" signal that took 4+ months.
        notify_telegram CRITICAL "weekly_audit PROMOTION READY on $(hostname)" \
"stage_promotion_check returned exit 0 — all evaluable gates pass. Forward-paper
has reached STAGE_0 → STAGE_1 readiness.

Pre-promotion checklist:
  1. Confirm Layer 2 testnet smoke completed cleanly (--executor=binance_live_testnet)
  2. Confirm Layer 3 7-day shadow parity completed (cmd/journal_diff)
  3. Generate fresh BINANCE_API_KEY/SECRET (separate from testnet creds)
  4. Flip the engine: --executor=binance_live, stake=\$100 per pre-reg §STAGE_1

Run: python3 scripts/stage_promotion_check.py
Snapshot: results/decision_snapshots/$(date -u +%Y-%m-%d)-promote.txt"
        ;;
    CANDIDATE)
        # PROMOTE-CANDIDATE: all mechanizable gates pass but ≥1 deferred
        # (e.g. transient Binance API failure in the BTC-HODL helper).
        # WARN tier because the operator should know but should NOT be
        # paged at CRITICAL — the deferred gate may simply be a network
        # blip that resolves itself before next week's run.
        notify_telegram WARN "weekly_audit promotion: candidate (deferred gates)" \
"stage_promotion_check returned exit 4 — all mechanizable gates pass but ≥1
DEFERRED gate (typically the BTC-HODL helper hit a transient Binance API
issue). NOT yet a deploy signal — operator must manually verify the deferred
gate(s) before treating this as PROMOTE-READY.

Run: python3 scripts/stage_promotion_check.py
$(echo "$PROMOTE_OUTPUT" | grep -E '\[DEFERRED\]' | head -3)"
        ;;
    BLOCKED)
        # BLOCKED: a gate fails outright after sufficient data. Could
        # repeat weekly while data accumulates and a criterion stays
        # failing — accept the noise, operator should know.
        notify_telegram WARN "weekly_audit promotion BLOCKED" \
"stage_promotion_check returned exit 1 — at least one locked promotion gate
fails outright at current data volume. Forward-paper is not on track for
STAGE_1 promotion under current conditions.

Run: python3 scripts/stage_promotion_check.py"
        ;;
    ERROR)
        notify_telegram WARN "weekly_audit promotion: ERROR" \
"stage_promotion_check returned exit 3 (input/env failure).
$(echo "$PROMOTE_OUTPUT" | tail -3)"
        ;;
    UNEXPECTED|*)
        # Crash / OOM / env corruption — outside documented contract 0-4.
        notify_telegram WARN "weekly_audit promotion: UNEXPECTED EXIT" \
"stage_promotion_check returned exit $PROMOTE_EXIT (outside documented contract 0-4).
Likely script crash, OOM, or env failure. Investigate before next cron firing.
$(echo "$PROMOTE_OUTPUT" | tail -3)"
        ;;
esac

# --- 6. Forward-paper resolution (LIMBO synthesis) ---
# forward_paper_resolution.py implements the locked LIMBO decision rule
# (forward_paper_outcome_resolution_decision_rule_2026-05-10.md). It
# synthesizes drift state + kill_protocol exit + stage_promotion exit +
# the latest snapshot's cohort metrics into a single 5-verdict outcome
# (CONTINUE / WATCH / PROMOTE / KILL / OPERATOR_REVIEW). Telegram tier
# is mapped per the locked rule. Pass the captured exit codes from
# steps 4 + 5 so the resolution script doesn't re-invoke the siblings.
RESOLUTION_OUTPUT=$(python3 "${REPO_ROOT}/scripts/forward_paper_resolution.py" \
    --kill-exit "$KILL_EXIT" --promote-exit "$PROMOTE_EXIT" 2>&1)
RESOLUTION_EXIT=$?
RESOLUTION_CLASS=$(_classify_python_exit "$RESOLUTION_EXIT" \
    "1:PROMOTE,2:WATCH,3:OPERATOR_REVIEW,4:KILL,5:INPUT_ERROR" "0")
echo "resolution: exit=$RESOLUTION_EXIT class=$RESOLUTION_CLASS"
case "$RESOLUTION_CLASS" in
    CONTINUE)
        # CONTINUE — silent per the locked rule (no operator alert on
        # status-quo to prevent alert fatigue per kill-bar mis-calibration).
        :
        ;;
    PROMOTE)
        # PROMOTE — captured separately by promotion_check above, but
        # surface here too so the resolution view is complete.
        notify_telegram INFO "weekly_audit resolution: PROMOTE" \
"forward_paper_resolution returned PROMOTE (Rule 4 — all gates pass).
This duplicates the promotion_check CRITICAL alert above; treat them
as a paired confirmation."
        ;;
    WATCH)
        # WATCH — soft signal in slack window. INFO only per locked rule.
        notify_telegram INFO "weekly_audit resolution: WATCH" \
"forward_paper_resolution returned WATCH — 1-2 soft signals fired
(slack-window threshold approached, single drift firing, etc.). Not
a kill; investigate at next operator session.
$(echo "$RESOLUTION_OUTPUT" | grep '•' | head -3)"
        ;;
    OPERATOR_REVIEW)
        # OPERATOR_REVIEW — locked rule's middle ground. WARN tier so
        # the operator knows but isn't paged at CRITICAL.
        notify_telegram WARN "weekly_audit resolution: OPERATOR_REVIEW" \
"forward_paper_resolution returned OPERATOR_REVIEW — rule application
produced no clear answer (3+ soft signals, slow-bleed, low trade rate,
or input freshness issue). Cross-check drift detector + write rationale
before continuing. Per the locked rule, OPERATOR_REVIEW is NOT a kill.
$(echo "$RESOLUTION_OUTPUT" | grep '•' | head -3)"
        ;;
    KILL)
        # KILL — captured separately by kill_check above, but surface
        # here too. Paired CRITICAL with the kill_check alert.
        notify_telegram CRITICAL "weekly_audit resolution: KILL" \
"forward_paper_resolution returned KILL (Rule 1). Paired confirmation
with kill_check above. Execute auto-kill per auto_kill_execution_
decision_rule_2026-05-08.md."
        ;;
    INPUT_ERROR)
        # INPUT_ERROR — distinct from CONTINUE per audit-pattern: must
        # not collapse into "no kill, all clear." WARN tier.
        notify_telegram WARN "weekly_audit resolution: INPUT_ERROR" \
"forward_paper_resolution returned INPUT_ERROR — missing/stale snapshot
or drift history. The locked rule cannot be evaluated against incomplete
input. Investigate the cron output to see which input shape failed.
$(echo "$RESOLUTION_OUTPUT" | tail -3)"
        ;;
    UNEXPECTED|*)
        # Crash / OOM / env corruption — outside documented contract 0-5.
        notify_telegram WARN "weekly_audit resolution: UNEXPECTED EXIT" \
"forward_paper_resolution returned exit $RESOLUTION_EXIT (outside documented
contract 0-5). Likely script crash, OOM, or env failure. Investigate before
next cron firing.
$(echo "$RESOLUTION_OUTPUT" | tail -3)"
        ;;
esac

# --- 7. Fleet-wide source-to-receipt lag check ---
# lag_summary.sh aggregates the lag_p99_ms percentiles emitted by the
# REST-poll-lag instrumentation (commit 033ed02). Stage 7 closes the
# loop between the instrumentation and operator-actionable alerts —
# without this, lag degradation is only visible at redeploy time via
# scripts/post_deploy_check.sh §4 (between deploys it goes unmonitored).
# Tier contract matches lag_summary's locked exit codes:
#   0 HEALTHY      — no alert
#   1 DEGRADED     — WARN (p99 > 15s on ≥1 engine)
#   2 HIGH         — CRITICAL (p99 > 30s — severe degradation)
#   3 SSH_FAILURE  — WARN
#   4 INPUT_ERROR  — WARN
#   else           — UNEXPECTED WARN (script crash etc.)
# Runs in full mode so the captured output is suitable for the
# decision_snapshots/<date>-lag.txt persistence below.
LAG_OUTPUT=$("${REPO_ROOT}/scripts/lag_summary.sh" 2>&1)
LAG_EXIT=$?
LAG_CLASS=$(_classify_python_exit "$LAG_EXIT" "1:DEGRADED,2:HIGH,3:SSH_FAILURE,4:INPUT_ERROR" "0")
echo "lag_summary: exit=$LAG_EXIT class=$LAG_CLASS"
case "$LAG_CLASS" in
    CONTINUE)
        # 0 HEALTHY — documented status quo, no alert.
        :
        ;;
    DEGRADED)
        notify_telegram WARN "weekly_audit lag: DEGRADED" \
"lag_summary returned exit 1 — ≥1 engine reported lag_p99 > 15s in the
last heartbeat. Source-to-receipt lag (Binance trade time → our receipt)
is above the TYPICAL band; not severe but operator should investigate
upstream API or network conditions.

Run: scripts/lag_summary.sh
Snapshot: results/decision_snapshots/$(date -u +%Y-%m-%d)-lag.txt
$(echo "$LAG_OUTPUT" | grep -E '⚠|🚨|DEGRADED' | head -3)"
        ;;
    HIGH)
        # CRITICAL because p99 > 30s indicates severe upstream
        # degradation that will manifest as realized fill drift at
        # Layer 2. Operator should investigate before the next cron.
        notify_telegram CRITICAL "weekly_audit lag: HIGH on $(hostname)" \
"lag_summary returned exit 2 — ≥1 engine reported lag_p99 > 30s. Severe
source-to-receipt lag indicating upstream API degradation, network
partition, or REST polling falling behind. At Layer 2 testnet this
would manifest as realized fill drift; at paper today it's an early
warning that the data pipeline is unhealthy.

Run: scripts/lag_summary.sh
Snapshot: results/decision_snapshots/$(date -u +%Y-%m-%d)-lag.txt
$(echo "$LAG_OUTPUT" | grep -E '🚨|HIGH' | head -3)"
        ;;
    SSH_FAILURE)
        notify_telegram WARN "weekly_audit lag: SSH_FAILURE" \
"lag_summary returned exit 3 — could not reach VPS to fetch heartbeats.
Same shape as the F2-fix tier classifier on journal_validate: this is
operational/transient (network, ssh, auth) not lag degradation.
$(echo "$LAG_OUTPUT" | tail -3)"
        ;;
    INPUT_ERROR)
        notify_telegram WARN "weekly_audit lag: INPUT_ERROR" \
"lag_summary returned exit 4 — bad flags or missing dependencies on
the cron environment. Investigate before next firing.
$(echo "$LAG_OUTPUT" | tail -3)"
        ;;
    UNEXPECTED|*)
        notify_telegram WARN "weekly_audit lag: UNEXPECTED EXIT" \
"lag_summary returned exit $LAG_EXIT (outside documented contract 0-4).
Likely script crash, OOM, or env failure.
$(echo "$LAG_OUTPUT" | tail -3)"
        ;;
esac

# --- 8. End-to-end promotion-readiness rehearsal ---
# promotion_rehearsal.sh walks all 5 phases of the locked STAGE_0→STAGE_1
# promotion path (LIMBO + STAGE_0→1 gates + kill criteria + Layer 2
# attestation + Layer 3 attestation). Phases 1-3 invoke the same Python
# decision-grade tools as stages 4-6 above — redundant ~5s of Python work,
# acceptable for the weekly cron.
#
# The value-add over stages 4-6 alone: phases 4-5 inspect the operator-
# touched `.last_pass` / `.last_layer3_pass` attestation markers. Without
# this stage, weekly_audit could fire PROMOTE_READY (stage 5) while Layer
# 2 / Layer 3 attestation was stale or missing — the operator would only
# discover this when invoking promotion_rehearsal manually.
#
# Tier mapping per the script's contract (0 READY / 1 BLOCKED / 2 WAITING / 3 INPUT_ERR):
#   READY     → INFO (paired with PROMOTE_READY above, but covers attestation)
#   BLOCKED   → CRITICAL (real blocker; could be Layer 2/3 attestation gone stale)
#   WAITING   → silent (status quo while data accumulates)
#   INPUT_ERR → WARN (helper missing / config error)
REHEARSAL_OUTPUT=$("${REPO_ROOT}/scripts/promotion_rehearsal.sh" --quiet 2>&1)
REHEARSAL_EXIT=$?
REHEARSAL_CLASS=$(_classify_python_exit "$REHEARSAL_EXIT" \
    "0:READY,1:BLOCKED,3:INPUT_ERR" "2")
echo "rehearsal: exit=$REHEARSAL_EXIT class=$REHEARSAL_CLASS"
case "$REHEARSAL_CLASS" in
    CONTINUE)
        # 2 WAITING — phases incomplete, data still accumulating. Silent.
        :
        ;;
    READY)
        # READY: all 5 phases green, INCLUDING Layer 2 + Layer 3 attestation
        # markers fresh. INFO tier — pairs with the PROMOTE_READY CRITICAL
        # from stage 5, but ADDS the attestation verification stages 4-6
        # don't cover. A bare PROMOTE_READY without rehearsal-READY means
        # operator must still verify Layer 2 + Layer 3 attestations before
        # flipping the executor.
        notify_telegram INFO "weekly_audit rehearsal: READY on $(hostname)" \
"promotion_rehearsal returned exit 0 — ALL 5 phases green, including the
operator-touched Layer 2 + Layer 3 attestation markers. Combined with the
PROMOTE_READY CRITICAL above (stage 5), the operator may execute
stage_promotion.sh now.

If stage 5 fired but rehearsal did NOT, the Layer 2 + Layer 3 attestations
have aged out — re-run layer2_smoke.sh + layer3_verdict.sh and touch
.last_pass + .last_layer3_pass before promoting.

Run: scripts/promotion_rehearsal.sh
Snapshot: results/decision_snapshots/$(date -u +%Y-%m-%d)-rehearsal.txt
$REHEARSAL_OUTPUT"
        ;;
    BLOCKED)
        # BLOCKED: ≥1 phase has a real problem. The kill_check above
        # likely already fired CRITICAL on the same root cause, but
        # rehearsal-BLOCKED also captures Layer 2/3 attestation going
        # stale — which would not appear in stage 4 or stage 5.
        notify_telegram CRITICAL "weekly_audit rehearsal: BLOCKED on $(hostname)" \
"promotion_rehearsal returned exit 1 — ≥1 phase has a real problem.
Most likely paired with the kill_check / kill_protocol CRITICAL above
(LIMBO=KILL or kill_protocol_check fires). BUT if no kill alert fired
and rehearsal still BLOCKED, the cause is Layer 2 or Layer 3
attestation state.

Run: scripts/promotion_rehearsal.sh
Snapshot: results/decision_snapshots/$(date -u +%Y-%m-%d)-rehearsal.txt
$REHEARSAL_OUTPUT"
        ;;
    INPUT_ERR)
        notify_telegram WARN "weekly_audit rehearsal: INPUT_ERR" \
"promotion_rehearsal returned exit 3 — config or helper issue.
Likely a missing sibling helper (forward_paper_resolution.py,
stage_promotion_check.py, kill_protocol_check.py) or environment
problem. Investigate before next cron firing.
$REHEARSAL_OUTPUT"
        ;;
    UNEXPECTED|*)
        # Crash / OOM / env corruption — outside documented contract 0-3.
        notify_telegram WARN "weekly_audit rehearsal: UNEXPECTED EXIT" \
"promotion_rehearsal returned exit $REHEARSAL_EXIT (outside documented contract 0-3).
Likely script crash, OOM, or env failure. Investigate before next cron firing.
$REHEARSAL_OUTPUT"
        ;;
esac

# --- 9. Layer 3 reconcile liveness (silent-stall guard) ---
# The Layer 3 testnet reconciler can silently lose its STAGE_1 parity gate if a
# Binance IP ban recurs (creds expiry, testnet outage, a failure mode the
# 2026-05-29 backoff fix doesn't cover). Count `positionRisk 418` over the last
# 24h across Layer 3 engine logs (those whose ExecStart carries
# --layer3-binance-testnet-journal-dir) and alert if the ban is sustained.
# Non-fatal — never masks the drift verdict (DRIFT_EXIT preserved). Engines are
# discovered dynamically so a fleet with no Layer 3 wrap is a clean no-op.
LAYER3_BAN_COUNT=$(ssh -o BatchMode=yes -o ConnectTimeout=10 "$WEEKLY_AUDIT_VPS" '
    units=$(systemctl list-units "paper-live@*.service" --no-legend --plain 2>/dev/null | awk "{print \$1}")
    layer3_syms=""
    for u in $units; do
        es=$(systemctl show -p ExecStart --value "$u" 2>/dev/null)
        case "$es" in *--layer3-binance-testnet-journal-dir*)
            sym=$(echo "$u" | sed "s/paper-live@//; s/\.service//")
            layer3_syms="$layer3_syms $sym" ;;
        esac
    done
    [ -z "$layer3_syms" ] && { echo "NONE"; exit 0; }
    # Window: last 24h. Match ISO dates for today + yesterday (UTC), count 418s.
    d_today=$(date -u +%Y-%m-%d); d_yest=$(date -u -d "yesterday" +%Y-%m-%d 2>/dev/null || date -u -v-1d +%Y-%m-%d)
    total=0
    for s in $layer3_syms; do
        c=$(grep -hE "$d_today|$d_yest" /var/log/paper-live/$s.log 2>/dev/null | grep -c "positionRisk 418")
        total=$((total + c))
    done
    echo "$total"
' 2>/dev/null) || LAYER3_BAN_COUNT="-1"
LAYER3_BAN_COUNT="${LAYER3_BAN_COUNT:--1}"

if [[ "$LAYER3_BAN_COUNT" == "NONE" ]]; then
    echo "Stage 9 (Layer 3 liveness): no Layer 3 engines deployed — skipped"
else
    LAYER3_HEALTH=$(_classify_layer3_health "$LAYER3_BAN_COUNT")
    echo "Stage 9 (Layer 3 liveness): 418-bans/24h=$LAYER3_BAN_COUNT → $LAYER3_HEALTH"
    case "$LAYER3_HEALTH" in
        OK) : ;;  # healthy — no alert
        DEGRADED)
            notify_telegram WARN "weekly_audit Layer 3 reconcile DEGRADED on $(hostname)" \
"Layer 3 testnet reconciler logged $LAYER3_BAN_COUNT positionRisk 418 (IP-ban) responses
in the last 24h (threshold ${LAYER3_BAN_THRESHOLD:-10}). The STAGE_1 same-tick parity gate
may be starving — reconcile cannot confirm fills while banned.

This is the failure mode fixed 2026-05-29 (reconciler 418 backoff). Recurrence means
the backoff is not keeping up OR a new cause (testnet creds expired, testnet outage).
Run: ssh $WEEKLY_AUDIT_VPS 'grep \"positionRisk 418\" /var/log/paper-live/{ens,kava}usdt.log | tail'"
            ;;
        SSH_FAILURE)
            notify_telegram WARN "weekly_audit Layer 3 liveness: VPS query failed" \
"Could not query Layer 3 reconcile-ban count on $WEEKLY_AUDIT_VPS (remote query failed).
Layer 3 health UNKNOWN this cycle — not necessarily broken, but unverified. Investigate
if it persists across cycles."
            ;;
        *)
            notify_telegram WARN "weekly_audit Layer 3 liveness: UNEXPECTED" \
"_classify_layer3_health returned an unexpected verdict for count='$LAYER3_BAN_COUNT'.
Likely a script/parsing bug introduced in weekly_audit.sh. Investigate."
            ;;
    esac
fi

# --- Persist decision snapshots for longitudinal review ---
SNAP_DIR="${REPO_ROOT}/results/decision_snapshots"
mkdir -p "$SNAP_DIR"
TODAY=$(date -u +%Y-%m-%d)
echo "$KILL_OUTPUT" > "${SNAP_DIR}/${TODAY}-kill.txt"
echo "$PROMOTE_OUTPUT" > "${SNAP_DIR}/${TODAY}-promote.txt"
echo "$RESOLUTION_OUTPUT" > "${SNAP_DIR}/${TODAY}-resolution.txt"
echo "$LAG_OUTPUT" > "${SNAP_DIR}/${TODAY}-lag.txt"
echo "$REHEARSAL_OUTPUT" > "${SNAP_DIR}/${TODAY}-rehearsal.txt"

# --- exit with the drift wrapper's code so launchd surfaces the right thing ---
# Rationale: drift is the decision-grade KILL signal calibrated against null;
# kill_protocol_check is broader (covers slip / concentration / drawdown) but
# fires within drift's domain too. Using DRIFT_EXIT keeps launchctl's
# last-exit-code column meaningful as a kill signal — the kill_protocol_check
# value is delivered via Telegram, not via wrapper exit code.
exit "$DRIFT_EXIT"
