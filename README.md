# fin-trading-engine

A Go trading engine for Binance USDT-M Futures. It has a backtest harness, a paper-live trading mode, and a real-money executor.

> **Source of truth: [`CLAUDE.md`](./CLAUDE.md).** `CLAUDE.md` tracks the strategy, the deployment condition, the locked decision rules, the known bugs, the forward-paper validation plan, and the real-money promotion protocol. This README covers the code layout, the assemble and test commands, and the operate tooling. The two files are historical as of 2026-08-05.

## Status: closed (2026-08-05)

The project is closed. Nothing is active. The Hetzner VPS was removed on 2026-08-04. All 16 engines, all crons, and the local launchd timers are gone. The operator did not deploy money at all.

- **Outcome:** The forward-paper run stopped short at day 88 of approximately 112. The results: 134 trades, WR 17.9%, NET −$1,626.58. Costs were 114.2% of gross (gross +$11,420, fees $9,177, slip $3,870).
- **Why it closed:** Break-even was 8 bp. The run paid 10 bp. The run missed break-even by two basis points. This is a structural problem.
- **Revival paths:** The two paths closed negative on 2026-08-05. Maker entry: the fill rate passes (77.3%, n=2,882), but the fill distance between winners and losers is −7.1pp, z=−3.74. A pending short limit misses the trades that win. Wider stops: the actual fee cut is −65% (57/57 symbols), but this reaches only 0.86× break-even, which is negative on the deployed book.
- **Strategy-class search:** The search closed at N≈85 trials, PBO 0.52.

**To start a different quant project:** Read [`docs/QUANT_METHOD.md`](./docs/QUANT_METHOD.md). It gives the sequence of operations, the two checks that killed each candidate, the pre-registration discipline, and the traps. The portable code is [`scripts/quant_honesty.py`](./scripts/quant_honesty.py) (stdlib + numpy, `--selftest`).

To read the close-out results, start with [`NEXT_STEPS.md`](./NEXT_STEPS.md), then read `results/INDEX.md` → `## Close-out 2026-08-04 → 08-05`. The transferable lessons are in `results/v2_lessons_and_design_2026-08-04.md`. The screening rule that rejects each candidate this project ran is in `results/viability_frontier_2026-07-27.md`.

The run commands below continue to work against the Binance public API for backtests. But all deployment and monitoring instructions target infrastructure that no longer exists.

Caution: Do not use `scripts/run_drift_check.sh`. It writes to `drift_check_history.jsonl` and can make an incorrect auto-kill result against a closed book.

## Assemble and test

Prerequisites: Go 1.26.2 (or a version with toolchain auto-download), `jq`, `curl`, `unzip`.

```bash
go build ./...
go test ./...
```

The test suite includes strategy entry detection and the Stub paper executor. It also includes the BinanceLive real-money executor (mocked), the indicators, market-data ingestion, and funding accrual. It includes tests for notifications and the `journal_diff` and `journal_validate` operator tools.

## Run

```bash
# Single-month backtest
go run ./cmd/backtest --config configs/btcusdt.yaml

# Live engine (paper trading via Binance WebSocket — DEFAULT)
go run ./cmd/engine --config configs/btcusdt.yaml

# Live engine pointed at Binance USDT-M Futures TESTNET (Layer 2 gate)
BINANCE_API_KEY=... BINANCE_API_SECRET=... \
  go run ./cmd/engine --config configs/btcusdt.yaml --executor binance_live_testnet

# Live engine in REAL-MONEY mode (STAGE_1+ promotion ONLY; default is stub)
BINANCE_API_KEY=... BINANCE_API_SECRET=... \
  go run ./cmd/engine --config configs/btcusdt.yaml --executor binance_live
```

Read `CLAUDE.md` for the full set of locked CLI flags and the canonical strategy parameters.

## Architecture

The engine uses an event-driven pipeline. A single goroutine owns all mutable strategy data. There are no mutexes downstream of the tick fan-out.

```
Tick source (CSV replay or Binance WebSocket)
    │
    ▼ fan-out goroutine
    ├── aggTicks  ─→  Aggregator (5m / 30m / 4H candles)
    │                       │
    │                  candle channels
    │                       │
    └── stratTicks ─→ Runner (strategy event loop)
                          ├── BiasTracker   (4H → Long/Short/Neutral)
                          ├── VWAP          (session, resets 00:00 UTC)
                          ├── DailyLevels   (PDH/PDL, rolls at midnight)
                          └── EntryDetector (EMA crossover; absorption+breakout toggleable)
                                │
                                ▼
                          Executor (Stub paper-money | BinanceLive real-money | TeeExecutor for Layer 3)
```

`DataSource` is the only boundary between backtest and live. The two components, `CSVReplay` and `BinanceFutures`, give the same `Tick` stream. All downstream steps are the same.

## Project layout

