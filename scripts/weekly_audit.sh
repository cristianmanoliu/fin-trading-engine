#!/usr/bin/env bash
# weekly_audit.sh — operational wrapper combining the two artifacts the
# Sunday-09:00 launchd job produces:
#
#   1. run_drift_check.sh — decision-grade kill signal (exit 0/1/2/3/4)
#   2. forward_paper_status.sh — operational snapshot, dated under
#      results/forward_paper_snapshots/<YYYY-MM-DD>.txt for longitudinal
#      diffing
#
# Drift check exits with the decision-grade severity; this wrapper
# preserves that as its OWN exit code so launchd's last-exit-code column
# (visible via `launchctl list`) reflects the kill signal, not snapshot
# success.
#
# Snapshot failure is non-fatal — a transient SSH or jq error at snapshot
# time should not mask a real drift verdict. The drift check ran first
# anyway, so the decision-grade signal is already persisted to
# results/drift_check_history.jsonl regardless of what happens here.
#
# Usage:
#   ./scripts/weekly_audit.sh              # default — invoked by launchd
#   ./scripts/weekly_audit.sh --no-snapshot  # only run drift check
#
# Composable with deploy/drift-check.launchd.plist; the plist's
# ProgramArguments points at this wrapper instead of run_drift_check.sh
# directly.
set -uo pipefail

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
VALIDATE_OUTPUT=$(ssh root@178.105.24.230 \
    '/opt/trading-engine/bin/journal_validate --dir /var/log/paper-live/journal --exclude archive 2>&1')
VALIDATE_EXIT=$?
echo "validate: exit=$VALIDATE_EXIT"
echo "$VALIDATE_OUTPUT" | tail -1
if [[ "$VALIDATE_EXIT" -ge 2 ]]; then
    # ERROR-level: corruption detected. Telegram CRITICAL — operator must
    # investigate before next forward-paper analysis.
    notify_telegram CRITICAL "weekly_audit on $(hostname)" \
"journal_validate found errors in live journals
exit=$VALIDATE_EXIT
last line: $(echo "$VALIDATE_OUTPUT" | tail -1)
Run: ssh root@178.105.24.230 /opt/trading-engine/bin/journal_validate --dir /var/log/paper-live/journal --exclude archive"
elif [[ "$VALIDATE_EXIT" -eq 1 ]]; then
    # WARN-only: usually trailing-malformed-line tolerance. Logged but
    # not alerted (operator can review snapshot/run logs).
    echo "validate: warnings present (non-blocking)"
fi

# --- 4. Kill-protocol check (decision-grade STOP signal) ---
# The kill_protocol_check.py mechanizes 4 of 6 locked kill criteria from
# real_money_protocol_decision_rule_2026-05-08.md §Kill criteria. Exit 1
# = at least one criterion fires; operator MUST stop the protocol.
# Cadence-aligned with drift detector (weekly) — kill criteria use the
# same drift_check_history that the drift cron writes.
KILL_OUTPUT=$(python3 "${REPO_ROOT}/scripts/kill_protocol_check.py" 2>&1)
KILL_EXIT=$?
echo "kill_check: exit=$KILL_EXIT"
case "$KILL_EXIT" in
    1)
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
    4)
        # OPERATOR-VERIFY: mechanical pass + #3 stake-assumption or
        # #5 unrecoverable-error require operator review. WARN tier.
        notify_telegram WARN "weekly_audit kill: operator-verify needed" \
"kill_protocol_check returned exit 4 — mechanical criteria pass but ≥1 deferred
criterion requires operator confirmation (e.g., paper-money stake interpretation,
unrecoverable-error log scan). Default action: CONTINUE if those check out.

Run: python3 scripts/kill_protocol_check.py"
        ;;
    3)
        # ERROR: input/env failure. WARN — operator should investigate.
        notify_telegram WARN "weekly_audit kill: ERROR" \
"kill_protocol_check returned exit 3 (input/env failure). Operator should
investigate the cron environment.
$(echo "$KILL_OUTPUT" | tail -3)"
        ;;
    *)
        # 0 CONTINUE / 2 WAITING — status quo, no alert.
        :
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
echo "promotion_check: exit=$PROMOTE_EXIT"
case "$PROMOTE_EXIT" in
    0)
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
    4)
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
    1)
        # BLOCKED: a gate fails outright after sufficient data. Could
        # repeat weekly while data accumulates and a criterion stays
        # failing — accept the noise, operator should know.
        notify_telegram WARN "weekly_audit promotion BLOCKED" \
