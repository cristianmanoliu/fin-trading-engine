# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

**Operator-facing reference:** see `docs/OPERATOR_HANDBOOK.md` for daily/weekly cadence, Telegram tier guide, scenario playbook (drift fires, kill protocol, promotion ready, recovery drift, real-money emergency kill), and tool map. CLAUDE.md is project-history dense; the handbook is the focused operational reference.

## Strategy status (2026-05-10)

**Live:** 16 paper-trading engines on Hetzner VPS, each running 1 live + 3 shadow strategies (alt5-15-336, alt5-15-504, bb20). Engines now have **journal-replay on startup** — restarts no longer orphan in-flight positions (commit `9eaeb54`, deployed 2026-05-07T20:24 UTC, 7 orphans recovered cleanly on first run).
- **Live config:** `--signal-tf 4H --side-filter short --target-rr 6.0 --max-hold-hours 504 --funding-csv-dir data/funding --fee-bps 10 --stop-slippage-bps 5` (EMA 9/21 hardcoded). Selected via 6-window walk-forward validation 2026-05-06.
- **Shadow A:** EMA 5/15 + mh336 (Cat A weak signal, forward A/B test).
- **Shadow B:** EMA 5/15 + mh504 (joint candidate).
- **Deployed shortlist:** 16 symbols in `configs/symbols.yaml:deployed`. Selection adds variance not edge (B3 cross-validation 2026-05-07 — no Bonferroni-significant feature distinguishes deployed from rejected). Universe-level: mean_price + mean_daily_vol_m predict annual NET (small-cap altcoins outperform); stages future Cat F2. See `results/b3_features_verdict_2026-05-07.md`. Operationally capped by Binance per-IP rate limit (16 × 10s × 20 weight = 1920/min, cap 2400) — see Bug 4.

**Validation status:** SUPPORTIVE walk-forward verdict (4H short EMA 9/21 mh504, 6 windows). Mean +$130k/yr with 95% CI [−$111k, +$372k] — **CI includes negative; uncertainty is irreducible from history alone.** Trade-level block bootstrap (5y × 16 sym × 2,210 trades, stationary bootstrap at L=√N=47) gives a much tighter CI [+$42k, +$220k]/yr, P(>$0)=99.9%, P(>$50k)=96.5% — but walk-forward CI is **2.8× wider**, confirming regime-variance (quarter-level) dominates trade-level autocorrelation (lag-1 ρ=+0.32). **Anchor expectations to walk-forward, not bootstrap** — bootstrap underestimates per-quarter regime swings. See `results/bootstrap_ci_verdict_2026-05-07.md`.

**Real-money allocation:** ZERO. BinanceLive executor code complete; promotion-ready. Gated on forward-paper validation (≥150 trades, ≥60 days net-positive — see `## Forward-paper go/no-go criteria`) PLUS Layer 2 testnet + Layer 3 7d shadow parity per `results/real_money_executor_architecture_decision_rule_2026-05-08.md`. Plumbing in place: Layer 2 via `--executor binance_live_testnet` (real prices, fake fills via `testnet.binancefuture.com`); Layer 3 via `--layer3-binance-testnet-journal-dir DIR` (TeeExecutor fans to stub primary + testnet shadow). After 7d, run `scripts/layer3_verdict.sh --stub-dir <stub> --testnet-dir DIR` for PASS/FAIL verdict (exit codes: 0 PASS / 1 THRESHOLD / 2 SIGNAL_DIV / 3 INPUT_ERROR / 4 INSUFFICIENT_DURATION; `--skip-min-days` for dry-run). Remaining operator action: generate testnet credentials + invoke.

## Recent session logs

