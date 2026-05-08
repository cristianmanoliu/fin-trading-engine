# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Strategy status (2026-05-07 EOS)

**Live:** 16 paper-trading engines on Hetzner VPS, each running 1 live + 3 shadow strategies (alt5-15-336, alt5-15-504, bb20). Engines now have **journal-replay on startup** — restarts no longer orphan in-flight positions (commit `9eaeb54`, deployed 2026-05-07T20:24 UTC, 7 orphans recovered cleanly on first run).
- **Live config:** `--signal-tf 4H --side-filter short --target-rr 6.0 --max-hold-hours 504 --funding-csv-dir data/funding --fee-bps 10 --stop-slippage-bps 5` (EMA 9/21 hardcoded). Selected via 6-window walk-forward validation 2026-05-06.
- **Shadow A:** EMA 5/15 + mh336 (Cat A weak signal, forward A/B test).
- **Shadow B:** EMA 5/15 + mh504 (joint candidate).
- **Deployed shortlist:** 16 symbols in `configs/symbols.yaml:deployed`. Selection adds variance not edge per mechanism analysis (re-confirmed in B3 cross-validation 2026-05-07 — no feature distinguishes deployed from rejected at Bonferroni). However the universe IS heterogeneous: cross-universe correlation finds **mean_price (ρ=−0.435) and mean_daily_vol_m (ρ=−0.424) Bonferroni-significant predictors of annual NET** — small-cap, low-liquidity altcoins outperform on this strategy. Stages a future Cat F2 mechanism-grounded selection rule. See `results/b3_features_verdict_2026-05-07.md`. Operationally constrained by Binance per-IP rate limit (16 × 10s polling × 20 weight/call = 1920 weight/min, cap 2400). Bumped from 6s on 2026-05-07 — see Bug 4.

**Validation status:** SUPPORTIVE walk-forward verdict (4H short EMA 9/21 mh504, 6 windows). Mean +$130k/yr with 95% CI [−$111k, +$372k] — **CI includes negative; uncertainty is irreducible from history alone.** Trade-level block bootstrap (5y × 16 sym × 2,210 trades, stationary bootstrap at L=√N=47) gives a much tighter CI [+$42k, +$220k]/yr, P(>$0)=99.9%, P(>$50k)=96.5% — but walk-forward CI is **2.8× wider**, confirming regime-variance (quarter-level) dominates trade-level autocorrelation (lag-1 ρ=+0.32). **Anchor expectations to walk-forward, not bootstrap** — bootstrap underestimates per-quarter regime swings. See `results/bootstrap_ci_verdict_2026-05-07.md`.

**Real-money allocation:** ZERO. BinanceLive executor code complete; Go binary is promotion-ready. Gated on forward-paper validation (≥150 trades, ≥60 days net-positive, see `## Forward-paper go/no-go criteria`) PLUS Layer 2 testnet integration + Layer 3 7-day shadow parity per `results/real_money_executor_architecture_decision_rule_2026-05-08.md`. Both gates have plumbing in place as of 2026-05-08: **Layer 2** via `--executor binance_live_testnet` (swaps OrderRouter + PositionReconciler to `testnet.binancefuture.com`; price feeds stay on production fapi — locked contract is "real prices, fake fills"). **Layer 3** via `--layer3-binance-testnet-journal-dir DIR` (wraps the live Stub in a `TeeExecutor` that fans signals/ticks to BOTH stub primary AND a BinanceLiveTestnet shadow journaling to DIR — single engine, single subscription, identical ticks). After ≥7d, operator runs `cmd/journal_diff --dir-a <stub-journal> --dir-b DIR` to validate the locked 0.5% per-trade pnl tolerance. Operator action remaining for both: generate testnet credentials + invoke.

## Recent session logs

- `docs/findings/2026-05-08.md` — combined 2026-05-07/08 session: milestone-1 backtest investigation closure (5-fold strategy validation, F1×F2 mechanism class rejected, drift detector deployed at α=0.001, journal-replay shipped), operational hardening pass (forward_paper_status.sh / run_drift_check.sh / 9 defensive test suites / 10 pre-regs), and BinanceLive executor implementation (5 commits, ~75 tests, promotion-ready pending Layer 2/3 operational gates).
- `docs/findings/2026-05-06.md` — walk-forward framework, mechanism analysis, regime finding, funding-loader bug fix.