"stage_promotion_check returned exit 1 — at least one locked promotion gate
fails outright at current data volume. Forward-paper is not on track for
STAGE_1 promotion under current conditions.

Run: python3 scripts/stage_promotion_check.py"
        ;;
    3)
        notify_telegram WARN "weekly_audit promotion: ERROR" \
"stage_promotion_check returned exit 3 (input/env failure).
$(echo "$PROMOTE_OUTPUT" | tail -3)"
        ;;
    *)
        # 2 WAITING — status quo while data accumulates.
        :
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
echo "resolution: exit=$RESOLUTION_EXIT"
case "$RESOLUTION_EXIT" in
    0)
        # CONTINUE — silent per the locked rule (no operator alert on
        # status-quo to prevent alert fatigue per kill-bar mis-calibration).
        :
        ;;
    1)
        # PROMOTE — captured separately by promotion_check above, but
        # surface here too so the resolution view is complete.
        notify_telegram INFO "weekly_audit resolution: PROMOTE" \
"forward_paper_resolution returned PROMOTE (Rule 4 — all gates pass).
This duplicates the promotion_check CRITICAL alert above; treat them
as a paired confirmation."
        ;;
    2)
        # WATCH — soft signal in slack window. INFO only per locked rule.
        notify_telegram INFO "weekly_audit resolution: WATCH" \
"forward_paper_resolution returned WATCH — 1-2 soft signals fired
(slack-window threshold approached, single drift firing, etc.). Not
a kill; investigate at next operator session.
$(echo "$RESOLUTION_OUTPUT" | grep '•' | head -3)"
        ;;
    3)
        # OPERATOR_REVIEW — locked rule's middle ground. WARN tier so
        # the operator knows but isn't paged at CRITICAL.
        notify_telegram WARN "weekly_audit resolution: OPERATOR_REVIEW" \
"forward_paper_resolution returned OPERATOR_REVIEW — rule application
produced no clear answer (3+ soft signals, slow-bleed, low trade rate,
or input freshness issue). Cross-check drift detector + write rationale
before continuing. Per the locked rule, OPERATOR_REVIEW is NOT a kill.
$(echo "$RESOLUTION_OUTPUT" | grep '•' | head -3)"
        ;;
    4)
        # KILL — captured separately by kill_check above, but surface
        # here too. Paired CRITICAL with the kill_check alert.
        notify_telegram CRITICAL "weekly_audit resolution: KILL" \
"forward_paper_resolution returned KILL (Rule 1). Paired confirmation
with kill_check above. Execute auto-kill per auto_kill_execution_
decision_rule_2026-05-08.md."
        ;;
    5)
        # INPUT_ERROR — distinct from CONTINUE per audit-pattern: must
        # not collapse into "no kill, all clear." WARN tier.
        notify_telegram WARN "weekly_audit resolution: INPUT_ERROR" \
"forward_paper_resolution returned INPUT_ERROR — missing/stale snapshot
or drift history. The locked rule cannot be evaluated against incomplete
input. Investigate the cron output to see which input shape failed.
$(echo "$RESOLUTION_OUTPUT" | tail -3)"
        ;;
esac

# --- Persist decision snapshots for longitudinal review ---
SNAP_DIR="${REPO_ROOT}/results/decision_snapshots"
mkdir -p "$SNAP_DIR"
TODAY=$(date -u +%Y-%m-%d)
echo "$KILL_OUTPUT" > "${SNAP_DIR}/${TODAY}-kill.txt"
echo "$PROMOTE_OUTPUT" > "${SNAP_DIR}/${TODAY}-promote.txt"
echo "$RESOLUTION_OUTPUT" > "${SNAP_DIR}/${TODAY}-resolution.txt"

# --- exit with the drift wrapper's code so launchd surfaces the right thing ---
# Rationale: drift is the decision-grade KILL signal calibrated against null;
# kill_protocol_check is broader (covers slip / concentration / drawdown) but
# fires within drift's domain too. Using DRIFT_EXIT keeps launchctl's
# last-exit-code column meaningful as a kill signal — the kill_protocol_check
# value is delivered via Telegram, not via wrapper exit code.
exit "$DRIFT_EXIT"