See `docs/findings/<date>.md` per session. Latest: `2026-05-11.md` (19 commits — F1 run_drift_check corrupt-history → exit 5 / F2-F5 weekly_audit tier classifiers + UNEXPECTED branches / F6 lib/notify fail-loud unknown severity / F7 ssh BatchMode+ConnectTimeout sibling propagation across 22 callsites + lint-pinning / F8 journal_validate missing-fields / J1-J3 journal_diff scanner-err + corrupt-counter + symmetric glob / R1-R3 journal_report scanner-err + corrupt-counter + exit-3 harmonization / F12 panic-recover across 3 cmd binaries / PD-1 to PD-7 post_deploy_check ssh-vs-data disambiguation across 5 sections + md5 empty-empty equality / SD-1 deploy/sync.sh missing-funding silent skip / SD-2 deploy/install.sh env-required Telegram credentials (HIGH security — leaked token in source-current fixed; historical-git leak accepted-risk per operator decision since repo is private) / SD-3 deploy/install.sh engine-list drift fixed via symbols.yaml SoT. Bash test infra wired into CI — `scripts/test_*.sh` auto-discovered; 84+ assertions across 5 test files. Python sibling check on drift_check_history consumers REFUTED — already Mode-2 hardened at write time. **24 fail-opens closed in one session.** ssh-failure-vs-data-failure shape now confirmed in 6 paired implementations — firmly predictive); `2026-05-10-pm.md` (resume session, 9 commits — heartbeat 90→180s deployed, milestone-2 prep bundle, post_deploy_check >14d CRITICAL escalation, PROMOTE-closure template T2b, stage_promotion.sh T2a 6-phase orchestrator with 26 tests, AUDIT_LENS Mode 4 (writer-equals-model adjacent pattern), layer2_smoke.sh harness, self-audit follow-ups); `2026-05-10.md` (full-day record — morning: +10 trajectory + 4 fwd_paper_status + Layer 3 wrapper + 3 binance_live + LIMBO rule mechanical wiring; afternoon: +9 commits across handbook, cmd/engine 2nd-pass, Stub 1st-pass, marketdata 2nd-pass, strategy 1st-pass, kill_switch 1st-pass — critical-path audit complete across all 5 layers); `2026-05-09-pm.md` (stage_promotion_check + 13 fail-opens + CI lint expansion); `2026-05-08.md` (milestone-1 closure + drift detector + BinanceLive). **Cumulative across 8 sessions: 91 lens-applicable findings closed (84 missing-input + 4 lens-as-self-correction during T2a build + 3 lens-as-self-correction during 2026-05-11 morning). Pattern observation: silent-on-corrupt-input now confirmed in 5 paired implementations (BinanceLive 028e6a2 + Stub 4154374 + SignalContextWriter f760327 + run_drift_check 3b664f1 + journal_diff 3ee564b). ssh-failure-vs-data-failure dual-sense confirmed in 6 paired locations (weekly_audit + post_deploy_check ×5 sections) — both shapes are firmly predictive. Inverse confirmed: when the lens has been applied at write time (Python consumers of drift_check_history.jsonl), sibling checks correctly yield zero. New adjacent pattern documented 2026-05-10-pm: writer-equals-model (gate informationality during the phase the writer IS the model) — see `docs/AUDIT_LENS.md ## Adjacent pattern`.**

Pre-registration catalog: `results/INDEX.md` (55 docs: 28 decision rules + 22 verdicts + 5 syntheses; INDEX.md itself trails by ~7 entries — refresh when convenient).

## Build & Run

```bash
# Build
go build ./...

# Run backtest (single month)
go run ./cmd/backtest --config configs/btcusdt.yaml

# Override date without editing config
go run ./cmd/backtest --config configs/btcusdt.yaml --year 2024 --month 06

# Run live engine (paper trading via Binance WebSocket — DEFAULT)
go run ./cmd/engine --config configs/btcusdt.yaml

# Run live engine pointed at Binance USDT-M Futures TESTNET (Layer 2 gate;
# play-money fills, real production prices). Use SEPARATE testnet credentials
# from mainnet — generate at testnet.binancefuture.com.
BINANCE_API_KEY=... BINANCE_API_SECRET=... \
  go run ./cmd/engine --config configs/btcusdt.yaml --executor binance_live_testnet

# Run live engine in REAL-MONEY mode (STAGE_1+ promotion ONLY; default is stub).
# Requires Layer 2 (testnet) + Layer 3 (7d shadow parity) gates passed first
# per real_money_executor_architecture_decision_rule_2026-05-08.md.
BINANCE_API_KEY=... BINANCE_API_SECRET=... \
  go run ./cmd/engine --config configs/btcusdt.yaml --executor binance_live

# Emergency Path C real-money close-all (operator-driven only — see
# auto_kill_execution_decision_rule_2026-05-08.md). Dry-run by default;
# append CONFIRM to fire. CRITICAL Telegram alerts pre/post-fire.
BINANCE_API_KEY=... BINANCE_API_SECRET=... \
  go run ./cmd/kill_switch --reason drift_kill_<date> \
                           --positions "BTCUSDT,LONG,0.5;ETHUSDT,SHORT,2" CONFIRM

# Run all tests
go test ./...

# Run tests for a specific package
go test ./pkg/execution/...
go test ./pkg/marketdata/...
```

## Per-Symbol Configs

