# Layer 3 enablement — capability pre-registration (2026-05-19)

**Status:** LOCKED 2026-05-19. Activates at deploy. Pure capability-build (Layer 3 gate operationalization), not a strategy decision.

## Context

Earlier today (2026-05-19, commit `3f18ed2`) the standalone testnet engine path was extended from solo-BTCUSDT to multi-symbol KAVAUSDT + ENSUSDT, replacing a 2-day-old single-symbol setup that had produced zero signals. Per `results/testnet_multi_symbol_extension_2026-05-19.md`, the goal was to accumulate real Binance testnet fill data faster.

Subsequent analysis identifies a finer-grained gap: **the standalone testnet engines do NOT satisfy the locked Layer 3 acceptance criterion.**

Per `results/real_money_executor_architecture_decision_rule_2026-05-08.md`, Layer 3 — Production shadow mode requires:

> "Before flipping any single engine to BinanceLive in production, run BinanceLive in PARALLEL on testnet alongside the existing Stub on the **same input ticks**. Run for ≥7 days. Diff the journals. Acceptance: pnl_usd per closed trade differs by ≤ 0.5%, no signal-generation divergence."

The keyword is **same input ticks**. Standalone testnet engines subscribe to their own WebSocket / REST stream independently from the live engine — small jitter in tick timing, REST poll boundaries, network latency. Even with byte-identical executor code, this independent-stream setup can produce >0.5% PnL drift on a single trade just from tick-timing differences.

`TeeExecutor` (already in `pkg/execution/tee.go`, wired through `cmd/engine` via `--layer3-binance-testnet-journal-dir`) solves this by fanning OnSignal/OnTick calls from a SINGLE engine to BOTH stub primary AND binance_live_testnet shadow. Both executors see identical inputs by construction.

## What this change does

1. Stop standalone testnet engines: `testnet-engine@kavausdt.service`, `testnet-engine@ensusdt.service` (deployed earlier today, ~3h old).
2. Add Layer 3 wrap to the LIVE engines for KAVAUSDT + ENSUSDT via systemd drop-in overrides — adds `--layer3-binance-testnet-journal-dir /var/log/paper-live/journal/layer3` + loads `/etc/paper-live/testnet-env` for credentials.
3. Restart KAVAUSDT + ENSUSDT live engines. Layer 3 wrap activates; both run with TeeExecutor (Stub primary + BinanceLive-testnet shadow on identical ticks).
4. Journal layout post-deploy:
   - Live stub primary: `/var/log/paper-live/journal/{KAVA,ENS}USDT-2026-05.jsonl` (UNCHANGED — same path live cohort always wrote to)
   - Layer 3 testnet shadow: `/var/log/paper-live/journal/layer3/{KAVA,ENS}USDT-2026-05.jsonl` (NEW)

## What this change does NOT do

- Does NOT change live cohort journals. The Stub primary writes to the same path as today — `forward_paper_status.sh`, drift detector, daily_digest, and all decision-grade gates continue reading the same `*-*.jsonl` root-level files. The journals' contents are unchanged because the Stub executor is unchanged.
- Does NOT add new tick subscriptions. The live engine already subscribes to KAVAUSDT/ENSUSDT WebSocket+REST. TeeExecutor lives in-process and consumes the existing tick stream — zero new mainnet weight.
- Does NOT use mainnet credentials. The shadow executor sends orders to `testnet.binancefuture.com` via separate `BINANCE_API_KEY/SECRET` from `/etc/paper-live/testnet-env` (locked by `cmd/engine` validation: layer3 + executor=stub combo refuses to start without testnet creds).
- Does NOT promote any shadow. Shadow runners (alt5-15-336, alt5-15-504, bb20) continue as stub-only. Layer 3 wraps ONLY the live strategy (P4-Combined).

## Rate-limit math (binding constraint)

| Cohort | Symbols | mainnet weight/min | testnet weight/min |
|---|---:|---:|---:|
| Live (16 symbols, REST polling) | 16 | 1920 | — |
| Standalone testnet (current) | 2 | 240 | low |
| **After: standalone testnet stopped** | -2 | -240 | -low |
| **After: Layer 3 wrap on 2 live engines** | (same 2 syms) | 0 (same tick stream) | ~10/min (reconciler poll 60s × 2 × 5 weight) |
| **Net delta** | 0 | -240 | -low + ~10 |
| **Total mainnet weight/min** | | **1920** (was 2160) | |

Layer 3 enablement **reduces** mainnet rate-limit pressure by 240/min by retiring redundant standalone testnet engines. Headroom grows from 240/min to 480/min.

