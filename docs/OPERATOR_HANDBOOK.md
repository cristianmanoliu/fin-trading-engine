# Operator handbook

Operator-facing distillation of the mechanized forward-paper monitoring
system. Reference for "what should I look at, when, and what does it mean?"

For project history, locked decision rules, and architecture detail see
`CLAUDE.md` and `results/INDEX.md`. This document is intentionally focused:
**daily/weekly cadence + alert response + tool map**.

---

## Mode you're in (2026-05-10)

**MONITORING.** Forward-paper accumulating; mechanical machinery emits
weekly verdict via `weekly_audit.sh` launchd cron. Earliest STAGE_1
promotion ~2026-09-13 (n=150 trades is the binding gate at fleet rate
1.18/day). Real-money allocation: ZERO until LIMBO rule emits PROMOTE
verdict AND Layer 2/3 operational gates pass.

**Your role for the next ~4 months:** receive Telegram alerts on state
changes, otherwise wait. The system is now mostly self-monitoring.

---

## Cadence

### Daily (≤ 5 min, optional)

Most days, do nothing. The cron does the work.

If you check anyway:

```bash
ssh root@178.105.24.230 'systemctl list-units "paper-live@*.service" --state=active --no-legend | wc -l'
# Expected: 16
```

That's it. If it's 16, the engines are running. If it's <16, something needs investigation — but `post_deploy_check.sh` section 12 (restart-loop detection) will Telegram you within minutes anyway.

### Weekly (≤ 15 min, every Sunday)

The launchd cron `com.tradingengine.drift-check` fires every Sunday at 09:00 local. It runs `scripts/weekly_audit.sh` which executes 7 stages and Telegram-routes per the locked tier mapping:

1. `run_drift_check.sh` — decision-grade kill signal (exit 0/1/2/3/4/5 — **NEW exit 5 = HISTORY_CORRUPT**: malformed line in `drift_check_history.jsonl`, two-firings rule cannot be evaluated until repaired)
2. `forward_paper_status.sh` snapshot → `results/forward_paper_snapshots/<date>.txt`
3. `cmd/journal_validate` — journal self-consistency
4. `kill_protocol_check.py` — locked kill criteria
5. `stage_promotion_check.py` — locked promotion criteria
6. `forward_paper_resolution.py` — LIMBO 5-verdict synthesis
7. `lag_summary.sh` — fleet-wide source-to-receipt lag aggregator (commit `7784c6a`). Tier verdict: HEALTHY (silent) / DEGRADED (WARN, p99 > 15s on ≥1 engine) / HIGH (CRITICAL, p99 > 30s — severe API degradation)

If you receive **NO Telegram alerts** Sunday 09:00–10:00, the system is healthy. **Status quo means silence.** Don't be alarmed by silence.

If you want the Sunday read anyway:

```bash
cat results/forward_paper_snapshots/$(date -u +%Y-%m-%d).txt        # forward_paper_status snapshot (stage 2)
cat results/decision_snapshots/$(date -u +%Y-%m-%d)-resolution.txt  # LIMBO verdict (stage 6)
cat results/decision_snapshots/$(date -u +%Y-%m-%d)-kill.txt        # kill_protocol_check (stage 4)
cat results/decision_snapshots/$(date -u +%Y-%m-%d)-promote.txt     # stage_promotion_check (stage 5)
cat results/decision_snapshots/$(date -u +%Y-%m-%d)-lag.txt         # lag_summary fleet view (stage 7)
```

The first is the operator dashboard; the rest are decision-grade verdicts written by the cron's stages 4–7. A weekly fire that produces `-kill.txt` + `-promote.txt` but **no `-resolution.txt`** means stage 6 (LIMBO) did NOT run — investigate. Same shape for missing `-lag.txt` and stage 7.

For trends across snapshots:

```bash
python3 scripts/forward_paper_trajectory.py
python3 scripts/realized_cost_trajectory.py
```

### Monthly (≤ 30 min)

- Refresh funding CSVs: `scripts/refresh_funding.sh && deploy/redeploy.sh all`
- Verify drift-cron is actually firing: `launchctl list | grep tradingengine` (last column = exit code: 0 CLEAN / 1 INVESTIGATION / 2 INSUFFICIENT / 3 ERROR / 4 AUTO-KILL)

---

## Telegram tier guide

Tier prefixes (from `pkg/notify/telegram.go`):