8 per-symbol YAMLs in `configs/` (BTC/ETH/BNB/SOL/XRP/LINK/LTC/DOGE) pin falsified Option C settings (`min_rr: 1.0, target_rr: 5.0, stake_usd: 1000`) — NOT migrated to P4-Combined. Candidate strategy runs via CLI overrides on `configs/default.yaml` (`--signal-tf 4H --target_rr 6.0 --side-filter short --max-hold-hours 336 --funding-csv-dir data/funding`). `run_backtest.sh` is legacy-wired; new P4-Combined sweeps use `scripts/run_p4_variant.sh` + `scripts/p4_oos_persistence.sh` (which build their own configs and skip the per-symbol YAMLs).

## Paper-Live (VPS)

The production run is on Hetzner CX23 at `178.105.24.230`. Use `deploy/` scripts to manage it.

```bash
# Sync code + rebuild + restart one symbol and tail log
./deploy/redeploy.sh             # btcusdt (default)
./deploy/redeploy.sh ethusdt     # specific symbol
./deploy/redeploy.sh all         # all 8, no tail

# Sync only (no restart)
./deploy/sync.sh

# Check all 8 engines
ssh root@178.105.24.230 'systemctl status "paper-live@*.service" --no-pager | grep -E "●|Active:"'

# Tail a log
ssh root@178.105.24.230 'tail -f /var/log/paper-live/btcusdt.log'

# View all trades (wins/losses/PnL summary) — passive monitoring
./scripts/paper_live_trades.sh root@178.105.24.230

# Run reconciliation (compares live journals vs backtest)
./scripts/paper_live_report.sh

# Post-deploy operational health audit — RUN AFTER EVERY redeploy.sh
./scripts/post_deploy_check.sh
```

**Post-deploy validation is mandatory.** After ANY `deploy/redeploy.sh`, run `scripts/post_deploy_check.sh`. 13 audit sections: engines active, watchdog timers, binary md5, tick freshness, ERROR logs, rate-limit, position recoveries, executor mode, funding-CSV staleness, disk/log size, drift cron freshness, restart-loop, live-config compliance. Sections 4/5/6 apply 5-min uptime gate. `STRICT=1` exits non-zero + fires Telegram WARN (CI-friendly).

**Local paper-live scripts** (for running locally without VPS):
```bash
./scripts/paper_live_start.sh   # builds bin/engine, launches 8 background processes
./scripts/paper_live_status.sh  # heartbeat count, last trade time
./scripts/paper_live_stop.sh    # graceful SIGTERM
```

Trade journals land in `/var/log/paper-live/journal/{SYMBOL}-YYYY-MM.jsonl` on VPS (or `./logs/journal/` locally). The engine backfills 96h of 1m klines from `fapi.binance.com` on startup (paginated across 4 calls — see Bug 5) to prime EMA21/BB20 indicators and `DailyLevels` before the first live signal evaluation.

## Backtest Scripts

```bash
# Single symbol, one year — auto-uses configs/{symbol}.yaml
./scripts/run_backtest.sh BTCUSDT 2024 01 12

# Multi-year heatmap for one symbol (compiles once, sweeps min_rr)
./scripts/full_analysis.sh BTCUSDT 2020 2025 04

# Find optimal min_rr across multiple instruments simultaneously
./scripts/cross_analysis.sh "BTCUSDT ETHUSDT SOLUSDT BNBUSDT" 2020 2025 04

# Validate all 8 instruments using their per-symbol configs (no sweep — fixed settings)
./scripts/validate_all.sh
```

**Script choice:**
- Exploring one instrument → `full_analysis.sh`
- Finding a universal min_rr → `cross_analysis.sh`
- Confirming final per-symbol settings → `validate_all.sh`
- Signal-level debugging → `run_backtest.sh`
- **Continuous 5y sweep with realistic costs (deployment-decision baseline) → `realistic_sweep.sh`**
- **Realistic target_rr sweep (parameter falsification with costs) → `realistic_targetrr_sweep.sh`**

`continuous_sweep.sh` is preserved for historical reproducibility but produces gross-only PnL. New deployment decisions should use `realistic_sweep.sh`.

## Architecture

Event-driven channel pipeline. A single goroutine owns all mutable strategy state — no mutexes anywhere.

```
Tick source (CSV or Binance WS)
    │
    ▼ fan-out goroutine
    ├──→ aggTicks ──→ Aggregator  (builds 5m/30m/4H candles)
    │                     │
    │              candle channels
    │                     │
    └──→ stratTicks ──→ Runner (strategy event loop)
                          ├── BiasTracker   (4H → Long/Short/Neutral)
                          ├── VWAP          (session, resets 00:00 UTC)
                          ├── DailyLevels   (PDH/PDL, rolls at midnight UTC)
                          └── EntryDetector (EMA9/EMA21 crossover; absorption+breakout toggleable)
                                │
                                ▼
                          Executor (paper Stub)
```