Testnet-side weight is negligible (5w/call × 1 call/min × 2 engines = 10/min vs testnet's separate 2400/min pool).

## Why KAVAUSDT + ENSUSDT (continuity)

Same symbols selected earlier today for standalone testnet. Carries over the rationale: top observed signal rates in the live cohort (~0.3/d and ~0.2/d respectively). No new symbol decision; this is the same testnet capability re-wired into the correct architecture.

## Expected outcomes (timeline)

- **First Layer 3 PnL pair to diff:** ~3-5 days at observed signal rates (assumes KAVAUSDT or ENSUSDT fires).
- **7-day Layer 3 window evaluable:** earliest ~2026-05-26 (the `--min-days 7` gate fires once the OLDEST testnet open event has been on disk ≥7d).
- **Power to evaluate `pnl_usd ≤ 0.5% drift`:** any matched-pair diff is a verdict input. Threshold is per-trade. ~3-5 matched pairs = decision-grade.
- **Layer 3 PASS verdict pathway:** `scripts/layer3_verdict.sh --stub-dir /var/log/paper-live/journal --testnet-dir /var/log/paper-live/journal/layer3 --threshold-pct 0.5 --min-days 7`. Exit 0 = PASS, exit 1 = THRESHOLD breach, exit 2 = SIGNAL_DIV, exit 3 = INPUT_ERROR, exit 4 = INSUFFICIENT_DURATION.

## Verification gates (this deploy)

1. `systemctl status testnet-engine@kavausdt.service testnet-engine@ensusdt.service` after stop: `inactive (dead)`.
2. `systemctl status paper-live@kavausdt.service paper-live@ensusdt.service`: both `active (running)` with TeeExecutor wrap.
3. `grep "LAYER 3 SHADOW MODE ACTIVE" /var/log/paper-live/kavausdt.log /var/log/paper-live/ensusdt.log`: present (cmd/engine emits this WARN when --layer3 flag is set).
4. `ls /var/log/paper-live/journal/layer3/` directory exists with correct permissions.
5. Live cohort journals (`journal/KAVAUSDT-2026-05.jsonl`, `journal/ENSUSDT-2026-05.jsonl`) continue receiving heartbeat / open / close events as before — no schema or location change.
6. `scripts/post_deploy_check.sh` STRICT on live cohort: PASS (no regression).
7. `bash scripts/forward_paper_status.sh` continues to read live cohort path; reported live PnL/trades/days unchanged from immediately before deploy.
8. `cmd/engine` reads back from journal_replay successfully on restart (no orphan positions left behind by the executor swap).

## What signals abort

- Layer 3 wrap fails to start the engine (e.g., creds missing, JOURNAL_DIR perms, validateExecutorArgs trips) → revert by removing the drop-in override and restarting the unmodified live engine. Live engine recovery from journal preserves any open position.
- Live cohort journal location/contents change in any way → critical failure of the isolation invariant. Revert immediately.
- testnet POST /fapi/v1/order returns sustained 4xx/5xx (testnet outage, key invalidation) → BinanceLive Shadow degrades gracefully (errors logged, primary Stub unaffected). Investigate, do not panic.

## Reversal

Drop-in override file at `/etc/systemd/system/paper-live@kavausdt.service.d/layer3.conf` (and same for ensusdt). Remove via:

```bash
ssh root@178.105.24.230 'rm /etc/systemd/system/paper-live@{kavausdt,ensusdt}.service.d/layer3.conf && \
    systemctl daemon-reload && \
    systemctl restart paper-live@kavausdt.service paper-live@ensusdt.service'
```

Engine restart with journal-replay recovers any open Layer-3-era position. The Layer 3 shadow journal at `/var/log/paper-live/journal/layer3/` is left intact for forensic analysis.

## Audit reference

Consistent with:
- `real_money_executor_architecture_decision_rule_2026-05-08.md` — Layer 3 is the locked formal gate before STAGE_1; this is its operationalization on real engines.
- `forward_paper_completion_review_decision_rule_2026-05-08.md` — Layer 3 PASS is one of the conditions for the completion review GREEN.
- `testnet_multi_symbol_extension_2026-05-19.md` (earlier today) — standalone testnet was a stopgap; Layer 3 wrap is the formally-correct architecture for parity testing.

NOT a deviation from:
- "Don't add symbols" — same symbols stay; just changes their executor wiring.
- "Don't promote shadows" — Layer 3 wraps the LIVE strategy only; alt5-* and bb20 shadow runners untouched.
- "Don't tune parameters" — same `--signal-tf 4H --target-rr 6.0 --side-filter short --max-hold-hours 504`. The Layer 3 flag is additive.
- MONITORING mode — capability operationalization in preparation for the locked STAGE_1 gate.