```
cmd/
  backtest/         CSV-replay backtest harness
  dashboard/        Local forward-paper monitoring UI (localhost:8080; reads results/journal_cache)
  engine/           Live Binance Futures WebSocket entry point (--executor stub | binance_live_testnet | binance_live)
  journal_diff/     Layer 3 shadow-parity comparator (per-trade pnl tolerance ≤0.5%)
  journal_report/   Live-vs-backtest reconciliation
  journal_validate/ Self-consistency checker (open/close pairing, ts monotonicity, in-flight invariants)
  kill_switch/      Operator-driven Path C real-money close-all CLI (per auto_kill_execution_decision_rule)
pkg/
  models/           Tick, Candle, Signal, Direction, Timeframe
  marketdata/       DataSource interface, Binance WS + REST aggTrade fallback, CSV replay, heartbeat with startup grace
  aggregator/       Multi-timeframe candle builder
  indicators/       EMA, ATR, MACD, Bollinger, VWAP, DailyLevels
  strategy/         Bias, entry detection, runner, shadow-runner fan-out
  execution/        Stub (paper) + BinanceLive (real-money) + KillSwitch + PositionReconciler + journal-replay
  funding/          Constant + Historical funding-rate providers
  notify/           Telegram client with tier-aware retry + bot-token redaction
configs/            Per-symbol YAML configs + symbols.yaml deployed-list
deploy/             VPS sync, redeploy, launchd plists, systemd units
scripts/            Backtests, sweeps, paper-live orchestration, audits, drift detector wrapper, weekly_audit
results/            Pre-registration catalog (decision rules + verdicts), drift-check history, snapshots
docs/               findings/, plans/, DATA_RECOVERY.md
```

## Operate tooling

> **Historical. None of this works.** The VPS was removed on 2026-08-04. All `deploy/` and monitoring commands below do not work at the SSH step. Do not use `run_drift_check.sh`: it writes to `drift_check_history.jsonl` and can make an incorrect auto-kill result. This section is a record of how the operator used this tooling.

For the paper-live deploy:

```bash
# Sync code + rebuild binaries on VPS (no restart)
./deploy/sync.sh

# Sync + rebuild + restart one symbol (or all)
./deploy/redeploy.sh [SYMBOL|all]

# Post-deploy health audit — RUN AFTER EVERY redeploy.sh
./scripts/post_deploy_check.sh

# Forward-paper go/no-go status with R-multiple open positions + countdown to STAGE_1
./scripts/forward_paper_status.sh

# Drift detector (decision-grade kill mechanism)
./scripts/run_drift_check.sh

# Weekly audit (drift + snapshot + journal validate). Wired to launchd Sun 09:00 local
./scripts/weekly_audit.sh

# Journal self-consistency check
./bin/journal_validate --dir /var/log/paper-live/journal --exclude archive

# Mechanical PASS/FAIL of the locked promotion gates for any of the 4 stage transitions.
# Default is STAGE_0→STAGE_1 (the forward-paper resolution gate); promote
# to later stages via --from-stage STAGE_1|STAGE_2|STAGE_3 with the
# corresponding --stage-N-start ISO timestamps.
python3 scripts/stage_promotion_check.py
python3 scripts/stage_promotion_check.py --from-stage STAGE_1 --stage-1-start <iso>

# Mechanical KILL/CONTINUE of the locked kill criteria. Symmetric counterpart
# to stage_promotion_check; runs on the same journal data.
python3 scripts/kill_protocol_check.py
```

`post_deploy_check.sh` runs 13 audit sections. The sections check:

- engines active, watchdog armed, code in sync
- tick freshness, errors, rate-limit, recovery occurrences
- executor mode, funding staleness, disk space
- drift cron freshness, restart-loop, live-config compliance

It sends a Telegram WARN on STRICT-mode problems.

`forward_paper_status.sh` includes a drift-detector heartbeat. You can see a stopped weekly cron at each status check, not only at deploy time.

## Testing

```bash
go test ./...                                # full suite
go test ./pkg/execution/...                  # paper-money + real-money executor
go test ./pkg/strategy/...                   # entry detection, bias, fan-out
go test ./pkg/indicators/...                 # EMA/ATR/MACD/Bollinger/VWAP/DailyLevels
go test ./cmd/journal_validate/...           # journal invariant checks

# Python tooling test suite (mechanical evaluators + drift detector + helpers)
for f in scripts/test_*.py; do python3 "$f"; done

# Layer 2 testnet integration tests (skip cleanly without credentials)
BINANCE_TESTNET_API_KEY=... BINANCE_TESTNET_API_SECRET=... \
  go test ./pkg/execution -run Testnet_ReadOnly -v
```

CI (`.github/workflows/test.yml`) runs Go tests, Python tests, shellcheck (operate scripts at warning level, the full repo at error level), and ruff `--select F` on each push and PR. The same patterns that catch approximately half the historical fail-open bug class must complete on each future change.

## Known unmodeled risks

See `CLAUDE.md` section "Known unmodeled risks".

## License

See `LICENSE` or contact the repo owner.