The `Runner.Run` select loop handles four channels: `candle4H`, `candle30m`, `candle5m`, `ticks`. Channels are set to `nil` when exhausted so the select naturally drops them without blocking.

## Strategy Logic

**Candidate strategy (P4-Combined, post-fees backtest leader):** EMA9×EMA21 crossover on the **4H** signal timeframe, with a **wick-based stop** and **fixed 6:1 R:R** take-profit. A bearish 4H EMA cross opens a SHORT (longs are filtered out via `--side-filter short`). Any open short older than **336 hours (14 days)** is force-closed at the current tick price. Funding cost is accrued from per-symbol historical Binance funding-rate CSVs (`data/funding/{SYMBOL}.csv`) — longs pay positive funding, shorts receive it; net aggregate over the 5y sample is roughly zero, not a benefit.

Cost-geometry rationale: on the 5m timeframe, wick stops are tight (~0.18% on BTC at p50) which forces ~500× implicit leverage to size each trade to a $1k stake, which makes round-trip taker fees eat the entire edge (Option C falsification). The 4H wick is wider, implicit leverage drops, fee per trade as a fraction of risked $ falls below the gross edge.

Two legacy entry types remain in code and are toggleable via `ema_mode: false` (neither is in the candidate or live path):

- **Absorption (reversal):** N consecutive 5m candles near a key level with wick/body ≥ `wick_ratio`, body closing away from the level. Allowed in any 4H bias.
- **Breakout (continuation):** 5m candle closes through a level with body/range ≥ `breakout_body_ratio`. Only taken aligned with 4H bias; blocked when bias is Neutral.

Key levels (used by absorption/breakout only): PDH, PDL, and optional manual `zones` from config.

## Telegram Notifier

`pkg/notify/telegram.go` — opt-in startup/shutdown alerts. Reads `TELEGRAM_BOT_TOKEN` and `TELEGRAM_CHAT_ID` from env (set in `/etc/paper-live/env` on VPS). When either is unset, `Send` is a no-op — same pattern as `JournalPath`. Test: `pkg/notify/telegram_test.go`.

`SendStructured` retries per tier: CRITICAL 3, WARN 2, INFO 1 (jittered exp backoff 1s→2s; 4xx skips retry). Final CRITICAL failure → `slog.Error` so post_deploy_check section 5 surfaces it. HTTP timeout 10s.

## Key Invariants

