# fin-trading-engine

A Go-based algorithmic trading engine for Binance USDT-M Futures. Backtesting + paper-live engine + (gated) real-money executor.

> **Operational source of truth: [`CLAUDE.md`](./CLAUDE.md).** It tracks the current strategy, deployment state, locked decision rules, known bugs, the forward-paper validation plan, and the staged real-money promotion protocol. This README covers the project surface — code layout, build/test commands, and operational tooling. For the *current* strategy / running state / open positions / next gate, read CLAUDE.md.

## Status (2026-05 forward-paper window)

- **Paper-live deploy:** 16 engines on a Hetzner VPS, each running 1 live + 3 shadow strategy variants per symbol.
- **Live strategy:** `--signal-tf 4H --side-filter short --target-rr 6.0 --max-hold-hours 504`. EMA 9/21 cross. Selected via 6-window walk-forward validation.
- **Real-money allocation:** ZERO. The `binance_live` executor is code-complete but gated behind forward-paper validation (≥150 trades, ≥60 days net-positive) plus pre-registered Layer 2 (testnet) + Layer 3 (7d shadow parity) gates per `results/real_money_executor_architecture_decision_rule_2026-05-08.md`.
- **Decision-grade kill mechanism:** `scripts/run_drift_check.sh` (Welch t-test on Bonferroni-corrected metrics; locked at α=0.001). Wired to a weekly launchd cron with Telegram alerts on fire.

## Build & test

Prerequisites: Go 1.26.2 (or any version with toolchain auto-download), `jq`, `curl`, `unzip`.

```bash
go build ./...
go test ./...
```

Test suite covers strategy entry detection, executor (Stub paper + BinanceLive real-money mocked), indicators, market-data ingestion, funding accrual, notifications, and the journal-diff / journal-validate operator tools.

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

See `CLAUDE.md` for the full set of locked CLI flags expected on the paper-live deploy and the canonical strategy parameters.

## Architecture

Event-driven pipeline. A single goroutine owns all mutable strategy state — no mutexes anywhere downstream of the tick fan-out.

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

`DataSource` is the only seam between backtest and live. Both `CSVReplay` and `BinanceFutures` produce the same `Tick` stream — everything downstream is identical.

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

## Operational tooling

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

`post_deploy_check.sh` runs 13 audit sections (engines active / watchdog armed / code in sync / tick freshness / errors / rate-limit / recovery events / executor mode / funding staleness / disk space / drift cron freshness / restart-loop / live-config compliance) and surfaces Telegram WARN on STRICT-mode failures.

`forward_paper_status.sh` includes a drift-detector heartbeat section so a silently-stopped weekly cron surfaces at every status check (operator's most frequent natural touchpoint), not just at deploy time.

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

**CI** (`.github/workflows/test.yml`) runs Go test, Python test, plus
shellcheck (operational scripts strict at warning severity; whole repo
at error severity) and ruff `--select F` (pyflakes ruleset) on every
push/PR. Multiplicative payoff — the same patterns that catch about
half of the historical fail-open bug class are now blocking on every
future change.

## Known unmodeled risks

See `CLAUDE.md` § "Known unmodeled risks" for the current list. The forward-paper window is the test that resolves the remaining ones.

## License

See `LICENSE` (if present) or contact the repo owner.
