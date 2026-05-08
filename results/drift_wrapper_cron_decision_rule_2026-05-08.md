# Drift wrapper cron — locked design rule (2026-05-08)

**Status:** LOCKED 2026-05-08, before any cron infrastructure exists. Applies mechanically when triggered (see Migration triggers).

## Question

`scripts/run_drift_check.sh` (committed `e2bca04`) wraps `scripts/live_vs_backtest_drift.py` with state persistence so the locked **two-firings-≥7d-apart auto-kill rule** (per `results/drift_detector_time_to_detection_verdict_2026-05-08.md`) is mechanically evaluable across sessions. The wrapper has no scheduler — cadence currently relies on operator memory.

The locked weekly cadence requirement implies *some* form of automation. **What is the deployment shape?** Locking now prevents debate when the question becomes urgent (STAGE_X promotion, or a drift firing with a missed week).

## Locked decision rule

The rule is phased by **forward-paper STAGE** because the detector is investigation-grade only at low real-money exposure, and the cost of a missed week scales with the dollar value at stake.

### Phase 1 — Paper-only (no real money allocated, current state)

**Manual operator invocation. No automated cron required.**

- Cadence: weekly. Operator sets up a recurring calendar reminder (e.g., Sunday evening). Out-of-scope to enforce in code.
- Mechanism: `./scripts/run_drift_check.sh` from the operator workstation.
- State: `results/drift_check_history.jsonl` and `results/drift_runs/<ts>.log` in the local repo.

**Justification:**
- Detector is INSUFFICIENT until `n_live ≥ 30` closes. At fleet rate ~1.21/day this is ≈25 calendar days minimum from the first close. For most of Phase 1 the wrapper would log noise.
- Detector is investigation-grade — a single firing is "investigate", not "auto-kill". Operator review is already in the loop.
- ±2-day cadence variance from operator-memory-induced jitter does not materially inflate sequential FP. The 28%/year inflation cited in the time-to-detection verdict came from *daily* testing, not from skipped weeks.
- Zero real-money exposure → cost of a missed week is reputational, not financial.

### Phase 2 — STAGE_1 / STAGE_2 real money active ($100/trade and $300/trade)

**Manual + calendar reminder remains acceptable, with a hard pre-promotion check.**

- Same mechanism as Phase 1.
- **NEW:** before processing any STAGE-promotion event, operator must verify the wrapper has been run within the prior 7 days. If not, run it BEFORE the promotion proceeds.
- The pre-promotion check ensures the auto-kill rule has had its chance to fire on accumulated data before the operator commits more capital.

**Justification:** STAGE_1/2 sizes are bounded ($100-$300/trade). A worst-case missed-week scenario (4 net-loss trades at full stop, max 5× stake = $1.5k-$4.5k drawdown) is recoverable. The pre-promotion check forces a fresh wrapper run at the highest-leverage moments (size-up decisions).

### Phase 3 — STAGE_3 / STAGE_4 real money active ($500/trade and $1k/trade)

**Automated cadence is REQUIRED. VPS systemd timer is the locked implementation.**

- Cadence: weekly, Sunday 02:00 UTC (low-activity window for Binance).
- Mechanism: `/etc/systemd/system/paper-live-drift-check.{timer,service}` on the production VPS.
- ExecStart: `/opt/trading-engine/scripts/run_drift_check.sh --quiet --live-source local --live-dir /var/log/paper-live/journal`.
- User: `paperlive` (matches existing engine units).
- State: `/opt/trading-engine/results/drift_check_history.jsonl` on VPS, rsync'd to operator workstation on every login (operator script: `scripts/sync_drift_state.sh`, deferred to Phase 3 deployment).
- Verdict propagation: Telegram alert on wrapper exit code 4 (AUTO-KILL CANDIDATE) using the existing `pkg/notify/telegram.go` infrastructure. Exit codes 1 (single firing) and 2 (insufficient) suppress alert (operator reviews on next login).

**Justification:** $500-$1k/trade exposure makes operator-memory cadence inadequate. A missed 2-week window at STAGE_4 with adverse drift could accumulate $20k+ in losses before detection. Automated cadence + Telegram alerting tightens the worst-case to ~24h.

**Required deps on VPS at Phase 3 activation:**
- python3 (already present)
- jq (already present)
- `/opt/trading-engine/scripts/run_drift_check.sh` (deployed via existing sync)
- `/opt/trading-engine/scripts/live_vs_backtest_drift.py` (NOT currently in the deploy set — must be added)
- `/opt/trading-engine/results/hod_journals/2026-05-07-mfe/` (backtest reference data — NOT currently in the deploy set; must be added; ~5MB)
- Telegram bot token / chat id (already in `/etc/paper-live/env` per the existing notify integration)

## Alternatives considered (rejected)

### GitHub Actions cron

**Considered:** A weekly GHA workflow that SSHes to the VPS and runs the wrapper, committing state back to the repo.

**Rejected because:**
- Adds GHA workflow + SSH-secret storage as a new failure surface.
- VPS systemd aligns with existing watchdog/digest timers (operational coherence).
- State commit-back means automated commits to `main`, which complicates the audit trail.
- Re-evaluate if VPS is ever migrated to a host where systemd is unavailable.

### Local laptop launchd

**Considered:** A `~/Library/LaunchAgents/com.user.drift-check.plist` running the wrapper from the operator's machine.

**Rejected because:**
- Laptop-offline weeks silently miss the cron — exactly the failure mode the locked rule was designed against.
- State is laptop-local; operator changes (different machine, OS reinstall) lose the history index.
- Manual operator invocation is more reliable: the operator at least knows they ran it.

### Always-automated from Phase 1

**Considered:** Deploy the VPS systemd cron immediately, regardless of forward-paper progress.

**Rejected because:**
- Detector returns INSUFFICIENT for the first ~25 days of forward-paper (n_live below floor). Cron would log noise.
- Premature deployment of python detector + reference data on VPS expands the deployment surface before there's any real-money rationale.
- Defer to Phase 3 when the cost-of-missed-week justifies the infrastructure.

### Daily cadence (regardless of phase)

**Rejected by the upstream verdict.** `results/drift_detector_time_to_detection_verdict_2026-05-08.md` found daily testing inflates sequential FP to 28%/year. Locked at WEEKLY across all phases.

## Migration triggers

The rule re-opens for design when any of these fire:

1. **Promotion to STAGE_3:** Phase 3 implementation REQUIRED before promotion completes. Pre-promotion checklist must include "drift cron deployed and verified firing weekly for ≥4 weeks."
2. **Drift firing during Phase 1/2:** Re-evaluate whether the rule was correctly applied. If a missed-week pattern was the underlying cause, the rule advances (e.g., Phase 2 might require automated cadence).
3. **VPS host migration:** Phase 3 deployment plan needs re-validation; this doc re-opens.
4. **Wrapper exit-code semantics change:** If `run_drift_check.sh`'s exit codes shift (currently 0/1/2/3/4), the Telegram alert filter and any cron-side gating must be re-derived.

## Cross-references

- `results/drift_detector_calibration_verdict_2026-05-07.md` — the locked operating point (α_family=0.001, N_LIVE=50)
- `results/drift_detector_time_to_detection_verdict_2026-05-08.md` — the cadence rule + sequential-FP nuance
- `results/real_money_protocol_decision_rule_2026-05-08.md` — STAGE_1 → STAGE_4 promotion criteria
- `scripts/run_drift_check.sh` — the wrapper this rule schedules
- `CLAUDE.md ## Forward-paper go/no-go criteria → Kill mechanism` — operational doctrine