- **Exchange timestamps only.** `Candle` and `Tick` timestamps come from the exchange. `time.Now()` is never used for price data.
- **`DataSource` is the only seam between backtest and live.** `cmd/backtest` uses `CSVReplay`; `cmd/engine` uses `BinanceFutures`. Everything downstream is identical.
- **`expandKlineToTicks` is the canonical tick-expansion helper** (`pkg/marketdata/klines.go`). Both `CSVReplay` (backtest) and `BinanceFutures` (REST backfill on startup) call it so interpolation is identical in both paths. Do not inline a second copy.
- **`BinanceFutures` backfills 96h of 1m klines on startup, paginated across 4 calls** by fetching `GET /fapi/v1/klines?startTime=...&limit=1500` from `fapi.binance.com` before opening the WebSocket. Sized so 4H-signal indicators (EMA21, BB20) prime during backfill; without this, each restart cost ~64h of cold-start blindness — see Bug 5. Backfill failure (any page) is non-fatal — engine logs a warning and proceeds with whatever ticks were successfully pushed before the failure.
- **Live engine MUST call `Runner.SetLiveMode(true)`** to suppress signals from candles whose `CloseTime` is older than `backfillStaleness` (90s). Without this gate, paginated backfill (96h × 60 1m klines → 24 closed 4H candles) would prime EMA21 mid-backfill and emit a spurious "signal" at a stale historical close, opening a position at a price hours/days old. `cmd/backtest` leaves the gate off so historical CSV replay still produces signals — the gate is live-only.
- **Heartbeat startup grace.** `marketdata.Heartbeat.StartupGrace` suppresses Warn-level alerts for the first 3 min (= `wsReadDeadline × wsMaxStalls`, the WS→REST fallback boundary). Without it, every `redeploy.sh all` floods Telegram with ~16 stale-feed WARN that auto-clear. During grace, Warn snapshots downgrade to Info "warming up". Real stalled feeds alert post-grace.
- **Telegram bot token MUST NOT leak into logs.** Go's net/http embeds the request URL (containing the token) in transport errors verbatim. All slog + return paths in `pkg/notify/telegram.go` go through `Notifier.redactErr`. New err-logging code paths in this package MUST use it — `TestSendStructured_BotTokenNeverInSlog` enforces the contract.
- **Engine startup MUST call `Stub.RecoverFromJournal()` for live + each shadow** — bridges orphan-open gap on restart. No-op if `JournalPath` unset / position non-nil / no unclosed open. Reconstructs entry/stop/target/side/reason/ts from open event; `MaxAdverse`/`MaxFavorableR` reset to defaults (lossless for live config — no trail-stop/B2). Pre-open tick guard in `OnTick` (`tick.Timestamp < Signal.Timestamp` → skip) prevents backfill from spuriously closing recovered positions.
- **Signal-context sidecars (C2)** capture state at signal-emission for pattern-matching. Opt-in via `--signal-context-dir <path>` or `PAPER_LIVE_SIGNAL_CONTEXT_DIR`. Writes `<dir>/<label>/<SYMBOL>-<month>.jsonl`. Schema in `pkg/strategy/signal_context.go`. Written **after** filters pass — filtered signals not captured.
- **`Stub.JournalPath` opt-in — journal writes only happen in live mode.** `cmd/engine` sets `JournalPath`; `cmd/backtest` does not. When `JournalPath == ""`, `appendJournal` is a no-op. This preserves backtest behaviour byte-for-byte.
- **`stake_usd` must be set in config** for dollar PnL output. Without it, `total_pnl_usd` is not emitted and all `$1k` columns will show zero.
- **`julianDay`** (`pkg/indicators/vwap.go`) is the shared helper for detecting UTC day boundaries (used by both VWAP and DailyLevels). Returns `year*1000 + yearDay`.
- **Backfill channel buffer must exceed all backfill ticks.** `Subscribe` creates the tick channel before consumers start; backfill is synchronous and blocks on send if the buffer fills, so an undersized buffer hangs `Subscribe` indefinitely. The buffer is now sized dynamically: `backfillHours × 60 × 4 + 2048` (with a floor of 8192). At default `BackfillHours=96` that's 25,088 entries.
- **`http.DefaultClient` has no timeout — always use a custom client.** The backfill HTTP call uses `&http.Client{Timeout: 30 * time.Second}`. Without a timeout, a slow Binance response hangs `Subscribe` indefinitely with no log output.
- **`BinanceFutures` falls back to REST aggTrade polling when WebSocket stalls.** `fstream.binance.com` resolves globally to AWS Tokyo servers that accept WebSocket connections but deliver zero data frames. After `wsMaxStalls=2` consecutive 90s read timeouts (~3 minutes), `readLoop` switches to `aggTradeLoop` which polls `GET /fapi/v1/aggTrades?fromId=<lastID>` every 6 seconds. `fromId` pagination guarantees zero gaps and zero duplicates.
- **REST aggTrade rate limit: 10s × 16 sym × 20 weight = 1920/min (cap: 2400/min).** `GET /fapi/v1/aggTrades` is **20 weight/call** (not 1). Never reduce poll interval below 10s without recomputing. On 418/429, back off 60s and retry — **never exit the goroutine** (closes tick channel → "clean" shutdown → infinite systemd restart loop). See Bug 4.
- **Any new strategy or RR comparison must be backtested with `--exact-fills --include-boundary` before any live decision.** The default backtest exits at synthetic-tick wick prices, which overstates 1:1 RR strategies by enough to flip a 57-sym sweep from +$8M to −$4.7M (verified 2026-05-04). See `scripts/audit_phase4_btc.sh` for the reference harness.
- **Multi-year validation must use the continuous sweep, not monthly segments.** `scripts/continuous_sweep.sh` concatenates each symbol's 64 monthly CSVs and runs one Stub instance over the full 5y. Monthly-segmented sweeps force-close open positions at month-end last-known price (not at stop/target), inflating reported PnL by ~5% in aggregate (verified 2026-05-05: $7.09M segmented vs $6.77M continuous). The monthly approach is fine only for single-month debugging.
- **Realistic fee/slippage modeling is mandatory for deployment-decision backtests.** `Stub.FeeBps` (round-trip taker on entry notional `units × entry`) + `Stub.StopSlippageBps` (loss-side only); CLI: `--fee-bps`, `--stop-slippage-bps`. **Use `--fee-bps=10 --stop-slippage-bps=5`** (Binance USDT-M Regular: 0.05% × 2 sides = 10bp; 9bp with BNB discount). Use `scripts/realistic_sweep.sh` not `continuous_sweep.sh`. **Position notional, not stake, is the fee base** — at p50 BTC stop_dist 0.18%, $1k stake → $555k notional → $555 round-trip @ 10bp. Dominant realistic cost; this is what falsified Option C.

