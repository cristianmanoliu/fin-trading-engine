# NEXT_STEPS — P4-Combined forward-paper validation, 2026-05-05 onward

> **Supersedes the 2026-05-05 morning version** (which planned a 30-day quiet period on the 11-engine Option C deploy). Option C was falsified at realistic costs the same evening (NET −$143.76M, 0/57 profitable — `results/option_c_targetrr_realistic_2026-05-05.txt`); the VPS migrated to P4-Combined on 2026-05-07. The old document is preserved in git history.

## Where we are

**Live:** 16 paper-trading engines on Hetzner VPS `root@178.105.24.230`. Each engine runs **P4-Combined** as the live strategy + 3 shadow strategies (alt5-15-336, alt5-15-504, bb20). Live config — pinned, do not change:

```
--signal-tf 4H --side-filter short --target-rr 6.0 --max-hold-hours 504
--funding-csv-dir data/funding --fee-bps 10 --stop-slippage-bps 5
```

EMA 9/21 hardcoded. Wick stop, 6:1 fixed RR.

**Deployed symbols (`configs/symbols.yaml:deployed`, single source of truth):** 1000SHIBUSDT, 1INCHUSDT, ADAUSDT, APTUSDT, AVAXUSDT, BCHUSDT, DOTUSDT, ENSUSDT, ETCUSDT, FILUSDT, GRTUSDT, IMXUSDT, KAVAUSDT, ROSEUSDT, RUNEUSDT, XLMUSDT.

**Forward-paper started:** 2026-05-05 20:06 UTC. Real-money allocation = ZERO.

**Honest annual expectation (anchor here):** walk-forward CI [−$111k, +$372k], mean +$130k/yr. Pro-rated honest annual ≈ $69k/yr at slip=25bp. Bootstrap CI [+$42k, +$220k] is tighter but underestimates per-quarter regime variance — do not anchor to it.

## The dual-gate timeline

The promotion criterion is **≥150 live trades AND ≥60 calendar days net-positive**. At the historical fleet rate of ~1.18 trades/day, these collide:

| Threshold | Hits at | Calendar date |
|---|---|---|
| 60 calendar days | Day 60 | **2026-07-04** |
| 150 trades (~1.18/day) | Day 127 | **~2026-09-09** |
| **Effective dual-gate (whichever-second)** | Day 127 | **~2026-09-09** |

So the real "decision day" is roughly **2026-09-09**, not Day 60. Plan accordingly.

Earliest STAGE_4 ($1k/trade) reach per pre-reg protocol: ~2027-03-09.

## Calendar reminders to set NOW

- **2026-05-12 (Day 7)** — first weekly drift check (also auto-runs via launchd Sunday 09:00). Validate launchd job actually fired: `launchctl list | grep tradingengine` and check `results/drift_runs/launchd.{out,err}.log`.
- **2026-06-04 (Day 30)** — first material checkpoint. Statistically thin (~35 trades at fleet rate) but meaningful PnL signal. Compare per-symbol live PnL vs backtest for the same window.
- **2026-07-04 (Day 60)** — calendar gate satisfied. Trade-count gate still ~67 days out.
- **~2026-09-09 (Day 127)** — projected dual-gate satisfaction. Real promotion decision; apply `results/real_money_protocol_decision_rule_2026-05-08.md` mechanically.

## What to do during the wait

### Daily (1 minute)

```bash
ssh root@178.105.24.230 'systemctl status "paper-live@*.service" --no-pager | grep Active' | grep -v active
```

No output = healthy. Any output = investigate that one engine, leave the rest alone.

### Weekly (Sundays — 10 min)

The `weekly_audit.sh` wrapper runs automatically via launchd. Manually verify by reviewing:

```bash
# Fresh drift check (decision-grade kill signal)
./scripts/run_drift_check.sh

# Forward-paper status snapshot
./scripts/forward_paper_status.sh

# VPS journal validation
ssh root@178.105.24.230 './bin/journal_validate /var/log/paper-live/journal/'
```

**Drift detector exit codes that matter:**
- `0` clean — continue
- `1` investigation-grade firing — investigate; if a second firing follows ≥7 days later, **kill**
- `2` insufficient data — keep waiting
- `4` auto-kill candidate — already two firings 7+ days apart; halt the fleet

### Monthly (Day 30, 60, 90 — 30 min)

Read `results/forward_paper_snapshots/<latest>.txt`. Apply the decision matrix in CLAUDE.md "Day 30" / "Day 60" sections. Don't make symbol-rotation decisions before Day 60.

## Active operator work (does NOT violate "no tinkering")

These are the only **forward-motion** items permitted during forward-paper. Do them in order; each unblocks the next.

### 1. Layer 2 testnet kickoff (this week)

Plumbing complete; needs operator action:

1. Generate testnet credentials at https://testnet.binancefuture.com (separate from mainnet keys).
2. Set `BINANCE_API_KEY` / `BINANCE_API_SECRET` env vars.
3. Run a single symbol via `--executor binance_live_testnet`. Confirm round-trip fills land in journal.
4. Verify with the testnet integration tests in `pkg/execution/binance_live_test.go`.

Why now: Layer 2 must precede Layer 3, and Layer 3 needs 7 days of observation. Don't be on the critical path when forward-paper resolves.