| Prefix | Severity | Rate limit | Mute hours | Response |
|---|---|---|---|---|
| ℹ | INFO | 10/hour | fully muted | Read at next opportunity |
| ⚠ | WARN | 5/hour | sent with `[QUIET]` prefix | Investigate within 24h |
| 🚨 | CRITICAL | unlimited | never muted | **Investigate immediately** |

### What fires CRITICAL (act now)

- **STARTUP FAILED** on engine deploy — operator misconfig (config / flag / creds). Run `ssh root@178.105.24.230 'journalctl -u paper-live@<symbol>.service -n 100'` to see the slog.
- **REAL-MONEY engine started** — when --executor=binance_live flips. Confirm intent.
- **Position drift detected** — exchange disagrees with local state. Engine BLOCKED for new orders on this symbol. Run `scripts/post_deploy_check.sh` to inspect; reconcile manually (close exchange position OR adjust journal); operator-call ClearDrift.
- **Recovery drift on engine startup** — restart blocked by exchange divergence. Investigate before restarting.
- **Drift detector exit 4 (auto-kill candidate)** — two firings ≥7 days apart OR drift+threshold match. Locked rule: stop the protocol. Execute per `results/auto_kill_execution_decision_rule_2026-05-08.md`.
- **kill_protocol_check exit 1** — at least one locked kill criterion fires (slip>30bp sustained, drawdown 20%, single-sym>50%, 3-consecutive-day-loss). Cross-check drift detector before acting.
- **forward_paper_resolution exit 4 (KILL)** — LIMBO Rule 1: locked kill criterion fired with paired confirmation. Execute `auto_kill_execution_decision_rule_2026-05-08.md`.
- **PROMOTION READY** — stage_promotion_check exit 0 — all gates pass. Pre-promotion checklist (Layer 2 + Layer 3) before flipping --executor.
- **Engine panic / crashed** — investigate logs, restart engine.
- **Journal write failed** — REAL-MONEY POSITION MAY BE INVISIBLE. Check disk + permissions.
- **weekly_audit lag: HIGH** (stage 7, ≥1 engine `lag_p99 > 30s`) — severe source-to-receipt lag indicating upstream API degradation, network partition, or REST polling falling behind. At Layer 2 this would manifest as realized fill drift; in paper today it's early warning the data pipeline is unhealthy.

### What fires WARN (investigate within 24h)

- Heartbeat: stalled feed, no ticks received. Threshold is 180s (aligned with WS→REST fallback boundary). Real stalls clear within a few minutes once REST polling engages; persistent fires on low-volume altcoins (KAVAUSDT, IMXUSDT, ROSEUSDT) during quiet sessions are not actionable.
- forward_paper_resolution: OPERATOR_REVIEW (3+ soft signals OR low trade rate OR slow-bleed)
- forward_paper_resolution: INPUT_ERROR (exit 5) — missing/stale snapshot or drift history; investigate cron health, NOT strategy
- weekly_audit: validate warnings (trailing-malformed-line tolerance)
- Layer 2 / Layer 3 startup events
- Funding CSV staleness >7 days
- post_deploy_check.sh STRICT-mode FAIL
- **weekly_audit lag: DEGRADED** (stage 7, ≥1 engine `lag_p99 > 15s`) — investigate upstream API or network conditions. Not severe but the lag pipeline detected something abnormal.
- **weekly_audit lag: SSH_FAILURE** (stage 7, exit 3) — could not fetch heartbeats from VPS to compute lag; operational/transient, not lag degradation itself.
- **drift_check: HISTORY_CORRUPT** (exit 5) — malformed line in `drift_check_history.jsonl`; two-firings rule cannot be evaluated until repaired. Inspect the file, remove or correct the malformed line, then re-run.

### What fires INFO (read at leisure)

- Engine started / stopped (clean)
- Position recovered + verified clean
- Position drift cleared
- forward_paper_resolution: PROMOTE (exit 1) — operator confirmation gate before STAGE_1 flip
- forward_paper_resolution: WATCH (exit 2) — 1-2 soft signals; flagged in weekly digest, no kill

---

## Tool map

### Status / monitoring (read-only)