## Test Coverage

`go test ./...` runs ~25 packages. Highlights of what's covered:

| Package | Notable tests |
|---|---|
| `pkg/strategy` | EMA crossover (8 tests across primed/unprimed × bias × side-filter × fixed-RR target), funding-cross signals, shadow runner |
| `pkg/execution` | Stub: journal sink, fee/slip math both sides, funding accrual, summary aggregation, max-hold, trailing stop, multi-level TP, journal-replay (11 recovery cases incl. cross-month + corrupt-trailing-line + pre-open-tick guard). BinanceLive: constructor + endpoint defaults, OnSignal happy/drift/safety/reject paths, Layer 2 testnet integration tests (skip-by-default, 4 scenarios incl. round-trip + reconciler drift + KillSwitch) |
| `pkg/indicators` | EMA priming + numeric stability, ATR, Bollinger, MACD, DailyLevels day-roll at midnight UTC, VWAP session reset + year-boundary |
| `pkg/marketdata` | `expandKlineToTicks` invariants, heartbeat + startup-grace |
| `pkg/funding` | Constant + Historical providers, per-side sign, boundary exclusion, `LastTS` |
| `pkg/notify` | Telegram POST body + tier retries + bot-token redaction; no-op when env unset |
| `cmd/journal_diff` | Layer 3 parity comparator: pnl ≤0.5%, signal divergence, exit-code contract |

## Known Bugs

### Open

- `CSVReplay` double-close: stream goroutine defers `f.Close()` and `main.go` also calls `defer src.Close()`. Idempotent (second close returns harmless error). Skip.
- `run_backtest.sh` runs the binary twice per month (display + accumulate). Use `full_analysis.sh` for multi-month runs. Performance only.

### Fixed

Bugs 1–6 + pre-milestone fixes resolved. See commit history + `docs/findings/` for narratives.


## Forward-paper go/no-go criteria

Forward-paper validation started 2026-05-05 20:06 UTC (originally 32 Strategy B engines, now reduced to deployed-16). The original 32-list backtested at +$129k/yr at slip=25bp, but the train-only-shortlist diagnostic shows +70% look-ahead inflation at that slip level. **Anchor expectations to the honest annual: ≈ $69k/yr at slip=25bp**, not the deployed-claim or all-57 headline.

### Kill mechanism (decision-grade vs advisory) — IMPORTANT

Threshold-based criteria below are **advisory only** — Monte Carlo calibration (2026-05-07) found 30-40% FP under null, and no threshold can simultaneously meet FP(null)≤20% AND TP(dead)≥80% at 90d (statistical impossibility). See `results/kill_bar_recal_verdict_2026-05-07.md`.

**Decision-grade kill: `scripts/live_vs_backtest_drift.py`** — Welch t-tests + WR z-test vs backtest empirical distribution, Bonferroni-corrected. Operating point: α_family=0.001, N_LIVE=50 (FP=12.1%, TP=100% single-shot under any degradation).

**Cadence — weekly, NOT daily.** Daily sequential testing inflates FP to 28%/yr. Median detection day = 26d under any degradation. Operational rule:

- Run weekly or per-N (every ~50 new trades). Single firing = investigation-grade.
- **Two firings 7+ days apart, OR drift + threshold match** = auto-kill candidate.
- Verdict: `results/drift_detector_time_to_detection_verdict_2026-05-08.md` (NEEDS_TUNING flagged but locked).

**Canonical invocation: `scripts/run_drift_check.sh`** — wraps detector, persists to `results/drift_check_history.jsonl` + log, evaluates two-firings rule. Exit codes: 0 CLEAN / 1 INVESTIGATION / 2 INSUFFICIENT / 3 ERROR / 4 AUTO-KILL CANDIDATE. Use `--quiet` for cron. Don't invoke detector directly — bypasses history index.

**Scheduled via launchd.** `deploy/drift-check.launchd.plist` (weekly Sunday 09:00 local). Install: `cp deploy/drift-check.launchd.plist ~/Library/LaunchAgents/com.tradingengine.drift-check.plist && launchctl load -w ~/Library/LaunchAgents/com.tradingengine.drift-check.plist`. Inspect: `launchctl list | grep tradingengine`. Logs at `results/drift_runs/launchd.{out,err}.log`. Uses `--live-source vps` so passwordless SSH-to-VPS required.