### 2. Layer 3 shadow parity (target start ~2026-08-25, before Day 127)

Run TeeExecutor (stub primary + testnet shadow) for ≥7 days via `--layer3-binance-testnet-journal-dir DIR`, then:

```bash
./scripts/layer3_verdict.sh --stub-dir <stub> --testnet-dir DIR
```

Exit codes: 0 PASS / 1 THRESHOLD / 2 SIGNAL_DIV / 3 INPUT_ERROR / 4 INSUFFICIENT_DURATION. PASS is required to enter STAGE_1. Use `--skip-min-days` for a dry-run before the real 7-day window.

### 3. Continued audit-fix sweep (ongoing)

The 2026-05-10 session closed 9 fail-open / silent-failure fixes across 5 layers. Cumulative: 57 of this shape across 6 sessions. The "by the 3rd instance the lens is predictive" observation suggests more remain — but the critical-path audit is complete. Treat new findings as opportunistic, not scheduled.

## Things to NOT do (the deal with yourself)

For the next ~120 days you do NOT:

- Change `target_rr`, `signal_tf`, `side-filter`, `max-hold-hours`, `fee-bps`, or `stop-slippage-bps`
- Add or remove symbols from `configs/symbols.yaml:deployed` (currently 16; REST budget caps near 16-20 anyway at 10s aggTrade poll × 20 weight)
- Modify code in `pkg/strategy/`, `pkg/execution/`, or `pkg/aggregator/`
- Run additional sweeps "to check" something — every cell consumes statistical degrees of freedom already spent
- Promote Strategy A pre-emptively from shadow data (A vs B is a forward-paper question; backtest difference was inside the noise floor)
- Read into wins/losses inside the 60-day power floor (natural shorts-only WR is 20.6%, σ is enormous at low n)

You DO:
- Watch logs for engine deaths or error spam
- Run weekly reconciliation (auto via launchd)
- Layer 2 → Layer 3 plumbing per above
- Bug fixes (explicitly allowed — actual bugs only, not "could be better")

## Stopping rules

**Decision-grade (auto-kill):**
- Drift detector exit code `4` (two firings ≥7 days apart) → `ssh root@178.105.24.230 'systemctl stop "paper-live@*.service"'`, then investigate
- Drift detector exit code `1` + a separate threshold criterion firing concurrently → same

**Investigation triggers (run drift detector to confirm before acting):**
- First 60 days net-negative
- Realized stop-side slippage > 25bp (note: A2 sweep 2026-05-07 puts the actual breakeven cliff at ~81bp, linear in between)
- Realized WR < 14% over ≥150 trades (below breakeven; rarely fires under any scenario)
- Single symbol > 40% of cumulative live PnL (calibration: fires in 41% of healthy windows — high false-positive)
- Two consecutive 30-day windows underperform BTC-HODL benchmark by >$5k each

The full operator scenarios — drift fires, kill protocol, recovery drift, real-money emergency kill — are in `docs/OPERATOR_HANDBOOK.md`. Use it, not this doc, when something is on fire.

## Pre-registered protocols (locked, mechanical application when forward-paper resolves)

- `results/real_money_protocol_decision_rule_2026-05-08.md` — STAGE_1→4 sizing
- `results/real_money_executor_architecture_decision_rule_2026-05-08.md` — Layer 2/3 gate definitions
- `results/auto_kill_execution_decision_rule_2026-05-08.md` — Path C close-all
- `results/INDEX.md` — full pre-reg catalog (53 decision rules + verdicts + syntheses)

When forward-paper resolves on ~2026-09-09: read the pre-reg files. Promotion or kill is a mechanical rule application, no design choices remaining.

## Roadmap (NOT urgent — do not let these sneak into the quiet period)

- **Regime detector** — cross-asset realized vol, BTC dominance, etc.; pause engines in ranging conditions. Aspirational.
- **Per-symbol stopping rule built into the engine** — auto-halt a symbol if 30-day rolling PnL < threshold. Aspirational.
- **Add a few more symbols** (HBARUSDT, BNBUSDT, IOTAUSDT — GRTUSDT already deployed; TRXUSDT excluded as persistent OOS loser per `configs/symbols.yaml:persistent_losers`) up to the REST cap of ~16-20, **only after Day 60 if performance looks healthy**. Use absolute thresholds for any rotation, never within-window rank.

**Code hygiene (non-PnL):**
- `pkg/strategy/entry.go` — `MinRR` filter silently ignored in EMA mode. Apply for consistency.
- `pkg/aggregator/aggregator.go` — at 4H boundaries, 5m candle is processed before 4H bias updates (deflationary, minor).
- `pkg/execution/stub.go:261` — journal close timestamp uses `time.Now()` instead of tick timestamp. Cosmetic for live; backtest unaffected.
- `CSVReplay` double-close (defer + main.go defer). Harmless but ugly.
- `go mod tidy`.

## The single most important sentence in this document

**The work this period is patience and protocol adherence. The real test is not whether the strategy is profitable — it's whether you can leave a working hypothesis alone for ~120 days while the data accumulates. If you find yourself reading this doc looking for permission to tinker, the answer is no.**