| Tool | Purpose |
|---|---|
| `scripts/forward_paper_status.sh` | Per-cohort go/no-go status. Operator dashboard. |
| `scripts/forward_paper_trajectory.py` | Trend across snapshots — direction-of-travel. |
| `scripts/realized_cost_trajectory.py` | Per-trade fee/slip trend, not just cumulative average. |
| `scripts/forward_paper_resolution.py` | LIMBO 5-verdict synthesis (CONTINUE / WATCH / PROMOTE / KILL / OPERATOR_REVIEW). |
| `scripts/post_deploy_check.sh` | 13-section operational health audit. Run after every redeploy. §4 now includes per-engine `lag_p99` column (commit `58fd906`) — DEGRADED ≥15s, HIGH ≥30s tier rollups. `STRICT=1 ./scripts/post_deploy_check.sh` for fail-loud / CI mode (exit 1 + Telegram WARN on any FAIL). |
| `scripts/lag_summary.sh` | **NEW** (commit `bb53c1b`) — fleet-wide source-to-receipt lag aggregator. Per-engine p50/p99/max table + fleet rollup + tier verdict. `--quiet` for 1-line cron output; exit 0 HEALTHY / 1 DEGRADED / 2 HIGH / 3 SSH_FAILURE / 4 INPUT_ERROR. |
| `scripts/paper_live_trades.sh` | Per-engine trade summary. |
| `scripts/run_drift_check.sh` | Decision-grade kill detector (manual invocation). Exit codes: 0 CLEAN / 1 INVESTIGATION / 2 INSUFFICIENT / 3 ERROR / 4 AUTO-KILL CANDIDATE / **NEW 5 HISTORY_CORRUPT** (commit `3b664f1`). |

### Decision-grade evaluators (mechanical verdicts)

| Tool | Verdict |
|---|---|
| `scripts/kill_protocol_check.py` | Locked kill criteria → CONTINUE / KILL / WAITING / OPERATOR-VERIFY |
| `scripts/stage_promotion_check.py` | Locked promotion gates → PROMOTE / BLOCKED / WAITING / DEFERRED |
| `scripts/forward_paper_resolution.py` | LIMBO synthesis → 5 verdicts (see exit-code table below) |
| `scripts/layer3_verdict.sh` | Layer 3 7d shadow parity → PASS / THRESHOLD / SIGNAL_DIV / INPUT_ERROR / INSUFFICIENT_DURATION |