**The plist invokes `scripts/weekly_audit.sh`** — 6 stages each Sunday: (1) drift check, (2) `forward_paper_status.sh` snapshot to `results/forward_paper_snapshots/<date>.txt`, (3) SSH-runs `cmd/journal_validate` (Telegram CRITICAL on errors). Stages 2-3 non-fatal; wrapper propagates drift exit code. `--no-snapshot` skips stage 2.

**`cmd/journal_validate`** — self-consistency checker, groups by (cohort, symbol) for cross-month closes. Tracks in-flight opens; TARGET/STOP/TIME decrement, PARTIAL doesn't (B2). Invariants: monotonic ts, no dup opens, no close-without-open, no open-while-in-flight. Exit codes: 0 CLEAN / 1 WARN / 2 ERROR / 3 USAGE. `--exclude archive` skips frozen pre-Bug-6 dirs.

**Telegram alerts on drift fire.** Wrapper POSTs on codes 1/3 (WARN) and 4 (CRITICAL). Same `TELEGRAM_BOT_TOKEN`/`TELEGRAM_CHAT_ID` env vars; missing → silent no-op. Codes 0/2 do NOT alert (avoid desensitization). Add env vars to plist's `EnvironmentVariables` for launchd context.

Always cross-reference threshold fires against the drift detector before killing. Full calibration record: `results/drift_detector_calibration_verdict_2026-05-07.md`.

### Statistical-power floor (before reading any signal)

P4-Combined backtests at WR 20.64% with 6:1 R:R. Breakeven WR ≈ 14.3% — only ~6.3pp cushion. To bound observed WR within ±10pp at 95% CI requires ~63 trades; ±5pp requires ~250 trades. Strategy B at 5,809/5y × 32/57 sym ≈ 1.8 trades/day → 60 days ≈ 108 trades = ~±7pp resolution. **Do not draw conclusions from < 60 calendar days of forward data.**

### Deploy real money (small tranche, 1/10th notional) only if ALL true

Note: the "≥150 trades AND ≥60 days" gate is internally inconsistent at the historical fleet trade rate of ~1.18 trades/day. 60 days yields ~70 trades on average; ~127 days are needed to hit 150 trades. Surfaced in kill-bar calibration 2026-05-07. The two thresholds collide; apply both literally (whichever comes second).

- ≥150 live trades accumulated AND ≥60 calendar days net-positive in dollar terms
- Realized round-trip taker fees ≤ 12 bp (vs 10 bp modeled — 20% slack). The journal cost-decomposition schema (commit `7939786`, 2026-05-07) writes `fee_usd`/`slip_usd`/`notional_usd` per close so this is directly evaluable in `forward_paper_status.sh` AND `realized_cost_trajectory.py`. **Paper-mode caveat (cost-trajectory run 2026-05-10):** during forward-paper the realized values are produced by the Stub executor's flat-rate `FeeBps`/`StopSlippageBps`, so the criterion is by-construction PASS at the modeled values (10.00 / 5.00) with zero variance. The plumbing works end-to-end (verified n=15), but the criterion only carries divergence signal when fills come from Layer 2 testnet (`--executor binance_live_testnet` writes real Binance fee/slip into the same schema) or STAGE_1+ real money. Treat the gate as PLUMBING-only during paper, INFORMATIONAL once Layer 2 lights up.
- Realized stop-side slippage ≤ 20 bp on the losing-trade subsample (vs 5-25 bp modeled range). Same paper-mode caveat as above — Stub computes slip from `StopSlippageBps` only on losers; the value is the model.
- Live PnL ≥ 60% of pro-rated honest-annual ($69k/yr × elapsed-fraction × 0.60)
- Live PnL beats `BTC HODL with $32k notional` over the same window
- No single symbol contributes >40% of cumulative live PnL
- **`scripts/live_vs_backtest_drift.py` returns exit code 0** (no Bonferroni-significant divergence at α_family=0.001) — this is the decision-grade gate; the threshold-based criteria above are advisory only

### Kill the strategy (advisory triggers — confirm via drift detector before acting)

These are NOT auto-kills. The threshold-based criteria are mis-calibrated (per kill-bar calibration 2026-05-07). Treat each as an **investigation trigger**: when one fires, run `scripts/live_vs_backtest_drift.py`; if THAT returns exit code 1 (decision-grade drift), kill. If drift detector is clean, the threshold fire is more likely sampling variance than strategy degradation.