The full pre-registration catalog is in `results/INDEX.md` (53 docs: 26 decision rules + 22 verdicts + 5 syntheses).

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

Each of the original 8 instruments has a dedicated YAML in `configs/` (`btcusdt.yaml`, `ethusdt.yaml`, `bnbusdt.yaml`, `solusdt.yaml`, `xrpusdt.yaml`, `linkusdt.yaml`, `ltcusdt.yaml`, `dogeusdt.yaml`). They are pinned to `min_rr: 1.0`, `target_rr: 5.0`, `stake_usd: 1000` — i.e. the falsified Option C settings. The configs have **not** been migrated to P4-Combined yet; the candidate strategy runs via CLI overrides (`--signal-tf 4H --target_rr 6.0 --side-filter short --max-hold-hours 336 --funding-csv-dir data/funding`) on top of `configs/default.yaml`. `run_backtest.sh` still auto-detects per-symbol configs by name and is wired to the legacy strategy. New sweeps for P4-Combined go through `scripts/run_p4_variant.sh` and `scripts/p4_oos_persistence.sh`, which build their own per-symbol configs from `configs/default.yaml` and the merged data CSVs — they do not read the per-symbol YAMLs.

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

**Post-deploy validation is mandatory.** After ANY `deploy/redeploy.sh`, run `scripts/post_deploy_check.sh`. 13 audit sections: (1) engines active, (2) watchdog timers armed, (3) deployed binary built from current source (md5 match), (4) per-engine tick freshness, (5) no ERROR logs in last 5 min, (6) rate-limit pressure healthy, (7) position-recovery events in last 24h surfaced for verification, (8) executor mode per engine (real-money / testnet / stub distinction; testnet/real-money detection avoids substring false-positives), (9) funding-CSV staleness warning (catches operator-missed weekly refresh), (10) disk space + log-size sanity (catches journal-write silent-failure mode), (11) drift detector cron freshness (launchd registration + history-recency check; catches the case where the decision-grade weekly cron silently stops firing), (12) restart-loop detection (catches systemd auto-restarting a crash-on-startup engine that §1/§4 miss), (13) live-config compliance (verifies each engine's ExecStart contains the locked CLAUDE.md flags — catches unauthorized `systemctl edit` parameter drift). Sections 4/5/6 each apply a 5-min uptime gate. Run with `STRICT=1` to exit non-zero on any warning AND fire a Telegram WARN (CI-friendly + alerts the operator on unattended runs).

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

**Live strategy (as of 2026-05-05):** the VPS engines are still running the falsified **Option C** — 5m EMA9×EMA21, `ema_mode: true`, `target_rr: 5.0`, both sides, no max-hold, no funding accrual. They are paper-only and produce no decision-grade signal until migrated.

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
- **Heartbeat startup grace.** `marketdata.Heartbeat.StartupGrace` suppresses Warn-level alerts during the first N seconds after `Run()` begins. `cmd/engine` sets it to `marketdata.DefaultStartupGrace` (3 min = `wsReadDeadline × wsMaxStalls` = the WS→REST fallback boundary). Without this, every `redeploy.sh all` floods Telegram with ~16 stale-feed WARN alerts that auto-clear within 3 min — pure noise that desensitizes the operator. During grace, snapshot outcomes that would be Warn (no-ticks, stale) are downgraded to Info "warming up" lines (logged only, no Telegram). Once grace expires the original level rules apply — a real stalled feed will alert at the next interval. Tests + zero-value preserve legacy alert-immediately behavior.
- **Telegram bot token MUST NOT leak into logs.** Go's net/http embeds the request URL (containing the token) in transport errors verbatim. All slog + return paths in `pkg/notify/telegram.go` go through `Notifier.redactErr`. New err-logging code paths in this package MUST use it — `TestSendStructured_BotTokenNeverInSlog` enforces the contract.
- **Engine startup MUST call `Stub.RecoverFromJournal()` for live + each shadow** to bridge the orphan-open gap that previously existed when an engine restart wiped `Stub.position` while the journal had an open event with no matching close. Recovery is no-op if `JournalPath` unset, position already non-nil, or no unclosed open in recent journal. Reconstructs entry/stop/target/side/reason/timestamp from the open event; `MaxAdverse`/`MaxFavorableR` reset to entry-time defaults (lossless for live config which doesn't use trail-stop or B2). Pre-open tick guard in `OnTick` (skip ticks where `tick.Timestamp < position.Signal.Timestamp`) prevents backfill replay from spuriously closing recovered positions on pre-open price action.
- **Signal-context sidecars (C2)** capture rich state at signal-emission time for forward-paper pattern-matching. Enabled by `--signal-context-dir <path>` flag or `PAPER_LIVE_SIGNAL_CONTEXT_DIR` env. Writes `<dir>/<label>/<SYMBOL>-<month>.jsonl` per runner (live + each shadow). Schema: `pkg/strategy/signal_context.go` `SignalContext` struct — entry/stop/target/RR + EMA9/EMA21/spread + ATR + realized vol + BB bands + bias + VWAP + PDH/PDL + funding rate + signal_tf + side_filter. Records are written **after all filters pass**, just before `executor.OnSignal` — filtered signals are not captured. Off by default; opt-in.
- **`Stub.JournalPath` opt-in — journal writes only happen in live mode.** `cmd/engine` sets `JournalPath`; `cmd/backtest` does not. When `JournalPath == ""`, `appendJournal` is a no-op. This preserves backtest behaviour byte-for-byte.
- **`stake_usd` must be set in config** for dollar PnL output. Without it, `total_pnl_usd` is not emitted and all `$1k` columns will show zero.
- **`julianDay`** (`pkg/indicators/vwap.go`) is the shared helper for detecting UTC day boundaries (used by both VWAP and DailyLevels). Returns `year*1000 + yearDay`.
- **Backfill channel buffer must exceed all backfill ticks.** `Subscribe` creates the tick channel before consumers start; backfill is synchronous and blocks on send if the buffer fills, so an undersized buffer hangs `Subscribe` indefinitely. The buffer is now sized dynamically: `backfillHours × 60 × 4 + 2048` (with a floor of 8192). At default `BackfillHours=96` that's 25,088 entries.
- **`http.DefaultClient` has no timeout — always use a custom client.** The backfill HTTP call uses `&http.Client{Timeout: 30 * time.Second}`. Without a timeout, a slow Binance response hangs `Subscribe` indefinitely with no log output.
- **`BinanceFutures` falls back to REST aggTrade polling when WebSocket stalls.** `fstream.binance.com` resolves globally to AWS Tokyo servers that accept WebSocket connections but deliver zero data frames. After `wsMaxStalls=2` consecutive 90s read timeouts (~3 minutes), `readLoop` switches to `aggTradeLoop` which polls `GET /fapi/v1/aggTrades?fromId=<lastID>` every 6 seconds. `fromId` pagination guarantees zero gaps and zero duplicates.
- **REST aggTrade rate limit: 10s poll interval × 16 symbols × 20 weight/call = 1920 weight/min (Binance cap: 2400/min).** `GET /fapi/v1/aggTrades` is **20 weight per call**, not 1 — the prior "1200 weight/min" claim assumed 10 weight/call and was wrong. At 6s × 16 symbols the actual load was 3200 weight/min, forcing ~50% wall-time in 60s rate-limit backoff (verified 2026-05-07; see Bug 4). Never reduce poll interval below 10s without recomputing against the actual 20-weight/call cost. On HTTP 418/429, back off 60s and retry — never exit the goroutine. Exiting closes the tick channel, which shuts down the strategy runner "cleanly" and triggers an infinite systemd restart loop.
- **Any new strategy or RR comparison must be backtested with `--exact-fills --include-boundary` before any live decision.** The default backtest exits at synthetic-tick wick prices, which overstates 1:1 RR strategies by enough to flip a 57-sym sweep from +$8M to −$4.7M (verified 2026-05-04). See `scripts/audit_phase4_btc.sh` for the reference harness.
- **Multi-year validation must use the continuous sweep, not monthly segments.** `scripts/continuous_sweep.sh` concatenates each symbol's 64 monthly CSVs and runs one Stub instance over the full 5y. Monthly-segmented sweeps force-close open positions at month-end last-known price (not at stop/target), inflating reported PnL by ~5% in aggregate (verified 2026-05-05: $7.09M segmented vs $6.77M continuous). The monthly approach is fine only for single-month debugging.
- **Realistic fee/slippage modeling is mandatory for any deployment-decision backtest.** `Stub.FeeBps` charges round-trip taker fees on entry notional (`units × entry`); `Stub.StopSlippageBps` charges adverse slippage on losing trades only. Default 0 reproduces pre-fee behavior bit-exact. The CLI flags are `--fee-bps` and `--stop-slippage-bps`. **Use `--fee-bps=10 --stop-slippage-bps=5` for Binance USDT-M Futures Regular tier** (0.05% taker × 2 sides = 10 bp round-trip; user is "Not Qualified" for VIP — verified 2026-05-05 from the user's Binance fee dashboard). With BNB held to enable the 10% Futures discount this drops to 9 bp. Use `scripts/realistic_sweep.sh` instead of `continuous_sweep.sh` for any new sweep. **Position notional, not stake, is the fee base** — at p50 BTC stop_dist of 0.18%, $1k stake → $555k notional → $555 round-trip fee per trade at 10 bp. This is the dominant realistic cost and the reason Option C was falsified on 2026-05-05.

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

- **Max-hold force-close skips slippage on winners** (`pkg/execution/stub.go:236`). Documented but **not fixed** — measurement on RUNEUSDT continuous 5y showed only 9 time_stops out of 154 trades (5.8%). Extrapolated impact across deployed-32: ~$3-17k over 5y depending on slip level (1-2% of Strategy A's NET). Cosmetic, not material. Fix would require ~75 min including baseline re-runs; deferred until forward-paper actually shows divergence.
- `CSVReplay` double-close: stream goroutine defers `f.Close()` and `main.go` also calls `defer src.Close()`. Idempotent (second close returns harmless error). Skip.
- `run_backtest.sh` runs the binary twice per month (display + accumulate). Use `full_analysis.sh` for multi-month runs. Performance only.

### Fixed

- ~~Pre-2026-05-08 fix-records~~ — go.mod version, indirect-deps, fundingUSDT comment, funding-CSV refresh tooling, EMAMode/TargetRR/P4-field wiring in cmd/engine, watchdog symbol list, **Bug 2** (wick-exit pricing → `--exact-fills`), **Bug 3α** (aggregator boundary-tick → `--include-boundary`). All landed before this milestone; full narrative in commit history + `docs/findings/`.
- **Bug 3β — Same-bar resolution**: synthetic open→high→low→close ordering means LONG positions always score TARGET when both stop and target lie within a single 1m kline range. `--pessimistic-ambiguous` flag detects and reclassifies these. At target_rr=5.0, ZERO ambiguous bars detected across 1,345 BTC winning trades — immaterial. Check again if target_rr is ever reduced below 2.
- ~~**Bug 4 — REST poll exceeded Binance weight cap**~~: 6s × 16 sym × 20 weight/call = 3200/min vs 2400 cap → ~50% backoff. Fix 2026-05-07: bumped to 10s (1920/min). See `docs/findings/2026-05-08.md`.
- ~~**Bug 5 — Cold-start indicator blind period**~~: backfill silently truncated to 1500 klines; restart needed 22 closed 4H candles → 64h blind. Fix 2026-05-07: paginated backfill, BackfillHours 48→96, liveMode stale-candle gate. See `docs/findings/2026-05-08.md`.
- ~~**Bug 6 — Engine restart orphans positions**~~: Stub.Position was RAM-only; restart left journal with open + no close. Fix 2026-05-07 (commit `9eaeb54`): `RecoverFromJournal()` + pre-open tick guard + 11 unit tests. 7 orphans recovered cleanly on deploy. See `docs/findings/2026-05-08.md`.


## Forward-paper go/no-go criteria

Forward-paper validation started 2026-05-05 20:06 UTC (32 Strategy B engines, since reduced to deployed-16). The deployed-32 list backtested at +$129k/yr at slip=25bp, but the train-only-shortlist diagnostic shows +70% look-ahead inflation at that slip level. **Anchor expectations to the honest annual: ≈ $69k/yr at slip=25bp**, not the deployed-claim or all-57 headline.

### Kill mechanism (decision-grade vs advisory) — IMPORTANT

The threshold-based criteria below are **advisory only.** Pre-registered Monte Carlo calibration (2026-05-07) found them mis-calibrated against the strategy's natural variance — under "null" (strategy performing as backtest predicted), they produce ~30-40% false-positive KILL verdicts. Recalibration via principled quantile-setting was attempted and found that no threshold can simultaneously meet FP(null)≤20% AND TP(dead)≥80% at the 90-day horizon (statistical impossibility; distribution overlap). See `results/kill_bar_recal_verdict_2026-05-07.md`.

**The decision-grade kill mechanism is `scripts/live_vs_backtest_drift.py`** — distribution-based detection comparing live trades to the backtest empirical distribution via Welch t-tests + WR z-test, Bonferroni-corrected. Calibration found it VIABLE: TP(deg30)=TP(deg50)=TP(dead)=100% at every operating point in the locked grid. Deployed operating point: α_family=0.001, N_LIVE=50 (FP=12.1%, TP=100% single-shot).

**Operational cadence matters — don't cron daily.** Time-to-detection analysis (2026-05-08) found median detection day = 26d under any degradation (deg30/deg50/dead) — fast. But cumulative FP under daily sequential testing reaches 28%/year (most accrued in the first 30 days). Single-shot at fixed N=50 has 12% FP; daily testing on accumulating sample inflates this 2.3×. Operational recommendation:

- **Cadence**: run weekly or per-N (every ~50 new trades, ≈42 days at fleet rate), NOT daily. Reduces sequential FP to estimated 8-15%/year.
- **Single firing**: investigation-grade signal, not auto-kill. Cross-check against forward-paper PnL trajectory.
- **Two firings 7+ days apart, OR drift + threshold-kill match**: strong signals — auto-kill candidate.
- The verdict (`results/drift_detector_time_to_detection_verdict_2026-05-08.md`) is NEEDS_TUNING — too-noisy-null at daily cadence; the locked rule forbids retuning within this milestone but flags the pattern for operational practice.

**Canonical invocation: `scripts/run_drift_check.sh`** — wrapper around `live_vs_backtest_drift.py` that persists each run to `results/drift_check_history.jsonl` and captures full output to `results/drift_runs/<ts>.log`, then evaluates the two-firings-≥7d rule mechanically across all-time history. Distinct wrapper exit codes for cron severity gating: 0 CLEAN, 1 INVESTIGATION (single firing), 2 INSUFFICIENT, 3 ERROR, 4 AUTO-KILL CANDIDATE (rule tripped, with the firing pair surfaced in the verdict line). Use `--quiet` for cron piping; pass-through args (e.g. `--live-source local`) are forwarded to the underlying detector. Don't invoke `live_vs_backtest_drift.py` directly except for one-off debugging — direct invocation skips the rule eval and won't update the history index, so the next wrapper run can't see that firing.

**Scheduled execution (macOS launchd).** `deploy/drift-check.launchd.plist` registers `com.tradingengine.drift-check` to run weekly (Sunday 09:00 local) — the locked cadence from the time-to-detection verdict. Install: `cp deploy/drift-check.launchd.plist ~/Library/LaunchAgents/com.tradingengine.drift-check.plist && launchctl load -w ~/Library/LaunchAgents/com.tradingengine.drift-check.plist`. Smoke-test: `launchctl start com.tradingengine.drift-check`. Inspect: `launchctl list | grep tradingengine` (last column = last exit code: 0 CLEAN / 1 INVESTIGATION / 2 INSUFFICIENT / 3 ERROR / 4 AUTO-KILL); launchd-side stdout/stderr go to `results/drift_runs/launchd.{out,err}.log`. Job uses `--live-source vps` (default) so SSH-to-VPS must be passwordless from launchd's environment — macOS Keychain integration handles this automatically when `ssh root@178.105.24.230 echo ok` works without prompting in a fresh Terminal.

The plist actually invokes `scripts/weekly_audit.sh`, a small wrapper that runs three independent stages each Sunday: (1) `run_drift_check.sh --quiet` for the decision-grade signal, (2) captures `forward_paper_status.sh` to a dated text snapshot at `results/forward_paper_snapshots/<YYYY-MM-DD>.txt` for longitudinal diffing, (3) SSH-runs `cmd/journal_validate --dir /var/log/paper-live/journal --exclude archive` for journal self-consistency (Telegram CRITICAL on errors via the shared `scripts/lib/notify.sh`). Snapshot or validate failure is non-fatal (drift signal is preserved regardless); the wrapper propagates the drift exit code so launchd's last-exit-code reflects the kill signal, not validation result. Manual `./scripts/weekly_audit.sh --no-snapshot` skips stage 2.

**`cmd/journal_validate`** is the journal self-consistency checker. Walks a journal directory and groups files by (cohort, symbol) so cross-month closes (positions opened in month-N, closed in month-N+1) don't false-positive. Per-cohort-per-symbol state tracks in-flight opens; terminal closes (TARGET/STOP/TIME) decrement, PARTIAL closes leave the count unchanged (B2 mid-R partial-take). Invariants: monotonic ts within file, no duplicate (symbol, ts) opens, no close-without-in-flight-open, no open-while-already-in-flight. Exit codes: 0 CLEAN / 1 WARN-only / 2 ERROR / 3 USAGE. `--exclude archive` skips frozen pre-Bug-6 journal subdirs whose pre-fix issues are intentionally immutable.

**Telegram alerts on drift fire.** The drift wrapper POSTs to Telegram on exit codes 1 (INVESTIGATION → ⚠ WARN), 3 (ERROR → ⚠ WARN), 4 (AUTO-KILL CANDIDATE → 🚨 CRITICAL). Reads the same `TELEGRAM_BOT_TOKEN` / `TELEGRAM_CHAT_ID` env vars the engine uses; missing vars → silent no-op (graceful degrade, same pattern as the engine's notify package). Code 0 (CLEAN) and 2 (INSUFFICIENT) do NOT alert — the kill-bar mis-calibration finding warned that routine-noise alerts desensitize the operator. To enable Telegram from the launchd context, add `TELEGRAM_BOT_TOKEN`/`TELEGRAM_CHAT_ID` to the plist's `EnvironmentVariables` block (operator-side; secrets stay out of the repo).

The threshold criteria remain useful as **early-warning indicators** that warrant investigation, but should not auto-trigger a kill. Always cross-reference against the drift detector before acting on a `forward_paper_status.sh` KILL verdict.

See `results/drift_detector_calibration_verdict_2026-05-07.md` and `results/drift_detector_time_to_detection_verdict_2026-05-08.md` for the full calibration record.

### Statistical-power floor (before reading any signal)

P4-Combined backtests at WR 20.64% with 6:1 R:R. Breakeven WR ≈ 14.3% — only ~6.3pp cushion. To bound observed WR within ±10pp at 95% CI requires ~63 trades; ±5pp requires ~250 trades. Strategy B at 5,809/5y × 32/57 sym ≈ 1.8 trades/day → 60 days ≈ 108 trades = ~±7pp resolution. **Do not draw conclusions from < 60 calendar days of forward data.**

### Deploy real money (small tranche, 1/10th notional) only if ALL true

Note: the "≥150 trades AND ≥60 days" gate is internally inconsistent at the historical fleet trade rate of ~1.18 trades/day. 60 days yields ~70 trades on average; ~127 days are needed to hit 150 trades. Surfaced in kill-bar calibration 2026-05-07. The two thresholds collide; apply both literally (whichever comes second).

- ≥150 live trades accumulated AND ≥60 calendar days net-positive in dollar terms
- Realized round-trip taker fees ≤ 12 bp (vs 10 bp modeled — 20% slack). The journal cost-decomposition schema (commit `7939786`, 2026-05-07) writes `fee_usd`/`slip_usd`/`notional_usd` per close so this is now directly evaluable in `forward_paper_status.sh`.
- Realized stop-side slippage ≤ 20 bp on the losing-trade subsample (vs 5-25 bp modeled range)
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

- Don't reshuffle the deployed-32 mid-flight. Look-ahead is in backtest test_NET, not forward data.
- Don't promote to Strategy A (Strategy A's max-hold-force-close-slippage bug `stub.go:236` is documented cosmetic but doesn't help here).
- Don't add symbols. Trade-count throughput is currently 1.8/day; adding symbols increases REST-poll load against the 2400 weight/min Binance Regular cap.
- Don't tune target_rr, signal_tf, or side-filter. Every additional sweep cell consumes statistical degrees of freedom you've already spent.
- Don't read into wins/losses inside the 60-day power floor. The natural shorts-only hit-rate is 20.6% — variance is enormous at low n.

## Historical strategies (kept for context — do not use to decide)

**Option C (EMA9×EMA21, 5m, target_rr=5.0)** — falsified 2026-05-05. Continuous 5y × 57 symbols at 8 bp fees + 5 bp slip = **NET −$143.76M, 0/57 profitable**. Required WR 22.9% vs observed 17.0%. Pre-fees the same sweep was +$6.77M (40/57 profitable) — a fee-illusion edge of 0.3pp above breakeven WR. Output: `results/option_c_57sym_realistic_2026-05-05.txt`. Live VPS engines are still paper-trading this strategy as of 2026-05-05 — see *Current state* table at top.

**Absorption + breakout (PDH/PDL, min_rr=1.0)** — original strategy, superseded 2026-05-04. Code remains live and toggleable via `ema_mode: false` in any per-symbol YAML. README.md still describes this strategy in the "Strategy" / "Backtest Results" / "Cross-Instrument Summary" sections; those sections are stale and pending rewrite once a successor is forward-validated.

## Known unmodeled risks

After the 2026-05-05 cost-survivor battery and 2026-05-06/07 follow-up work, the original 13 caveats are now reduced to 3 still-open:

- **6 s REST polling lag is unmodeled** (10s as of 2026-05-07 Bug 4 fix). Adverse on every entry and every stop. Live-only risk — not testable in backtest. Mitigation: monitor live vs backtest signal-to-execution gap during forward-paper. The journal cost-decomposition schema lets this be measured directly once realized fills accumulate.
- **Funding-CSV staleness drift.** Engines pick up refreshed CSVs only on restart. Between restarts, trades held past the CSV's last entry get $0 funding. Bounded by time-since-last-restart; weekly refresh + restart caps drift at ~7 days. **Mitigated 2026-05-08:** `cmd/engine` logs `slog.Warn` at startup if `funding.Historical.LastTS()` is >7d old, naming `symbol`, `last_funding_ts`, and `stale_days`. The previously-silent "operator forgot to refresh" failure mode now produces a loud signal in `/var/log/paper-live/<symbol>.log` (and surfaces via `post_deploy_check.sh` section 9). Net funding ≈ $0 in steady state per backtest, so divergence remains small in practice.
- **No real-money execution test.** Forward-paper with realistic position sizing, exchange position limits, margin reuse, and concurrent-trade interaction is unmodeled. Honest backtest projection is $69-184k/yr depending on slip (see `## Train-only shortlist diagnostic`); the original $74-142k/yr framing was based on the deployed-32 list which carries +26-70% look-ahead inflation.

**Resolved or downgraded since 2026-05-05:** max-hold winner-slippage (cosmetic, 5.8% time-stop rate, deferred); funding-CSV silent fallback (clean at deploy); `tradeResult.fundingUSDT` comment; symbol-selection look-ahead (quantified +26-70%, not eliminated); orphan-on-restart (Bug 6 fixed in `9eaeb54`); threshold kill criteria (mis-calibrated at 90d, replaced by drift detector as decision-grade).