**`forward_paper_resolution.py` exit-code map** (mirrors locked rule's Telegram tier mapping):

| Exit | Verdict | Tier | Operator action |
|:---:|---|:---:|---|
| 0 | CONTINUE | silent | Nothing — system healthy, monitoring |
| 1 | PROMOTE | INFO | Pre-promotion checklist (Layer 2 + Layer 3 gates) before flipping `--executor` |
| 2 | WATCH | INFO | Read flagged metric in next weekly digest; no kill |
| 3 | OPERATOR_REVIEW | WARN | Manual cross-check + written rationale before CONTINUE-ing or KILLing |
| 4 | KILL | CRITICAL | Locked kill criterion fired — execute `auto_kill_execution_decision_rule_2026-05-08.md` |
| 5 | INPUT_ERROR | WARN | Distinct from CONTINUE — missing/stale snapshot or drift history. Investigate cron health, NOT strategy |

The 6-stage `weekly_audit.sh` cron exits with the **drift wrapper's** code so `launchctl list | grep tradingengine` surfaces drift state. Resolution / kill_check / promotion verdicts arrive **independently via Telegram** — exit 0 in launchctl ≠ "all five stages clean."

### Operations (modify state)

| Tool | Action |
|---|---|
| `deploy/redeploy.sh [symbol\|all]` | Sync code → rebuild → restart. After: ALWAYS run `scripts/post_deploy_check.sh`. |
| `scripts/refresh_funding.sh` | Update funding CSVs. After: redeploy + post_deploy_check. |
| `cmd/kill_switch` | Real-money emergency close-all. Dry-run by default; append `CONFIRM` to fire. |
| `scripts/paper_live_start.sh` / `_stop.sh` | Local-machine paper-live control (rare). |

### Forensics

| Tool | Purpose |
|---|---|
| `cmd/journal_diff` | Per-pair pnl drift between two journal dirs. |
| `cmd/journal_validate` | Journal self-consistency (cross-month opens, duplicates). |
| `cmd/journal_report` | Trade-level summary. |

---

## Scenario playbook

### Scenario: drift detector fires (exit 1)

1. Cross-check: `python3 scripts/live_vs_backtest_drift.py --verbose` — see WHICH metric tripped.
2. Investigate: was there a regime change in the past week? Check `results/forward_paper_snapshots/<recent>.txt` — single-sym concentration shift, slip drift, etc.
3. Decision: per `results/drift_firing_investigation_decision_rule_2026-05-08.md` triage A/B/C. **Single firing alone is INVESTIGATION, not auto-kill.**
4. If a second firing lands ≥7 days later → drift wrapper exits 4 → auto-kill candidate. Execute kill per `results/auto_kill_execution_decision_rule_2026-05-08.md`.

### Scenario: kill_protocol_check fires (exit 1)

1. Read which criterion: snapshot at `results/decision_snapshots/<date>-kill.txt`.
2. Cross-check drift detector — if drift is CLEAN, the threshold-criterion firing might be sampling variance (kill-bar mis-calibration finding 2026-05-07).
3. **If drift detector also fires → kill is decision-grade.** Execute per auto_kill_execution.
4. If only kill_protocol_check fires → investigation tier. Don't reflexively kill.

### Scenario: forward_paper_resolution returns OPERATOR_REVIEW

The script already tells you which sub-rule fired (3+ soft signals / low trade rate / slow-bleed). Per the locked rule:

1. Cross-check drift detector.
2. Investigate the named issue.
3. Document a written rationale before either CONTINUE-ing or escalating to KILL.
4. **OPERATOR_REVIEW is NOT a kill.** It's a "rule application produced no clear answer" outcome demanding manual judgment.

### Scenario: PROMOTION READY (stage_promotion_check exit 0)

This is the moment forward-paper crosses STAGE_1 promotion criteria. Pre-flight:

1. **Layer 2 testnet gate.** Generate Binance testnet credentials at `testnet.binancefuture.com` (SEPARATE from mainnet). Smoke-run: `BINANCE_API_KEY=<testnet> BINANCE_API_SECRET=<testnet> go run ./cmd/engine --config configs/btcusdt.yaml --executor binance_live_testnet`. Verify orders fill cleanly. Locked criterion: testnet smoke must pass before any mainnet flip.
2. **Layer 3 dual-runner shadow.** With testnet creds: `go run ./cmd/engine --config configs/btcusdt.yaml --layer3-binance-testnet-journal-dir /var/log/paper-live/journal-testnet`. Run for ≥7 calendar days.
3. **Layer 3 verdict:** `scripts/layer3_verdict.sh --stub-dir /var/log/paper-live/journal --testnet-dir /var/log/paper-live/journal-testnet`. Must return PASS (exit 0). Use `--skip-min-days` for a dry-run preview before the 7d gate elapses (verdict still computed; gate-shortfall not blocking).
4. **Mainnet creds.** Generate FRESH mainnet credentials separate from testnet.
5. **Stage 1 flip.** Per `results/real_money_protocol_decision_rule_2026-05-08.md`: $100/trade. Update systemd ExecStart with `--executor=binance_live` flag. Redeploy. Verify CRITICAL "REAL-MONEY engine started" alert fires.

### Scenario: Position drift detected on a symbol

1. Engine has BLOCKED new orders on this symbol via `IsDrifted` gate.
2. Investigate: SSH to VPS, check `journalctl -u paper-live@<symbol>` for the drift report.
3. Reconcile: either (a) close the exchange position to match local-empty, OR (b) adjust journal to match exchange.
4. Manually call `ClearDrift` via the engine — typically requires a controlled restart with the journal updated.
5. After ClearDrift, the next reconcile pass should return clean and the gate re-opens.

### Scenario: Real-money engine emergency kill

```bash
BINANCE_API_KEY=<key> BINANCE_API_SECRET=<secret> \
  go run ./cmd/kill_switch --reason drift_kill_$(date -u +%Y%m%d) \
                           --positions "BTCUSDT,LONG,0.5;ETHUSDT,SHORT,2" CONFIRM
```

CRITICAL Telegram alerts pre/post-fire. Idempotent — re-running with same positions is safe.

### Scenario: weekly_audit lag fires DEGRADED or HIGH

1. Read the snapshot: `cat results/decision_snapshots/$(date -u +%Y-%m-%d)-lag.txt` — per-engine table + fleet rollup names the offending symbols.
2. Cross-check: run `scripts/lag_summary.sh` interactively to see live state (snapshot is from cron-firing time).
3. If single symbol → likely upstream WebSocket issue for that pair; not fleet-wide. Engine will fall back to REST polling at 10s interval, which is the documented worst-case.
4. If multiple symbols → fleet-wide pattern. Check `https://www.binance.com/en/support/announcement` for Binance status; check VPS network with `ssh root@178.105.24.230 'mtr -c 10 fapi.binance.com'`.
5. HIGH tier (`lag_p99 > 30s`) — degradation is severe enough that real fills at Layer 2/STAGE_1 would diverge from modeled cost. Pre-promotion: do NOT promote until lag clears. Post-promotion: monitor realized cost trajectory closely.

### Scenario: drift_check fires HISTORY_CORRUPT (exit 5)

The wrapper detected ≥1 malformed line in `results/drift_check_history.jsonl`. The decision-grade two-firings-≥7d rule cannot be evaluated against a partially-corrupt history.

1. Inspect: `jq -c . results/drift_check_history.jsonl > /dev/null 2>&1; echo $?` — non-zero confirms corruption. `tail -20 results/drift_check_history.jsonl | jq -c .` to find the bad line.
2. Repair: either delete the malformed line OR replace it with a valid `{"ts":"...","exit_code":N,"verdict":"..."}` entry. Backup first: `cp results/drift_check_history.jsonl{,.bak.$(date -u +%F)}`.
3. Re-run: `scripts/run_drift_check.sh --quiet` should exit 0/1/2 (CLEAN/INVESTIGATION/INSUFFICIENT) post-repair — exit 5 means corruption remains.
4. The Telegram alert during the corruption window did NOT trip auto-kill — the wrapper suppresses AUTO-KILL when MALFORMED>0, by design. Decision-grade kill mechanism is paused until repair completes.

### Scenario: Engine crashed

1. CRITICAL Telegram should fire (panic recovery + Telegram alert in `cmd/engine`).
2. Systemd auto-restarts. Watch for restart loops (post_deploy_check section 12 catches; CRITICAL Telegram if persistent).
3. Journal-replay recovers any in-flight position on next start.
4. If recovery itself fails / drifts → STARTUP BLOCKED CRITICAL — operator must reconcile + restart manually.

---

## Don't-do list

Things that look like signals but aren't:

- **Low-n PnL within power floor.** At n<50 trades, the `forward_paper_resolution` script returns CONTINUE (Rule 3). Don't react to absolute PnL numbers; the math at n=9 is meaningless. **Single-trade WR = 0% or 100%; single-symbol concentration = 100%; drawdown = 100% of any loss.** All trip threshold gates spuriously without n_trades floor.
- **Threshold-only KILL verdicts.** `forward_paper_status.sh` may show KILL on advisory criteria (PnL, single-sym, HODL underperformance). Per kill-bar mis-calibration verdict 2026-05-07, these are advisory only. Cross-check drift detector before acting.
- **Backtest re-runs / strategy tuning mid-milestone.** Locked rule. Forbidden until forward-paper resolves.
- **Reshuffling deployed-32.** Adding/removing symbols mid-flight invalidates the locked symbol set.
- **Daily emotional reads.** Yesterday's losing trade doesn't predict next quarter. Bootstrap CI is wide.

---

## Quick reference: where the locks live

| Question | Locked rule |
|---|---|
| When does drift fire kill? | `drift_detector_calibration_verdict_2026-05-07.md` + `_time_to_detection_verdict_2026-05-08.md` |
| What's the kill execution sequence? | `auto_kill_execution_decision_rule_2026-05-08.md` |
| What's the staged real-money rollout? | `real_money_protocol_decision_rule_2026-05-08.md` |
| When can I promote to STAGE_1? | `forward_paper_outcome_resolution_decision_rule_2026-05-10.md` (LIMBO rule) |
| What's the Layer 3 acceptance criterion? | `real_money_executor_architecture_decision_rule_2026-05-08.md` |
| Why are threshold gates advisory? | `kill_bar_recal_verdict_2026-05-07.md` |
| Daily-loss cap per stage? | `real_money_protocol_decision_rule_2026-05-08.md` §Kill |

Master index: `results/INDEX.md`.

---

## VPS connection summary

```
Host:      178.105.24.230
SSH:       ssh root@178.105.24.230
Journals:  /var/log/paper-live/journal/
Logs:      /var/log/paper-live/<symbol>.log
Engines:   16 × paper-live@<symbol>.service (systemd template)
Drift cron (local): com.tradingengine.drift-check (launchd, weekly Sunday 09:00)
```

---

## When in doubt

1. Run `scripts/forward_paper_status.sh` — current state across all cohorts.
2. Run `python3 scripts/forward_paper_resolution.py` — single mechanical verdict.
3. Read the most recent `docs/findings/<YYYY-MM-DD>.md` — latest session notes.
4. Don't act under emotional pressure on n<50 data. **Patience is the key.**