- First 60 days net-negative
- Realized stop-side slippage > 25 bp (historical-rule cliff edge — but A2 sweep 2026-05-07 shows the actual breakeven is at ~81bp; slip-cost is linear at −$1.71k/yr per bp with no nonlinear cliff. See `results/slip_cliff_verdict_2026-05-07.md`)
- Realized WR < 14% over ≥150 trades (below breakeven; calibration found this rarely fires under any scenario — useful but low-power)
- Single symbol contributes >40% of live PnL (calibration found this fires in 41% of healthy windows; treat with skepticism)
- Train-only-shortlist diagnostic re-run on rolling forward data shows non-positive honest test
- Two consecutive 30-day windows underperform BTC-HODL benchmark by >$5k each
- **Drift detector returns exit code 1** at α_family=0.001 — this IS a decision-grade kill, no further confirmation needed

### Initial real-money sizing

The full staged deployment protocol is pre-registered at
`results/real_money_protocol_decision_rule_2026-05-08.md` (locked
2026-05-08, before forward-paper validation completes — applies
mechanically when forward-paper resolves).

Summary:
- **STAGE_1**: $100/trade after forward-paper deploy criteria + 30d clean drift detector
- **STAGE_2**: $300/trade after ≥50 STAGE_1 trades + 30d + slip stable
- **STAGE_3**: $500/trade after ≥100 cumulative + 60d at STAGE_2 + slip/drift stable
- **STAGE_4**: $1,000/trade after ≥200 cumulative + 90d at STAGE_3

Earliest STAGE_4 reach from forward-paper start (~2026-05-05): approximately 2027-03-09.

Locked kill criteria fire at any stage (STOP entire protocol, no auto-resumption within milestone): confirmed drift firing (two 7+ days apart or drift+threshold match), realized slip >30bp sustained 30 trades, 3 consecutive days each net-loss >5× stake, single-symbol >50% PnL, unrecoverable engine/exchange error, 20% drawdown over 60d window.

When forward-paper resolves: read the pre-reg file. The promotion or kill decision is a mechanical rule application, no design choices remaining.

### Things to NOT do during forward-paper

- Don't reshuffle the deployed list mid-flight. Look-ahead is in backtest test_NET, not forward data.
- Don't promote to Strategy A pre-emptively (Strategy A vs B is a forward-paper question; backtest difference was inside the noise floor).
- Don't add symbols. Trade-count throughput is currently 1.8/day; adding symbols increases REST-poll load against the 2400 weight/min Binance Regular cap.
- Don't tune target_rr, signal_tf, or side-filter. Every additional sweep cell consumes statistical degrees of freedom you've already spent.
- Don't read into wins/losses inside the 60-day power floor. The natural shorts-only hit-rate is 20.6% — variance is enormous at low n.

## Historical strategies (kept for grep — do not decide from)

- **Option C** (5m EMA9×21, target_rr=5.0) — falsified 2026-05-05 at realistic costs (NET −$143.76M, 0/57 profitable; pre-fees was +$6.77M, a fee-illusion edge). VPS migrated off 2026-05-07.
- **Absorption + breakout** (PDH/PDL, min_rr=1.0) — original strategy, superseded 2026-05-04. Toggleable via `ema_mode: false`.

## Known unmodeled risks

Original 13 caveats reduced to 3 still-open after 2026-05-05 cost-survivor battery + follow-ups:

- **REST polling lag (10s)** — adverse on entry + stop. **Source-to-receipt lag is now instrumented per-tick** (commit `033ed02`, 2026-05-11): `models.Tick.LocalReceiptTS` set by `BinanceFutures` at receipt; `Heartbeat` keeps a 256-sample rolling ring; per-symbol heartbeat output now carries `lag_p50_ms` / `lag_p99_ms` / `lag_max_ms` / `lag_samples`. Surfaces: `scripts/post_deploy_check.sh §4` shows per-engine lag_p99 column + fleet-level DEGRADED/HIGH warns (commit `58fd906`); `scripts/lag_summary.sh` standalone aggregator with per-engine table + fleet verdict + locked exit-code contract for cron use (commit `bb53c1b`). Tier thresholds: ≤1000ms OK (WS-dominant) / ≤15000ms TYPICAL (REST or mixed) / ≤30000ms DEGRADED (investigate) / >30000ms HIGH (severe). Realized fill-vs-modeled-slip still requires Layer 2 (BinanceLive cost-decomp) to characterize.
- **Funding-CSV staleness drift** — picked up only on restart; weekly refresh caps drift at ~7d. Mitigated 2026-05-08: `cmd/engine` logs `slog.Warn` at startup if `LastTS()` >7d old; `post_deploy_check.sh` §9 surfaces it. Net funding ≈ $0 in steady state.
- **No real-money execution test** — position sizing, exchange limits, margin reuse, concurrent-trade interaction unmodeled. Honest backtest projection $69–184k/yr depending on slip.

