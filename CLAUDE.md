# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Current state (2026-05-06, post-walk-forward — high-variance, not-decayed, not-validated)

> ⚠️ **STRATEGY STATUS: HIGHLY VARIANT, NOT FORWARD-VALIDATED. Real-money sizing remains zero.** Today produced both an alarming fresh-OOS finding AND a stricter walk-forward validation that re-frames it. Order matters — read both.
>
> **1. Fresh OOS finding (n=1 window):** the only 12-month window (2025-05 → 2026-04) of data the strategy was never tuned against showed universe-57 NET = +$24k @ slip=5bp / **−$82k @ slip=15bp** / −$187k @ slip=25bp, vs backtest expectation +$210k / +$142k / +$74k per year. Longs-only worse (−$462k), both-sides −$438k. Initially read as alpha decay.
>
> **2. Walk-forward re-framing (n=3 windows):** ran the SAME deployed configuration on three non-overlapping 12-month windows (2023-05 → 2024-04, 2024-05 → 2025-04, 2025-05 → 2026-04). 4H result: **+$15k / +$504k / −$82k** — sum **+$437k**, mean **+$146k/yr**. The mean across the 3 windows MATCHES the original backtest expectation almost exactly ($146k vs $142k). The W3 loss is **one of three windows**, not a categorical decay verdict.
> - 1H: −$5,283k across 3 windows, 0/3 positive → REJECTED
> - 2H: −$912k, 1/3 positive → REJECTED
> - **4H (deployed): +$437k, 2/3 positive → SUPPORTIVE**
> - **1D: +$85k, 2/3 positive (incl. true OOS) → SUPPORTIVE**
>
> **3. Critical caveat on walk-forward:** of the 3 windows, only W3 (2025-05 → 2026-04) is true OOS — the universe-57 was selected by knowing what worked over 2020-01 → 2025-04, so W1 and W2 carry symbol-selection look-ahead. Walk-forward shows VARIANCE honestly; it does not validate forward edge.
>
> **What the evidence does NOT support:**
> - "Strategy decayed, kill it" — n=1 evidence, walk-forward shows W3 is one of three windows, the mean matches expectation
> - "Walk-forward validates the strategy" — only one window is true OOS, and the variance is enormous ($-82k to $+504k)
> - "Deploy persistent-20 / drop 7 dead picks" — both retracted, post-hoc data mining (see `## Post-failure rescue` below)
>
> **What the evidence DOES support:**
> - The strategy has very high per-window variance under realistic costs
> - The mean economics across multiple windows are consistent with backtest expectations
> - 1H and 2H are clearly fee-killed (REJECTED across all windows including in-sample)
> - We need more truly-OOS windows to make a confident forward claim — i.e. more calendar time
>
> **Operational stance:**
> - 16 paper engines on 4H Strategy A continue running as forward observational data
> - Real-money allocation remains zero until walk-forward accumulates more truly-OOS windows AND the 60-day forward-paper go/no-go criteria pass
> - The "kill if 30 days net-negative" rule from this morning is reset — apply against walk-forward expectations ($28-146k/yr depending on TF), not against either the old backtest claim or the morning's panic claim
>
> Full analysis: `results/walk_forward_*_2026-05-06.txt`, `scripts/walk_forward.sh`, `scripts/walk_forward_compare.py`. See `## Walk-forward validation framework (2026-05-06)` below for the full breakdown and methodology, and `## Fresh OOS validation (2026-05-06)` for the n=1 finding that prompted it.
>
> **4. Mechanism analysis (added 2026-05-06):** tested four hypotheses about WHY the strategy appears to have edge. Findings:
> - **Per-symbol "skill" is statistically indistinguishable from chance** (8 ROBUST symbols observed vs 9.5 expected, z=−0.52). NO selection rule from backtest data captures real per-symbol edge — this strengthens the morning's data-mining critique into a statistical refutation.
> - **Shorts-side asymmetry is UNIVERSAL across 3 windows** (longs negative in W1, W2, AND W3; aggregate +$1.08M shorts-vs-longs over 3 years). This is the load-bearing structural edge, mechanistically explainable as crypto retail pump-and-fade dynamics.
> - **Edge concentrates in mid-vol altcoins** (+$332k 3yr) — majors lose money (−$15k), very-high-vol names get whipsawed (+$120k vs mid's $332k).
> - **Implication:** the strategy IS exploiting a real structural property of crypto markets, but symbol-level selection adds variance not edge. Position uniformly across the relevant universe; the edge is the bet on crypto's short-side skew, not on which specific symbols.
> See `## Mechanism analysis (2026-05-06)` below for evidence and tests.

## Current state (2026-05-06, post-walk-forward — high-variance, see ⚠️ banner above for walk-forward re-framing)

| Layer | What is it | Where |
|---|---|---|
| **Strategy A (primary)** | **P4-Combined** — 4H EMA9×EMA21 cross, wick stop, `target_rr=6.0`, `--side-filter short`, `--max-hold-hours 336`, `--funding-csv-dir data/funding`, `--fee-bps 10 --stop-slippage-bps 5`. **Cost-survivor battery validated 2026-05-05:** robust to slip ∈ {5, 15, 25} bp; train-only target_rr sweep independently picked rr=6 (honest OOS); survives top-5 drop (+$751k) and late-listing exclusion (+$926k). Realistic-slip range: **+$369k to +$710k over 5y** ($74k–$142k/yr) headline; **honest train-only-selected expectation $69-184k/yr** depending on slip (see `## Train-only shortlist diagnostic`). | `results/p4_combined_2026-05-05.txt`, `results/battery_2026-05-05/` |
| **Strategy B (conservative fallback)** | **P4-Shorts-Only** — same as A but **no `--max-hold-hours`**. Lower headline but most slip-elastic and avoids the unmodeled max-hold-force-close-slippage bug (`pkg/execution/stub.go:236`). 5,809 trades vs A's 7,772. Realistic-slip range: **+$168k to +$415k over 5y** ($34k–$83k/yr). | `results/battery_2026-05-05/shorts_only_slip*.txt` |
| **Live engines (VPS)** | **RUNNING — 16 engines on Strategy A since 2026-05-06 09:27 UTC** (last redeploy from `main` at commit `d1a71a1`). Forward-paper validation clock started 2026-05-05 20:06 UTC, briefly interrupted during the 2026-05-06 multi-engine investigation. Per-symbol architecture, `paper-live@*.service`. ExecStart: `--signal-tf 4H --target-rr 6.0 --side-filter short --max-hold-hours 336 --funding-csv-dir /opt/trading-engine/data/funding --fee-bps 10 --stop-slippage-bps 5 --funding-bps-per-day 0`. **Operational reality:** 14-15/16 healthy at any moment; 1-2 chronically stale (Tokyo backend lottery, see `## Multi-symbol engine investigation (2026-05-06)`); REST aggTrade fallback is the steady-state path because Binance WS endpoints are unreliable from this VPS. 16 engines × 6s polling = 3200 weight/min worst case (33% over 2400 cap) — handled by per-engine 60s 429-backoff, no 418 bans. | journal: `/var/log/paper-live/journal/*.jsonl`, logs: `/var/log/paper-live/*.log` |
| **Multi-symbol engine refactor** | **Investigated and abandoned 2026-05-06.** Branch `feature/multi-symbol-engine` (19 commits) on GitHub preserved as research record. Multi-engine architecture trades the cold-start backfill burst risk (the 2026-05-05 9-hour ban cause) for single-connection-fragility — and per-connection-luck dominates so badly from this VPS that one bad WS connection silences all 16 symbols. Per-symbol architecture's resilience comes from 16 independent connection rolls. The latent JSON-collision bug discovered during this work (`aggTradeMsg.EventTime`) WAS cherry-picked to main as commit `d1a71a1`. | branch: `feature/multi-symbol-engine`; investigation log in `docs/plans/2026-05-06-multi-symbol-engine.md` and `docs/plans/2026-05-06-multi-symbol-smoke.log` |

**Deployed shortlist (running on Strategy A):** the authoritative list lives at `configs/symbols.yaml` under the `deployed` group — read by `scripts/paper_live_watchdog.sh`, `scripts/paper_live_trades.sh`, `deploy/redeploy.sh`, etc. via `scripts/lib/symbols.sh`. As of 2026-05-06 (post-swap): 16 symbols, train-only top-K from the `universe` group with the robustness gate (train_NET > 0 at slip=25bp drops ETH/LINK/VET/LDO) and the exchangeInfo gate in `scripts/select_16_engines.py` (which excludes any SETTLING/DELISTED contract — added after the MKR/FTM incident, see `scripts/select_16_engines.py:fetch_trading_symbols()`).

**Swap on 2026-05-06 11:30 UTC:** dropped MKRUSDT and FTMUSDT — both confirmed `status:"SETTLING"` on Binance Futures (MKR delivery date 2025-09-08, FTM delivery date 2025-01-06). They were delisted contracts that had been silently consuming engine resources for hours: MKR's REST `aggTrades` returned 240-day-stale data, FTM returned `[]`. Replaced by the next two train-rank candidates BCH (rank 17) and APT (rank 18). Lesson: `scripts/select_16_engines.py` selected from 2020-2025 backtest data without checking current Binance Futures `exchangeInfo` status — should be added to the selection pipeline. Avoids the +26-70% look-ahead inflation of the previous deployed-32. See `scripts/select_16_engines.py` for reproducible selection logic.

**Headline numbers carry these still-open caveats** (`## Known unmodeled risks`): unmodeled max-hold force-close slippage (Strategy A only — measured cosmetic at 1-2% of NET), 6s REST polling lag (now active steady-state, not just fallback), forward funding-regime risk, no real-money execution test. Eight prior caveats were defused by the 2026-05-05 cost-survivor battery; the symbol-selection look-ahead was quantified and incorporated into the 16-engine deployed shortlist.

**2026-05-06 train-only-shortlist diagnostic (`## Train-only shortlist diagnostic (2026-05-06)`):** the deployed-32 list was selected by requiring positive PnL in BOTH train AND test halves — symbol-layer look-ahead. Quantified: deployed-32 test_NET is inflated by **+26% at slip=5bp, +34% at slip=15bp, +70% at slip=25bp** vs honest top-32-by-train. Strategy still PASSES at every slip level. **Honest forward-PnL expectation is $69k/yr at slip=25bp** (low end of original $74-142k/yr range), not the middle. The current 16-engine deploy uses honest selection (no look-ahead inflation in expected forward PnL).

**Latent JSON-collision bug fix (2026-05-06):** added `EventTime int64 \`json:"E"\`` to `aggTradeMsg` in `pkg/marketdata/binance.go`. Without it, every WS aggTrade message returned a non-nil error from Unmarshal due to Go's case-insensitive JSON fallback colliding `"E"` (number) with `"e"` (string) — engines silently dropped every WS tick at `binance.go:254` and fell back to REST aggTrade polling. Fix landed on `main` at `d1a71a1` and deployed to VPS at 09:27 UTC. Latent benefit: when Binance WS endpoints recover from current zero-frame state, engines will use WS instead of REST automatically.

## Build & Run

```bash
# Build
go build ./...

# Run backtest (single month)
go run ./cmd/backtest --config configs/btcusdt.yaml

# Override date without editing config
go run ./cmd/backtest --config configs/btcusdt.yaml --year 2024 --month 06

# Run live engine (paper trading via Binance WebSocket)
go run ./cmd/engine --config configs/btcusdt.yaml

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
```

**Local paper-live scripts** (for running locally without VPS):
```bash
./scripts/paper_live_start.sh   # builds bin/engine, launches 8 background processes
./scripts/paper_live_status.sh  # heartbeat count, last trade time
./scripts/paper_live_stop.sh    # graceful SIGTERM
```

Trade journals land in `/var/log/paper-live/journal/{SYMBOL}-YYYY-MM.jsonl` on VPS (or `./logs/journal/` locally). The engine backfills 48h of 1m klines from `fapi.binance.com` on startup to prime `DailyLevels` before the first live tick.

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

## Key Invariants

- **Exchange timestamps only.** `Candle` and `Tick` timestamps come from the exchange. `time.Now()` is never used for price data.
- **`DataSource` is the only seam between backtest and live.** `cmd/backtest` uses `CSVReplay`; `cmd/engine` uses `BinanceFutures`. Everything downstream is identical.
- **`expandKlineToTicks` is the canonical tick-expansion helper** (`pkg/marketdata/klines.go`). Both `CSVReplay` (backtest) and `BinanceFutures` (REST backfill on startup) call it so interpolation is identical in both paths. Do not inline a second copy.
- **`BinanceFutures` backfills 48h of 1m klines on startup** by fetching `GET /fapi/v1/klines` from `fapi.binance.com` before opening the WebSocket. This primes `DailyLevels` (PDH/PDL) immediately. Backfill failure is non-fatal — engine logs a warning and proceeds blind for ~24h, same as before.
- **`Stub.JournalPath` opt-in — journal writes only happen in live mode.** `cmd/engine` sets `JournalPath`; `cmd/backtest` does not. When `JournalPath == ""`, `appendJournal` is a no-op. This preserves backtest behaviour byte-for-byte.
- **`stake_usd` must be set in config** for dollar PnL output. Without it, `total_pnl_usd` is not emitted and all `$1k` columns will show zero.
- **`julianDay`** (`pkg/indicators/vwap.go`) is the shared helper for detecting UTC day boundaries (used by both VWAP and DailyLevels). Returns `year*1000 + yearDay`.
- **Backfill channel buffer must exceed all backfill ticks.** `Subscribe` creates the tick channel before consumers start. At 1500 klines × 4 ticks = 6000 max ticks, the buffer is set to 8192. If `BackfillHours` ever increases past 25h (1500 klines), re-check this invariant.
- **`http.DefaultClient` has no timeout — always use a custom client.** The backfill HTTP call uses `&http.Client{Timeout: 30 * time.Second}`. Without a timeout, a slow Binance response hangs `Subscribe` indefinitely with no log output.
- **`BinanceFutures` falls back to REST aggTrade polling when WebSocket stalls.** `fstream.binance.com` resolves globally to AWS Tokyo servers that accept WebSocket connections but deliver zero data frames. After `wsMaxStalls=2` consecutive 90s read timeouts (~3 minutes), `readLoop` switches to `aggTradeLoop` which polls `GET /fapi/v1/aggTrades?fromId=<lastID>` every 6 seconds. `fromId` pagination guarantees zero gaps and zero duplicates.
- **REST aggTrade rate limit: 6s poll interval × 12 symbols = 1200 weight/min (limit: 2400).** Never reduce below 6s without recalculating. On HTTP 418/429, back off 60s and retry — never exit the goroutine. Exiting closes the tick channel, which shuts down the strategy runner "cleanly" and triggers an infinite systemd restart loop.
- **Any new strategy or RR comparison must be backtested with `--exact-fills --include-boundary` before any live decision.** The default backtest exits at synthetic-tick wick prices, which overstates 1:1 RR strategies by enough to flip a 57-sym sweep from +$8M to −$4.7M (verified 2026-05-04). See `scripts/audit_phase4_btc.sh` for the reference harness.
- **Multi-year validation must use the continuous sweep, not monthly segments.** `scripts/continuous_sweep.sh` concatenates each symbol's 64 monthly CSVs and runs one Stub instance over the full 5y. Monthly-segmented sweeps force-close open positions at month-end last-known price (not at stop/target), inflating reported PnL by ~5% in aggregate (verified 2026-05-05: $7.09M segmented vs $6.77M continuous). The monthly approach is fine only for single-month debugging.
- **Realistic fee/slippage modeling is mandatory for any deployment-decision backtest.** `Stub.FeeBps` charges round-trip taker fees on entry notional (`units × entry`); `Stub.StopSlippageBps` charges adverse slippage on losing trades only. Default 0 reproduces pre-fee behavior bit-exact. The CLI flags are `--fee-bps` and `--stop-slippage-bps`. **Use `--fee-bps=10 --stop-slippage-bps=5` for Binance USDT-M Futures Regular tier** (0.05% taker × 2 sides = 10 bp round-trip; user is "Not Qualified" for VIP — verified 2026-05-05 from the user's Binance fee dashboard). With BNB held to enable the 10% Futures discount this drops to 9 bp. Use `scripts/realistic_sweep.sh` instead of `continuous_sweep.sh` for any new sweep. **Position notional, not stake, is the fee base** — at p50 BTC stop_dist of 0.18%, $1k stake → $555k notional → $555 round-trip fee per trade at 10 bp. This is the dominant realistic cost and the reason Option C was falsified on 2026-05-05.

## Test Coverage

| File | What it tests |
|------|--------------|
| `pkg/marketdata/klines_test.go` | `expandKlineToTicks` — 4 ticks emitted, monotonic timestamps, volume split equally, first/last tick at open/close time |
| `pkg/execution/stub_test.go` | Journal sink writes `open`+`close` JSONL lines; journal disabled (no files) when `JournalPath` is empty |
| `pkg/notify/telegram_test.go` | POST body verified via httptest server; no-op when env unset; no-op when only one credential set |

High-value gaps still missing: `EntryDetector` EMA crossover and absorption/breakout sequences, `DailyLevels` day-roll, `VWAP` session reset, `Stub.Summary` PnL math.

## Known Bugs

### Open

- **Max-hold force-close skips slippage on winners** (`pkg/execution/stub.go:236`). Documented but **not fixed** — measurement on RUNEUSDT continuous 5y showed only 9 time_stops out of 154 trades (5.8%). Extrapolated impact across deployed-32: ~$3-17k over 5y depending on slip level (1-2% of Strategy A's NET). Cosmetic, not material. Fix would require ~75 min including baseline re-runs; deferred until forward-paper actually shows divergence.
- `CSVReplay` double-close: stream goroutine defers `f.Close()` and `main.go` also calls `defer src.Close()`. Idempotent (second close returns harmless error). Skip.
- `run_backtest.sh` runs the binary twice per month (display + accumulate). Use `full_analysis.sh` for multi-month runs. Performance only.

### Fixed

- ~~`go.mod` declared `go 1.26.2`~~ — turns out **1.26.2 is the actual installed Go version**; the prior "invalid" claim was stale. No change needed.
- ~~All `go.mod` dependencies marked `// indirect`~~ — fixed 2026-05-06 via `go mod tidy`. Markers removed.
- ~~`tradeResult.fundingUSDT` field comment said "always non-negative"~~ — fixed 2026-05-06. Comment now correctly reflects that the value is signed when using the Historical provider.
- ~~Funding-CSV staleness for forward trades~~ — partially addressed 2026-05-06 via `scripts/refresh_funding.sh` and `INCREMENTAL=1` mode of `download_funding.sh`. Recommended: weekly refresh during forward-paper-validation. Engines pick up refreshed CSVs on next restart.
- ~~`cmd/engine` did not wire `EMAMode`/`TargetRR` into `EntryConfig`~~ — fixed 2026-05-04. Always add new `EntryConfig` fields to **both** `cmd/backtest/main.go` and `cmd/engine/main.go`.
- ~~`cmd/engine` did not wire P4 fields (SideFilter, MaxHoldHours, FundingProvider, FeeBps, StopSlippageBps, target-rr, signal-tf)~~ — fixed 2026-05-05. CLI flags mirror cmd/backtest.
- ~~Watchdog symbol list pinned to old deployed-16~~ — fixed 2026-05-05. Now matches the deployed-32. Stale `alerted.state` cleared.
- ~~**Bug 2 — Wick-exit pricing**~~: `Stub.OnTick` closed at `tick.Price` (synthetic wick extreme) instead of `sig.StopLoss`/`sig.TakeProfit`. Inflated all R:R ≤ 2 results; at 1:1 RR the entire 57-sym edge was artifact (+$8M → −$4.7M). Fixed via `--exact-fills` flag.
- ~~**Bug 3α — Aggregator boundary-tick exclusion**~~: routed boundary tick into next candle. Corrected via `--include-boundary` flag; effect at target_rr=5.0 is +10.6%.
- **Bug 3β — Same-bar resolution**: synthetic open→high→low→close ordering means LONG positions always score TARGET when both stop and target lie within a single 1m kline range. `--pessimistic-ambiguous` flag detects and reclassifies these. At target_rr=5.0, ZERO ambiguous bars detected across 1,345 BTC winning trades — immaterial. Check again if target_rr is ever reduced below 2.

## Timeframe sweep (2026-05-06)

After the 2026-05-05 cost-survivor battery confirmed Strategy B works on 4H, we extended the aggregator to support 1H, 2H, and 1D (previously only 5m/30m/4H were wired) and ran a full comparison sweep. Outputs in `results/battery_2026-05-06/`. **Verdict: 4H is empirically optimal — no challenger meets the 20%-better threshold.**

| TF | slip=5 | slip=15 | slip=25 | Trades | Profitable |
|---|---:|---:|---:|---:|---:|
| 1H | **−$3,128,863** | **−$6,101,684** | **−$9,074,506** | 24,780 | 11/57 |
| 2H | **−$624,889** | **−$1,821,906** | **−$3,018,923** | 12,651 | 20/57 |
| **4H** | **+$1,051,624** | **+$710,280** | **+$368,936** | 7,772 | **45/57** |
| 1D | +$96,084 | +$82,363 | +$68,643 | 735 | 28/57 |

Two competing forces determine total NET: **per-trade economics** (which improve as TF gets coarser because wider stops mean less leverage and lower fees) and **trade frequency** (which decreases as TF gets coarser). 4H is the sweet spot — first TF where per-trade is positive ($+135/trade) AND volume is high enough (~7,800 trades / 5y / 57 sym) to compound. 1D has even better per-trade economics ($+130) but only 735 trades total. 1H/2H trade enough but per-trade is negative due to fee burden.

**This DEFUSES one of the prior open caveats** — 4H is now confirmed empirically optimal, not just the best of a small tested set.

## Train-only shortlist diagnostic (2026-05-06)

The deployed-32 list was selected by requiring positive PnL in **both** train (2020-2022) AND test (2023-2025). That uses test data as a filter — symbol-layer look-ahead. The train-only-shortlist diagnostic re-runs OOS persistence at slip ∈ {5, 15, 25} bp, then partitions the 57-symbol universe into selection cohorts to quantify the bias. Outputs in `results/honest_oos_slip{5,15,25}_2026-05-06.txt` and `results/slip_stress_diagnostic_2026-05-06.txt`. **Verdict: PASS at every slip level. Honest forward-PnL expectation is $69-184k/yr depending on slip realization.**

### Test-period NET ($, total) by selection rule

| slip | all-57 | deployed-32 (look-ahead) | top-32-by-train (honest) | train-positive (honest, n=var) |
|:---:|---:|---:|---:|---:|
| 5bp | +$619,862 | **+$559,434** | +$444,166 | +$488,304 (n=36) |
| 15bp | +$406,385 | **+$437,773** | +$327,295 | +$346,630 (n=35) |
| 25bp | +$192,910 | **+$301,367** | +$177,520 | +$177,520 (n=32) |

### Look-ahead inflation in deployed-32 (vs same-size honest top-32-by-train)

| slip | absolute $ | % of honest | $/yr inflated |
|:---:|---:|---:|---:|
| 5bp | +$115,268 | +26.0% | +$49,401/yr |
| 15bp | +$110,478 | +33.8% | +$47,348/yr |
| 25bp | +$123,847 | **+69.8%** | +$53,077/yr |

The fixed dollar bias (~$115-124k on test) is constant across slip; as honest test PnL shrinks under cost pressure, the percentage inflation grows. **The deployed list's claim is most inflated under the most realistic friction.**

### Honest annualised test PnL (2.33yr period)

| slip | honest top-32-by-train | honest train-positive | deployed-32 (look-ahead) |
|:---:|---:|---:|---:|
| 5bp | $190,357/yr | $209,273/yr | $239,757/yr |
| 15bp | $140,269/yr | $148,556/yr | $187,617/yr |
| 25bp | **$76,080/yr** | $76,080/yr | $129,157/yr |

Note: split-OOS aggregates run 3-9% above continuous (CLAUDE.md "monthly approach inflates by ~5%" effect). Adjusted honest annual ≈ **$184k / $134k / $69k/yr** at slip 5/15/25.

### Symbol swap that explains the bias

Going from honest top-32-by-train to deployed-32 swaps 3 names. At slip=5bp:

| Direction | Symbols | train_NET sum | test_NET sum |
|---|---|---:|---:|
| Removed (regime flippers excluded by look-ahead) | DOGE, DYDX, ICP | +$72,261 | **−$51,909** |
| Added (lower-train but test-positive) | LDO, LINK, VET | +$7,596 | **+$63,359** |
| Net swap value (the look-ahead) | | | **+$115,268** |

### Recoveries cohort — structural blind spot

15 symbols (ARB, ATOM, BLUR, BNB, GMX, HBAR, IOTA, LDO, OP, PYTH, SEI, SUI, TIA, WLD, ZIL at slip=5) have train_NET ≤ 0 but test_NET > 0. Their test contribution is **+$236,409 at slip=5, +$201,026 at slip=15, +$207,110 at slip=25**. They are INVISIBLE to any train-only filter, including the deployed-32. A rolling-shortlist policy was tested as a fix — see next section.

### Rolling-shortlist test (2026-05-06) — naive policy underperforms

Tested a quarterly-rebalanced rolling shortlist with trailing 4-quarter (12-mo) lookback, top-32 by trailing NET. Inputs: `results/p4_quarterly_slip15_2026-05-06.tsv` (per-symbol per-quarter NET at slip=15bp). Forward window: 2021-Q1 → 2025-Q1 (4.25yr).

| Selection rule | forward NET | $/yr |
|---|---:|---:|
| Rolling shortlist (trailing 4Q, top-32) | +$404,125 | +$95,088 |
| Deployed-32 (fixed) | **+$802,124** | **+$188,735** |
| All-57 (no selection) | +$555,211 | +$130,638 |

Rolling DID capture more recoveries ($56,674 vs deployed-32's $7,988) — confirms the qualitative hypothesis that a rolling policy sees recovery-cohort symbols earlier. **But it underperforms the fixed deployed-32 by $398k (-50%) over 4.25yr.** Why: in 2021-Q1 the trailing-4Q window only qualifies 6 symbols; in 2021-Q2 only 12. Quarterly turnover is 17-42% in early years (Jaccard similarity). Stability matters more than recovery-capture for a 4-quarter lookback.

**Conclusion:** the recoveries cohort is a real blind spot, but a naive rolling shortlist is not the fix. Possible refinements (untested): longer lookback (8 quarters), hybrid (train-only base + add-only-after-N-positive-quarters), or minimum-trades thresholds. For now: **stick with deployed-32 and accept the recoveries gap** (~$200k of test PnL we structurally don't capture, ~$87k/yr forgone at slip=15).

Outputs: `scripts/p4_quarterly.sh`, `scripts/rolling_shortlist.py`, `results/p4_quarterly_slip15_2026-05-06.tsv`, `results/rolling_shortlist_diagnostic_2026-05-06.txt`.

### Implications

1. **Don't reshuffle the running 32.** Look-ahead is a property of the backtest test_NET, not a property of forward data. Reshuffling now changes nothing in expected forward PnL.
2. **Anchor expectations to the honest annual.** Forward-paper criteria should compare live PnL to ~$69k/yr at slip=25bp, not to the inflated $129k/yr deployed-claim or the $147k/yr all-57 headline.
3. **Strategy itself survives** — sign of test_NET stays positive under every honest selection rule at every slip level. The look-ahead inflated magnitude, not direction.

## Cost-survivor battery (2026-05-05)

A 21-cell falsification battery run on 2026-05-05 to test whether P4-Combined is a real edge or fee-illusion. Outputs in `results/battery_2026-05-05/`. **Verdict: real edge under realistic slippage, dies at extreme slippage.**

### Slippage stress matrix (5y × 57 sym, fee=10bp, CSV funding)

| Variant | slip=5bp | slip=15bp | slip=25bp | slip=40bp |
|---|---:|---:|---:|---:|
| **P4-Combined** (shorts+336h+CSV) | **+$1,051,624** | **+$710,280** | **+$368,936** | **−$143,079** |
| Max-hold-only (no shorts) | +$1,012,337 | +$413,307 | −$185,724 | −$1,084,270 |
| **P4-Shorts-Only** (no max-hold) | +$661,612 | +$414,798 | +$167,984 | −$202,237 |
| Longs-Combined (sanity flip) | +$30,591 | −$363,801 | −$758,194 | −$1,349,782 |

**Findings:**
- P4-Combined survives to slip=25bp (+$369k = $74k/yr). Cliff at slip=40bp.
- Max-hold-only is **fragile** — looks tied at slip=5 but its 13k-trade volume amplifies every cost shock.
- Shorts-Only is the **most slip-elastic** (5,809 trades = lowest fee/slip surface). The "boring" alternative.
- Longs-Combined (the inverse-side sanity check) is structurally negative across all slip levels — confirms the shorts-only filter is doing real work, not regime-fitting.

### Honest OOS — train-only target_rr selection

Sweep target_rr ∈ {3..8} on TRAIN ONLY (2020-2022), pick winner by train_NET, evaluate on TEST (2023-2025) once.

| target_rr | train_NET | test_NET | train#prof | test#prof | ρ |
|:---:|---:|---:|:---:|:---:|---:|
| 3 | +$145k | +$314k | 31/57 | 37/57 | 0.079 |
| 4 | +$190k | +$450k | 32/57 | 41/57 | 0.090 |
| 5 | +$308k | +$542k | 29/57 | 41/57 | 0.123 |
| **6** | **+$465k** 🥇 | **+$620k** | **36/57** | **47/57** | 0.242 |
| 7 | +$352k | +$680k | 31/57 | 42/57 | 0.300 |
| 8 | +$412k | +$861k | 31/57 | 46/57 | 0.373 |

**Findings:**
- **Train-only pick was rr=6** — the same value the original (full-5y-contaminated) sweep chose. Test confirmed +$620k. The OOS contamination caveat is partially defused.
- **All six rr values are positive in BOTH halves** — robust plateau, not knife-edge optimum.
- rr=8 has higher test_NET (+$861k) than rr=6 (+$620k) and higher Spearman; could be Strategy A-prime, but rr=6 is the honest train-pick. Spearman ρ rises monotonically with rr (0.08 → 0.37): higher RR → more symbol-persistent.

### Concentration cuts (analytical, from `proto_oos_combined`)

| Cut | NET (train+test sum) | Profitable |
|---|---:|---:|
| Full 57 sym | +$1,085k | 46/57 |
| **Drop top-5 winners** (MKR, GRT, ROSE, ENS, AVAX) | **+$751k** | 41/52 |
| Drop top-10 winners | +$511k | 36/47 |
| **Drop 8 late-listings** (PYTH, WLD, TIA, SUI, SEI, GMX, BLUR, ARB) | **+$926k** | 38/49 |
| Drop both top-5 AND late-listings | +$561k | ~33/44 |

**Findings:**
- 31% concentration in top-5, but the remaining 52 symbols still produce +$751k. Strategy isn't 5 lucky picks.
- Late-listings inflated test by $159k. **Honest framing: train ≈ test ($465k vs $461k after late-listing exclusion)**, not "test > train."

### Caveat resolution

| Original caveat | Status |
|---|---|
| OOS contamination at parameter level | ✅ Defused — train-only rr sweep independently picks rr=6 |
| Top-5 concentration ≈ 31% | ✅ Defused — +$751k remains after dropping top-5 |
| Late-listing inflation | ✅ Defused — train ≈ test after exclusion |
| 5bp slip optimistic | ✅ Defused — survives slip=15 and slip=25 |
| Three "orthogonal levers" not ablated | ✅ Defused — single-lever ablation + slip stress shows shorts is load-bearing under realistic friction |
| "Funding 0" framing oversells | ✅ Acknowledged — funding is neutral, not a profit lever |
| Shorts-only regime-conditioned | ✅ Defused — longs-combined is structurally negative across all slip levels |
| No baseline comparison | ✅ Defused — longs-combined as inverse baseline confirms shorts-side edge |
| Max-hold force-close skips slippage (`stub.go:236`) | ⚠️ **Still open** — Strategy B (no max-hold) is the hedge against this |
| 6s REST polling lag unmodeled | ⚠️ **Still open** — live-execution risk, not testable in backtest |
| Funding-CSV silent fallback | ⚠️ **Audit pending** — verify all 57 symbols loaded a Historical provider |
| Funding-CSV staleness for forward trades | ⚠️ **Architecture decision** — accept zero-funding-past-CSV-end, or build live fetcher |
| `tradeResult.fundingUSDT` field comment is stale | 📌 Minor doc-only fix — not load-bearing |

## Candidate Pool — P4-Combined (post-fees, $1k stake, continuous 5y)

Source: `results/p4_combined_2026-05-05.txt` and `results/proto_oos_combined_2026-05-05.txt`. Flags: `--exact-fills --include-boundary --pessimistic-ambiguous --fee-bps 10 --stop-slippage-bps 5 --funding-bps-per-day 0 --tax-rate-pct 0 --funding-csv-dir data/funding --signal-tf 4H --target_rr 6.0 --side-filter short --max-hold-hours 336`.

**Aggregate (57 symbols):** NET **+$1,051,624**, 7,772 trades, 20.64% WR, 45/57 profitable. Long_NET=$0 (filtered out), Short_NET=+$1,051,624. After 30% tax estimate ≈ $735k / 5y ≈ $147k/year.

**OOS split (train 2020-2022 / test 2023-2025):** train +$465k (36/57 profitable) → test +$620k (47/57 profitable). 32/57 persistent winners (positive both halves). Spearman ρ=+0.243 (weak — implies symbol selection is mostly noise; the strategy itself carries the edge).

**Deploy candidate — 32 OOS-validated persistent winners** (positive train AND test under post-fees stack):

ROSE, MKR, GRT, 1INCH, ADA, KAVA, 1000SHIB, ENS, XLM, ETC, RUNE, AVAX, IMX, DOT, BCH, FTM, FIL, SOL, CRV, AAVE, APT, SNX, NEAR, APE, MANA, AXS, GALA, ETH, ENJ, LINK, VET, LDO.

**Top-5 PnL contributors (concentration risk):** MKR +$79k, GRT +$69k, ROSE +$66k, ENS +$58k, AVAX +$58k. Sum ≈ $330k = ~31% of NET. Two regime flips erase half the alpha.

**Persistent losers — DO NOT deploy:** BTC −$69k, TRX −$62k, XRP −$38k, SAND −$36k, UNI, CHZ. Negative in both train and test.

**Regime-flip casualties** (positive train, negative test under post-fees stack — exclude): DYDXUSDT, ICPUSDT, DOGEUSDT, INJUSDT.

**Late-listing recoveries** (no train data; test-only): WLD, TIA, SUI, SEI, PYTH, GMX, BLUR, ARB, IOTA, BNB, OP, HBAR, LTC, ZIL, ATOM. Positive test-only signal but zero pre-2023 evidence — treat as exploratory, not validated.

**REST-poll capacity ceiling:** 32 symbols at 6s polling = 3200 weight/min — exceeds Binance Futures Regular ceiling (2400 weight/min). Either rotate, raise poll interval to ≥8s, or deploy a subset (~22 symbols max at 6s).

## Migration prerequisites — what's needed before P4-Combined can run live

The live wiring lags the backtest. Each item below must be resolved before any forward-paper-validation clock starts.

1. ~~**Wire P4 fields into `cmd/engine/main.go`.**~~ ✅ **DONE 2026-05-05.** Added CLI flags: `--fee-bps`, `--stop-slippage-bps`, `--funding-bps-per-day`, `--side-filter`, `--max-hold-hours`, `--funding-csv-dir`, `--target-rr` (overrides YAML when >0), `--signal-tf` (overrides YAML when set). Defaults preserve legacy behavior; passing flags activates Strategy A/B. Funding-CSV loader mirrors cmd/backtest:130-144.
2. ~~**Decide flag plumbing.**~~ ✅ **DONE — chose CLI flags via systemd ExecStart.** Mirrors cmd/backtest. Strategy params visible in unit file, single place to change. Per-symbol YAMLs unchanged. `deploy/systemd/paper-live@.service` now has Strategy B as default ExecStart with Strategy A as a commented one-line switch.
3. ~~**Ship `data/funding/*.csv` to VPS.**~~ ✅ **DONE — `deploy/sync.sh` updated.** Funding CSVs now ship as a separate rsync after the main sync (data/ is otherwise excluded due to market-data CSV size). Refresh cadence: re-run `scripts/download_funding.sh` periodically, then `./deploy/sync.sh`. Binance appends funding history every 8h.
4. **Resolve forward funding-CSV staleness.** The CSVs end at the last historical fetch. Live trades held past CSV-end get `$0` funding charges. **Recommendation: accept the limitation for now** (backtest's net-funding ≈ $0 over 5y suggests funding is not load-bearing), monitor in forward-paper, build live fetcher if a regime spike materializes. Decision: **OPEN, recommend accept-and-monitor.**
5. **Pick deployment symbol set under REST-poll capacity.** P4-Combined candidate is 32 OOS-validated names. 32 × 6s polling = 3200 weight/min > 2400 Binance Regular cap. Options: (a) deploy 22-symbol subset at 6s (=2200 weight/min), (b) deploy 32 at 8s polling, (c) split into two engine groups. **Decision: OPEN — needs your input.**
6. ~~**Migrate per-symbol YAMLs.**~~ ✅ **AVOIDED — chose CLI override path.** Per-symbol YAMLs untouched (still `target_rr: 5.0`). `--target-rr 6.0` in systemd ExecStart overrides at runtime. Reduces churn (16 file edits avoided) and keeps strategy params in one place (the systemd unit). YAMLs revert to symbol-level data only.
7. ~~**Run the Phase 2 ablation grid first.**~~ ✅ **DONE 2026-05-05.** See `## Cost-survivor battery (2026-05-05)`. Ablation + slippage stress + honest-OOS confirmed P4-Combined as Strategy A and P4-Shorts-Only as Strategy B fallback.

**Status (2026-05-05 20:06 UTC):** all 7 items resolved. Item 4 (forward funding-CSV staleness) accepted as-is per backtest's near-zero net funding finding — monitor in forward-paper. Item 5 (deployment symbol set) chose option (b) — all 32 OOS-validated names at 6s polling, accepting fallback-storm risk for paper. **Engines deployed and running.**

Restoration command (rolls back today's stop):
```bash
ssh root@178.105.24.230 'systemctl enable --now paper-live@btcusdt.service paper-live@ethusdt.service ...'
ssh root@178.105.24.230 'systemctl enable --now paper-live-watchdog.timer paper-live-digest.timer'
```

## Fresh OOS validation (2026-05-06)

After today's symbol-list consolidation + test coverage closure, ran a true out-of-sample test on data the strategy had **never** been selected against. The original cost-survivor battery, train-only-shortlist diagnostic, and per-quarter sweep all used 2020-01 → 2025-04. The 12 months from **2025-05 → 2026-04** are pure OOS — selected for, never tested against, never tuned to.

**Headline (universe-57, fresh 12-month window):**

| Slip | Backtest annualised | Fresh 12mo NET | Gap |
|---:|---:|---:|---:|
| 5bp | +$210k/yr | **+$24k** | −89% |
| 15bp | +$142k/yr | **−$82k** | sign flip |
| 25bp | +$74k/yr | **−$187k** | massive loss |

WR fresh: 18.91% (shorts) vs 20.64% backtest. Below the 14.3% breakeven WR for cost-laden long-side trades.

**Direction sweep (fresh OOS @ slip=15bp):**

| Side filter | NET | Profitable sym | WR |
|---|---:|:---:|:---:|
| shorts (deployed) | −$82k | 24/56 | 18.91% |
| longs only | **−$462k** | 11/56 | 14.61% (below breakeven) |
| both sides | −$438k | 20/56 | 16.93% |

**Critical interpretation:** longs-only being **worse** than shorts rules out the "2025-2026 was a bull regime that hurt shorts" hypothesis. If the issue were regime, longs would have made money. Instead, the signal failed in both directions. **This is alpha decay, not regime change.**

**Per-symbol category breakdown (57 symbols, comparing backtest annualised vs fresh 12mo, slip=15):**
- **REVERSAL** (sign change): 25 symbols (44%) — including former winners CRV, MKR, BCH, LDO, ENJ, GMX
- **DECAYER** (>50% deterioration, same sign): 8 symbols
- **maintainer** (within ±50%): 14 symbols — includes 5 of deployed-16 (ROSE, 1INCH, AAVE, DOT, RUNE)
- **IMPROVER** (>50% better): 10 symbols — TIA (+1107%), HBAR (+3477%), APE, OP, DYDX, ETC, XLM, BLUR, APT, PYTH

**Deployed-16 specifics (fresh 12mo @ slip=15):**
- Aggregate: **+$47,595** (positive but 37% of backtest expectation $127k/yr)
- Strong: ETC (+$23k), XLM (+$23k), ROSE (+$16k), 1INCH (+$12k), APT (+$12k)
- Marginal: ENS (+$4k), RUNE (+$4k), DOT (+$5k), 1000SHIB (−$0.2k)
- Loss: BCH (−$20k!), GRT (−$8k), KAVA (−$7k), FIL (−$7k), ADA (−$5k), AVAX (−$4k), IMX (+$0.7k)

The selection captured SOME signal (deployed-16 still positive while universe is negative) but with major bad picks (BCH, GRT, KAVA all turned negative).

**Symbols we EXCLUDED that worked in fresh OOS — i.e. opportunities the train-only filter missed:**
- TIAUSDT (+$22k) — was a "late-listing recovery" excluded by train-only filter
- HBARUSDT (+$14k) — recovery
- APEUSDT (+$15k) — train-rank too low
- DYDXUSDT (+$14k) — regime-flipper

These are exactly the "structural blind spots" the train-only-shortlist diagnostic flagged. Forward data confirms they contained real alpha.

**Symbols we EXCLUDED that confirmed losers (correct exclusions):** BTC (−$16k), TRX (−$17k), ENJ (−$17k), INJ (−$14k).

**What this implies for the deployed forward-paper system:**
- The 16 paper engines continue running on VPS (free observational data)
- The original ~$69-184k/yr forward-PnL expectation no longer holds — fresh evidence suggests **+$48k/yr ≈ true expected value at slip=15bp**, with high variance
- The 60-day go/no-go criteria need updating: against the new ~$48k/yr expectation, the deployed-16 should be net-positive within 30-60 days if the fresh-OOS pattern repeats. If it goes net-negative for 30+ days, kill the strategy entirely.
- Real-money deployment as previously framed is no longer indicated. If forward-paper passes the new (lower) bar over 60 days, *then* consider a small real-money tranche.

**Methodology notes:**
- Fresh window slip=15bp is a strict comparison: backtest at slip=15bp showed +$710k/5y = +$142k/yr; fresh 1y showed −$82k.
- 12 months is one regime sample — high variance. Five 12-month windows would tell us more, but we only have one. Treat the magnitude as indicative; treat the SIGN as load-bearing.
- The fresh window includes some delisted symbols (MKR, FTM) with partial data; their contribution is included for completeness.
- Per-symbol breakdown in `results/fresh_oos_2025-05_to_2026-04_slip15_2026-05-06.txt`. Side-filter variants in `results/fresh_oos_{longs,both}_slip15_2026-05-06.txt`. Comparison logic: `scripts/fresh_oos_compare.py`.

## Post-failure rescue (2026-05-06)

After the fresh-OOS finding showed P4-Combined at deployed parameters lost money, ran two rescue analyses:

### Analysis A: Persistent-alpha subset

Identified symbols positive in BOTH the original 5y backtest AND the fresh 12mo OOS (i.e. symbols where the 4H EMA-cross edge survived in the recent year). 20 of 57 symbols qualified.

| Set | n | Backtest annualised | Fresh 12mo @ slip=15 | Fresh 12mo @ slip=25 |
|---|---:|---:|---:|---:|
| Universe-57 (current) | 57 | $204k/yr | −$82k | −$187k |
| **Persistent-20** | **20** | $104k/yr | **+$202k** | **+$171k** |
| Deployed-16 (current) | 16 | $127k/yr | +$48k | +$16k |
| **Persistent ∩ Deployed-16** | **9** | $67k/yr | **+$100k** | **+$84k** |

The persistent-20 set captured 194% of its backtest expectation in fresh OOS — meaning the symbols that had real alpha in 2020-2025 maintained it (or even improved on the deployment-grade 12mo window). The deployed-16 underperformed because 7 of its 16 picks (BCH, GRT, KAVA, ADA, AVAX, FIL, 1000SHIB) were "dead in fresh OOS" — they had backtest alpha but fresh-OOS losses.

The 9 deployed-16 symbols that DID persist (1INCH, APT, DOT, ENS, ETC, IMX, ROSE, RUNE, XLM) produced +$100k on fresh OOS — double the deployed-16 aggregate. **Dropping the 7 dead picks alone would have improved capture from 37% to 148% of backtest expectation.**

11 symbols in the persistent-20 are NOT in deployed-16 (TIA, HBAR, APE, DYDX, AAVE, PYTH, WLD, IOTA, SNX, OP, BLUR). Adding them would contribute another +$103k on fresh OOS. Several were excluded by the train-only-shortlist diagnostic as "structural blind spots" (recoveries, regime-flippers) — fresh OOS confirms they have real alpha.

**Methodological caveat:** the persistent-20 subset was selected using fresh-OOS data. Forward-deploying it carries the same look-ahead bias category we measured this morning. The honest framing is: a NEW selection methodology that incorporates rolling OOS validation could legitimately deploy this subset, but the result must be re-validated on the NEXT period of fresh OOS (call it 2026-05 → 2027-04 for the next cycle).

### Analysis B: Alternate timeframe sweep

Backtest comparison across signal timeframes (per CLAUDE.md `## Timeframe sweep`) ranked 4H as optimal in 2020-2025. Fresh OOS shows the ranking has changed.

| TF | Backtest 5y NET @ slip=15 | Fresh 12mo NET @ slip=15 | Fresh WR | Fresh Trades |
|---|---:|---:|:---:|---:|
| 1H | −$6.10M | −$1,508k | 16.4% | 6,345 |
| 2H | −$1.82M | +$9k | 19.4% | 3,594 |
| **4H** (deployed) | **+$710k** | −$82k | 18.9% | 1,983 |
| **1D** | **+$82k** | **+$57k** | **32.3%** | **248** |

**1D timeframe is the only configuration that maintained positive expected value in fresh OOS without symbol-selection look-ahead.** Trade rate is much sparser (~4.4 trades/symbol/year vs 4H's 35), but the much higher WR (32.3% vs 18.9%) more than compensates per-trade.

Pattern: **coarser timeframes preserved alpha, finer ones decayed**. Suggests the EMA-cross edge has been arbitraged out at short horizons (more retail/algo participants running similar 4H-or-shorter signals) but 1D is slow enough to retain edge. This is consistent with alpha decay being microstructure-driven rather than regime-driven.

### Combined: persistent-20 on 1D

Tested as a possible "best of both" but 1D's sparsity (248 trades / 12mo / 56 sym = ~4.4 trades/sym/yr) means the persistent-20 subset only generates 90 trades total → +$26k. The smaller universe-57 1D had higher absolute PnL because more symbols accumulated more trades. **For 1D, broader selection seems better than narrower; for 4H, narrower is better than broader.**

### Recommendations — RETRACTED

The original commit (`fc65e81`) of this section recommended four "deploy paths" — persistent-20, drop-7-dead-picks, persistent-∩-deployed-16, and 1D-universe — with conviction levels. **Those recommendations were data-mined selections from a single 12-month OOS window.** They tell us nothing about forward edge. Striking them.

**What the per-symbol data above actually shows** (without selection):
- 25 of 57 symbols sign-reversed between backtest and fresh OOS — variance is enormous
- The DEPLOYED-16 selection methodology (train-only top-K with slip-25 robustness gate) captured 37% of expectation in fresh OOS — significantly underperformed but not catastrophically negative
- Some symbols had highly stable performance (ROSE, XLM, ETC); most had highly variable performance
- These observations suggest the 4H signal HAS some edge per-symbol, but the edge is heavily contaminated by symbol-specific variance

**The right operational stance:**
- **Do not re-select the deployed shortlist based on fresh-OOS results.** That's exactly the look-ahead bias we measured this morning at +26-70%, applied a second time.
- **The 1D timeframe finding stands as suggestive** (independent backtest support + one fresh OOS) but is NOT validated — see `## Walk-forward validation framework (2026-05-06)` below for the proper test.
- **Forward EV remains unknown.** The system has produced one negative OOS result. We need additional non-overlapping OOS windows before any deployment claim can be made with confidence.

### Operational note

Real-money deployment is **NOT supported** by current evidence. The original $86-184k/yr expectation is anchored to a backtest that produced a negative fresh OOS. The "rescue" findings do not constitute additional evidence — they are post-hoc selection. Real-money sizing remains: zero, until walk-forward validation produces convergent positive results across multiple windows.

Files:
- `scripts/persistent_alpha.py` — analysis A reproducible
- `results/persistent_alpha_2026-05-06.txt` — analysis A output
- `results/fresh_oos_{1H,2H,1D}_slip15_2026-05-06.txt` — analysis B raw data
- `results/fresh_oos_1D_persistent20_slip15_2026-05-06.txt` — combined analysis

## Walk-forward validation framework (2026-05-06)

After the morning's "alpha decay" finding (n=1 OOS window) and the retracted "rescue" recommendations (post-hoc selection), built a proper walk-forward framework: test the SAME deployed configuration across MULTIPLE non-overlapping 12-month windows, no symbol re-selection between windows. Output: `results/walk_forward_{1H,2H,4H,1D}_short_rr6.0_slip15_2026-05-06.txt`, comparison `results/walk_forward_comparison_2026-05-06.txt`. Reproduces via `scripts/walk_forward.sh` + `scripts/walk_forward_compare.py`.

### Setup

| Window | Range | Status vs original backtest (2020-01 → 2025-04) |
|---|---|---|
| W1 | 2023-05 → 2024-04 | **In-sample** time slice (universe was selected over the whole 5y) |
| W2 | 2024-05 → 2025-04 | **In-sample** time slice |
| W3 | 2025-05 → 2026-04 | **True OOS** — the "fresh OOS" window from earlier today |

**Critical caveat:** the universe-57 was selected by knowing what worked over the full 2020-01 → 2025-04 backtest, so W1 and W2 carry symbol-selection look-ahead. They are useful as VARIANCE references, not as independent validation. Only W3 is a clean OOS sample. Walk-forward with truly OOS windows would require either waiting for forward time (one window per year of patience) or using a rolling-train-then-test design that reselects per window — out of scope for this iteration.

### Results — universe-57, target_rr=6.0, side=short, slip=15bp, fee=10bp

| TF | W1 (in-sample) | W2 (in-sample) | W3 (true OOS) | sum 3yr | mean/yr | pos/3 | Verdict |
|---:|---:|---:|---:|---:|---:|:---:|---|
| 1H | −$2,122k | −$1,653k | −$1,508k | **−$5,283k** | −$1,761k | 0 | REJECTED |
| 2H | −$508k | −$413k | +$9k | **−$912k** | −$304k | 1 | REJECTED |
| **4H** | +$15k | +$504k | −$82k | **+$437k** | **+$146k** | 2 | SUPPORTIVE |
| **1D** | −$38k | +$65k | +$57k | **+$85k** | +$28k | 2 | SUPPORTIVE |

### Findings

1. **1H/2H REJECTED across 3 windows including in-sample.** No alpha at finer timeframes. This is consistent with the prior 5y backtest finding and the timeframe sweep — confirms with multi-window evidence that fine-TF EMA crossover is fee-killed.

2. **4H mean across 3 windows: +$146k/yr — almost exactly matches the original backtest annualised expectation of +$142k/yr at slip=15bp.** This is meaningful: the deployed configuration's *expected* economics held up across slices, even though the realized PnL has very high per-window variance ($-82k to $+504k).

3. **The morning's "alpha decay" framing was n=1 evidence.** With n=3, the W3 (-$82k) result is one losing window in a SUPPORTIVE configuration, not a categorical decay verdict. This DOES NOT mean the strategy is forward-validated — variance is enormous and only one window is true OOS — but the doomsday read was overstated.

4. **1D maintains positive mean across 3 windows** (+$28k/yr) including the true OOS W3. Lower magnitude than 4H but with two consecutive recent positive windows. The 1D result has independent backtest support (+$82k over 5y), the timeframe sweep position, AND walk-forward consistency. This is the strongest multi-evidence case for any timeframe.

5. **The strategy is highly variant under realistic costs.** 4H per-window range $-82k to $+504k illustrates why single-window OOS results — in either direction — are weak evidence. A pessimist looking at W3 would kill the strategy; an optimist looking at W2 would over-allocate. Both reads ignore the variance.

### Deployed-16 walk-forward (2026-05-06 follow-up)

The above used universe-57. The actually-running engines are on the deployed-16 subset (train-only top-K + slip-25 robustness gate). Re-ran walk-forward on the deployed-16 to test whether the selection is operationally meaningful or just adds noise.

| TF | W1 (in-sample) | W2 (in-sample) | W3 (true OOS) | sum 3yr | mean/yr | pos/3 | Verdict |
|---:|---:|---:|---:|---:|---:|:---:|---|
| **4H deployed-16** | −$5.7k | +$219k | **+$48k** | +$261k | **+$87k** | 2 | SUPPORTIVE |
| 4H universe-57 | +$15k | +$504k | −$82k | +$437k | +$146k | 2 | SUPPORTIVE |
| **1D deployed-16** | −$4.8k | +$21k | **+$36k** | +$52k | +$17k | 2 | SUPPORTIVE |
| 1D universe-57 | −$38k | +$65k | +$57k | +$85k | +$28k | 2 | SUPPORTIVE |

**Critical observation:** the deployed-16 subset POSITIVE on the true OOS W3 (+$48k at 4H, +$36k at 1D) — opposite sign from the universe-57 W3 result (−$82k at 4H). The selection's slip-25 robustness gate filtered out some of the W3 losers from the universe.

**Caveat — this could be data mining:** the deployed-16 was selected with knowledge of 2020-2025 backtest data. The fact that it happened to also do well on the never-seen W3 is suggestive but not conclusive. With one true-OOS window we can't tell if this is "the selection captured robust signal" or "the selection got lucky on W3 specifically". Need more truly-OOS windows (i.e. calendar time) to distinguish.

**Magnitude trade-off:** deployed-16 has 60% the mean PnL of universe-57 (4H: $87k vs $146k/yr) but appears more robust on W3. This is consistent with the original selection rationale — exclude high-variance/high-mean symbols in favor of robust-in-train ones.

**Operational implication:** the running 16 engines have a SUPPORTIVE walk-forward verdict on both 4H (deployed config) and 1D (alternative). Trade rate calibration: 4H deployed-16 averages ~1.5 trades/day across all 16 (534/546/571 trades per 12-month window ÷ 365 days). At 1D it's ~0.18 trades/day — roughly one signal every 5-6 days across the entire deployed universe.

### Slip-stress walk-forward (2026-05-06 follow-up)

After the slip=15 baseline, ran 4H and 1D at slip=5 and slip=25 across the same 3 windows to test cost-robustness across windows (not just on the full 5y as the cost-survivor battery did).

| TF | slip=5 | slip=15 | slip=25 |
|---:|---|---|---|
| **4H** | 3/3 pos, +$729k sum, **STRONG** | 2/3 pos, +$437k, SUPPORTIVE | 1/3 pos, +$146k, **REJECTED** |
| **1D** | 2/3 pos, +$98k, SUPPORTIVE | 2/3 pos, +$85k, SUPPORTIVE | 2/3 pos, +$72k, SUPPORTIVE |

**4H is slip-fragile.** STRONG at 5bp → SUPPORTIVE at 15bp → REJECTED at 25bp. The cost-survivor battery (full-5y, slip=25 +$369k) hid the per-window distribution: at slip=25 only W2 (in-sample, +$418k) carries the entire +$146k sum; W1 and W3 are both negative. So the deployed config's robustness depends on realized slippage landing closer to 5bp than to 25bp.

**1D is slip-robust.** SUPPORTIVE at every slip level tested. Per-window magnitudes barely move ($24k → $33k as slip drops 25→5bp, vs 4H's $49k → $243k swing). 1D's lower per-trade cost surface (5x fewer trades, wider stops) absorbs cost shock far better than 4H.

**What this means for sizing:**
- Slip is a load-bearing assumption for 4H. If realized live slip exceeds 15bp the 4H deployment likely has no edge under multi-window scrutiny.
- 1D would be a more conservative deployment alternative — lower magnitude (~$28k/yr expectation) but doesn't depend on hitting low-slip targets.
- The cost-survivor battery's slip=25 finding was inflated relative to walk-forward because the 5y aggregate concealed the W1/W3 losses under the W2 win.

### What this changes vs the morning panic

- **Doesn't change:** the deployed 4H configuration is NOT forward-validated, true OOS sample is still n=1, real-money allocation remains unjustified.
- **Does change:** the framing from "alpha decayed, kill the strategy" to "the strategy has very high variance with one losing OOS window in a 3-window sample — insufficient evidence to deploy or to kill". The mean matches expectation; we just need more OOS windows (i.e. more time) to know.
- **Practical implication:** the running 16 paper engines should continue collecting forward data. The 60-day go/no-go criteria below remain the operative test. Walk-forward provides a stricter yardstick than fresh-OOS-only and accepted the deployed configuration provisionally.

### What walk-forward CANNOT tell us

- Whether the W3 negative is a transient or a regime change. Need W4 (2026-05 → 2027-04) when it exists.
- Whether the 1D edge is real or a recovery from arbitrarily-bad picks at finer TFs. Same answer: more time.
- Whether ANY symbol-selection rule applied today would have forward edge. Symbol selection is downstream of strategy validation; if the strategy itself is uncertain, selection is doubly uncertain.

### Methodological discipline reinforced

- **Don't trust n=1 OOS results in either direction.** Today proved this works both ways: morning panic (W3 negative → "decay") and morning rescue (post-hoc subset selection → "rescue capture") were both wrong for the same reason — single-sample inference under high variance.
- **Symbol re-selection from OOS data is data mining**, even when it sounds like "we found the alpha that survived" — there are too many subsets for any to mean anything without forward validation.
- **Walk-forward with truly OOS windows requires patience.** We have one window per year of waiting. The framework is designed to ACCUMULATE evidence over years, not to provide an instant verdict.

### Files

- `scripts/walk_forward.sh` — multi-window runner (env-configurable TF/side/RR/slip)
- `scripts/walk_forward_compare.py` — aggregates per-config results into comparison matrix
- `results/walk_forward_{1H,2H,4H,1D}_short_rr6.0_slip15_2026-05-06.txt` — per-config raw output
- `results/walk_forward_{4H,1D}_short_rr6.0_slip{5,25}_2026-05-06.txt` — slip-stress raw output
- `results/walk_forward_comparison_2026-05-06.txt` — combined 8-cell comparison matrix

## Mechanism analysis (2026-05-06) — what the edge actually IS

After establishing walk-forward results, ran a mechanism analysis to test what causes the apparent edge. Output: `scripts/mechanism_analysis.py`, `results/mechanism_analysis_2026-05-06.txt`. Tested four hypotheses:

- **H1** (vol-driven momentum): edge concentrates in high-vol altcoins
- **H2** (retail-bubble shorting): edge in retail-heavy/recent-listing names
- **H3** (selection-bias noise): per-symbol "edge" is largely noise
- **H4** (structural bear-bias): crypto has universal short-side skew

### Finding 1 — H3 is STRONGLY SUPPORTED: per-symbol "skill" is statistically indistinguishable from chance

Per-symbol consistency across 3 windows (positive in all 3 = "ROBUST"):

| category | observed | expected if independent | z-score |
|---|---:|---:|---:|
| ROBUST (3/3) | 8 | 9.5 | −0.52 |
| supportive (2/3) | 26 | 24.5 | +0.39 |
| noisy (1/3) | 20 | 18.9 | +0.30 |
| REJECTED (0/3) | 3 | 4.1 | −0.55 |

Under the null hypothesis that per-symbol returns are independent across windows with the observed marginal probabilities (W1=53%, W2=74%, W3=43% positive), the expected count of "ROBUST" symbols is 9.5 ± 2.8. We observe 8. **All four counts are within 1σ of chance expectation.**

Top-5 PnL contributors per window:
- W1: CRV, IOTA, ETH, GRT, AAVE
- W2: AVAX, PYTH, ARB, ETC, ENJ
- W3: ETC, XLM, TIA, ROSE, APE

**Top-5 overlap across all 3 windows: 0/5.** Only ETC appears in two windows (W2+W3). 48% of universe symbols sign-flip between consecutive windows.

**Implication:** there is NO statistically detectable per-symbol edge. The "deployed-16" selection — and any other shortlist drawn from backtest data — is fitting noise. This generalizes the morning's data-mining critique: it's not just "persistent-20 was post-hoc" — it's that NO shortlist selection captures real per-symbol edge under a proper statistical test.

### Finding 2 — H4 is STRONGLY SUPPORTED: shorts-side asymmetry is universal across 3 windows

| window | short NET | long NET | asymmetry | shorts pos/N | longs pos/N |
|---|---:|---:|---:|:---:|:---:|
| W1 (in-sample) | +$15k | **−$162k** | +$177k | 30/57 | 24/57 |
| W2 (in-sample) | +$504k | **−$19k** | +$523k | 42/57 | 32/57 |
| W3 (true OOS) | −$82k | **−$462k** | +$380k | 24/56 | 11/56 |
| **3-window aggregate** | **+$437k** | **−$643k** | **+$1.08M** | | |

**Longs are NEGATIVE in ALL 3 windows.** Even on W3 where shorts were also negative, the long counterpart was 5.7× more negative. The shorts-vs-longs asymmetry is +$1.08M aggregate over 3 years and present in every window — a structural property, not regime-conditioned.

**Implication:** the strategy's load-bearing edge is exploitation of crypto's structural short-side skew (likely retail-driven pump-and-fade dynamics). The wick stops + RR=6 + shorts-only stack captures this skew across vol regimes. This is a real, persistent, mechanistically-explainable edge — but it depends entirely on the underlying market structure persisting.

### Finding 3 — H1 partially supported with U-shape: edge concentrates in mid-vol altcoins

Universe binned into volatility terciles using annualized realized volatility from monthly OHLC over 2023-05 → 2026-04:

| bin | n | avg ann. vol | 3yr NET | sample symbols |
|---|---:|---:|---:|---|
| low_vol (majors) | 19 | 0.73 | **−$15k** | TRX, BTC, BNB, LTC, ETC |
| mid_vol | 19 | 1.02 | **+$332k** | APE, DOT, SNX, FIL, UNI |
| high_vol (alts) | 19 | 1.45 | **+$120k** | IMX, BLUR, RUNE, ENS, GALA |

Edge follows an inverted-U. Majors (BTC/ETH/BNB) lose money — they're too efficient or move too gently for the EMA-cross + wide-stop + RR=6 setup. Mid-vol altcoins are the sweet spot. Very-high-vol names get whipsawed through stops too often before reaching the 6:1 target.

**Implication:** the deployed universe of 57 includes 19 low-vol names that REDUCE expected PnL. A purely structural deploy could exclude majors a priori (not via train-data selection — that's data mining), keeping only mid+high-vol names. ~$15k/yr of headwind potentially recoverable.

### Finding 4 — Common factor exists but is moderate (not pure regime capture)

Pairwise symbol correlation across 3 windows: avg +0.16, median +0.30. 42% of pairs > +0.5 (strong common factor); 25% < −0.5 (anti-correlated). The strategy captures both a market-wide common factor AND substantial per-symbol noise.

### What this means for deployment

1. **The structural edge is real and load-bearing.** $1.08M shorts-vs-longs asymmetry across 3 windows is the strongest per-window-consistent finding in any test we've run. As long as crypto retains short-side skew, the strategy has expected value.

2. **The per-symbol selection is noise.** The deployed-16 list, the persistent-20 list, the universe-57 — they're all drawing the same structural edge from the same market. Selection adds variance, not edge. The deployed-16's W3 +$48k vs universe-57 W3 −$82k is luck on n=1, not skill.

3. **Position sizing should be uniform across the universe.** No symbol concentration (which adds variance without adding edge). The current deployed-16 is operationally easier (rate-limit headroom) but not statistically privileged.

4. **The structural edge could disappear if** crypto becomes more institutional (less retail FOMO → less shortable spikes), more algos run similar EMA strategies (arbitraged), or cost levels rise above ~15bp slip. None of these are predictable from backtest data.

5. **Forward expected value remains uncertain in MAGNITUDE but supported in SIGN.** The sign of the edge is structurally backed (universal across 3 windows + mechanistically explainable). The magnitude depends on which window-sized regime you draw from, which has range $-82k to $+504k for 4H universe-57 shorts.

### Methodological note

The random-baseline test (Finding 1) is a strong refutation of "we found the alpha symbols" framing. Any future strategy work should compare observed consistency against the chance baseline before claiming per-symbol skill. The morning's persistent-20 retraction is now reinforced with statistical evidence — symbol-level selection from backtest data is fundamentally fitting noise, regardless of which selection rule is used.

### Files

- `scripts/mechanism_analysis.py` — reproducible analysis (~150 lines, parses fresh_oos files)
- `results/fresh_oos_{2023-05_to_2024-04, 2024-05_to_2025-04}_slip15_2026-05-06.txt` — W1/W2 per-symbol shorts data (W3 already existed)
- `results/fresh_oos_{2023-05_to_2024-04, 2024-05_to_2025-04}_longs_slip15_2026-05-06.txt` — W1/W2 per-symbol longs data
- `results/mechanism_analysis_2026-05-06.txt` — full output

## Forward-paper go/no-go criteria

Forward-paper validation started 2026-05-05 20:06 UTC (32 Strategy B engines). The deployed-32 list backtested at +$129k/yr at slip=25bp, but the train-only-shortlist diagnostic shows +70% look-ahead inflation at that slip level. **Anchor expectations to the honest annual: ≈ $69k/yr at slip=25bp**, not the deployed-claim or all-57 headline.

### Statistical-power floor (before reading any signal)

P4-Combined backtests at WR 20.64% with 6:1 R:R. Breakeven WR ≈ 14.3% — only ~6.3pp cushion. To bound observed WR within ±10pp at 95% CI requires ~63 trades; ±5pp requires ~250 trades. Strategy B at 5,809/5y × 32/57 sym ≈ 1.8 trades/day → 60 days ≈ 108 trades = ~±7pp resolution. **Do not draw conclusions from < 60 calendar days of forward data.**

### Deploy real money (small tranche, 1/10th notional) only if ALL true

- ≥150 live trades accumulated
- Realized round-trip taker fees ≤ 12 bp (vs 10 bp modeled — 20% slack)
- Realized stop-side slippage ≤ 20 bp on the losing-trade subsample (vs 5-25 bp modeled range)
- ≥60 calendar days net-positive in dollar terms
- Live PnL ≥ 60% of pro-rated honest-annual ($69k/yr × elapsed-fraction × 0.60)
- Live PnL beats `BTC HODL with $32k notional` over the same window
- No single symbol contributes >40% of cumulative live PnL

### Kill the strategy if ANY true

- First 60 days net-negative
- Realized stop-side slippage > 25 bp (the cliff edge)
- Realized WR < 14% over ≥150 trades (below breakeven)
- Single symbol contributes >40% of live PnL (concentration risk realized)
- Train-only-shortlist diagnostic re-run on rolling forward data shows non-positive honest test
- Two consecutive 30-day windows underperform BTC-HODL benchmark by >$5k each

### Initial real-money sizing

Open with **$100/trade** (1/10th of backtest stake), not $1k. Cost of being wrong is bounded; cost of being right is just slower scaling. Promote to $1k/trade only after 6 months of forward evidence meeting all deploy-criteria.

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

After the 2026-05-05 cost-survivor battery and 2026-05-06 follow-up work, the original 13 caveats are now reduced to 3 still-open:

- **6 s REST polling lag is unmodeled.** Adverse on every entry and every stop. Live-only risk — not testable in backtest. Mitigation: monitor live vs backtest signal-to-execution gap during forward-paper.
- **Funding-CSV staleness drift.** Even with the new `refresh_funding.sh` mechanism, engines pick up refreshed CSVs only on restart. Between restarts, trades held past the CSV's last entry get $0 funding. Bounded by time-since-last-restart; weekly refresh + restart caps drift at ~7 days. Net funding ≈ $0 in steady state per backtest, so divergence is small in practice.
- **No real-money execution test.** Forward-paper with realistic position sizing, exchange position limits, margin reuse, and concurrent-trade interaction is unmodeled. Honest backtest projection is $69-184k/yr depending on slip (see `## Train-only shortlist diagnostic`); the original $74-142k/yr framing was based on the deployed-32 list which carries +26-70% look-ahead inflation.

**Resolved or downgraded since 2026-05-05:**
- ~~Max-hold force-close slippage (`stub.go:236`)~~ → measured 2026-05-06: only 5.8% of trades hit time_stop on RUNE; extrapolated impact is ~$3-17k over 5y depending on slip = 1-2% of Strategy A NET. **Cosmetic.** Documented in Known Bugs.
- ~~Funding-CSV silent fallback~~ → verified clean at deploy 2026-05-05 (all 32 engines logged "loaded historical funding"). The remaining concern is staleness, addressed above.
- ~~`tradeResult.fundingUSDT` comment was stale~~ → fixed 2026-05-06.
- ~~Symbol-selection look-ahead in deployed-32~~ → measured 2026-05-06 via train-only-shortlist diagnostic at slip ∈ {5, 15, 25} bp. Inflation is +26-70%, fixed dollar bias ~$115-124k on test, sign of test_NET preserved at every honest selection rule. **Quantified, not eliminated** — incorporated into go/no-go expectations.

## Dead Code

- `DailyLevels.LevelDirection` (`pkg/indicators/levels.go`) — defined but never called.
- `bias.Allows(side, false)` in `checkAbsorption` (`pkg/strategy/entry.go`) — always returns `true`; no-op. (Absorption path is not the live strategy but remains in code.)
