# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Strategy status (2026-05-06 EOS)

**Live:** 16 paper-trading engines on Hetzner VPS, each running 1 live + 2 shadow strategies.
- **Live config:** `--signal-tf 4H --side-filter short --target-rr 6.0 --max-hold-hours 504 --funding-csv-dir data/funding --fee-bps 10 --stop-slippage-bps 5` (EMA 9/21 hardcoded). Selected via 6-window walk-forward validation 2026-05-06.
- **Shadow A:** EMA 5/15 + mh336 (Cat A weak signal, forward A/B test).
- **Shadow B:** EMA 5/15 + mh504 (joint candidate).
- **Deployed shortlist:** 16 symbols in `configs/symbols.yaml:deployed`. Selection adds variance not edge per mechanism analysis — operationally constrained by Binance per-IP rate limit (16 × 10s polling × 20 weight/call = 1920 weight/min, cap 2400). Bumped from 6s on 2026-05-07 — see Bug 4.

**Validation status:** SUPPORTIVE walk-forward verdict (4H short EMA 9/21 mh504, 6 windows). Mean +$130k/yr with 95% CI [−$111k, +$372k] — **CI includes negative; uncertainty is irreducible from history alone.** Trade-level block bootstrap (5y × 16 sym × 2,210 trades, stationary bootstrap at L=√N=47) gives a much tighter CI [+$42k, +$220k]/yr, P(>$0)=99.9%, P(>$50k)=96.5% — but walk-forward CI is **2.8× wider**, confirming regime-variance (quarter-level) dominates trade-level autocorrelation (lag-1 ρ=+0.32). **Anchor expectations to walk-forward, not bootstrap** — bootstrap underestimates per-quarter regime swings. See `results/bootstrap_ci_verdict_2026-05-07.md`.

**Real-money allocation:** ZERO. Gated on forward-paper validation (≥150 trades, ≥60 days net-positive, see `## Forward-paper go/no-go criteria`).

## Today's findings (2026-05-06) — see `docs/findings/2026-05-06.md` for full narrative

Headline outcomes (memory entries cover specifics):
- Funding-loader bug fixed (was silently zeroing all historical funding)
- Walk-forward + mechanism + quarterly framework built and run
- Per-symbol skill REFUTED (0/57 pass Bonferroni, z=−0.52)
- mh504 adopted as live (6/6 wins vs mh336 in pre-registered 6-window test)
- Shadow mode deployed for 5/15 EMA candidate
- Cat A entry-mechanism alternatives REJECTED (VWAP fade, PDH/PDL break, RSI cross-50)
- Funding-regime correlation HOLDS (r=−0.17 at quarter level) but trade-level filter REJECTED
- 1D both-sides identified as slip-robust alternative deployment (3/3 walk-forward, +$63k/yr — not deployed today)

**Pre-registered decision rules archived in:** `results/6window_decision_rule_2026-05-06.md`, `results/cat_a_decision_rule_2026-05-06.md`, `results/vwap_decision_rule_2026-05-06.md`.

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

# Post-deploy operational health audit — RUN AFTER EVERY redeploy.sh
./scripts/post_deploy_check.sh
```

**Post-deploy validation is mandatory.** After ANY `deploy/redeploy.sh`, run `scripts/post_deploy_check.sh`. It verifies (1) all deployed engines systemctl-active, (2) watchdog timers armed, (3) deployed binary built from current source (md5 match), (4) per-engine tick freshness (no stale heartbeats), (5) no ERROR-level logs in last 5 min, (6) rate-limit pressure within healthy bounds. Run with `STRICT=1` to make it exit non-zero on any warning (CI-friendly). Designed to catch silent failures the way today's funding-loader bug went undetected — by asserting that each subsystem is producing output rather than just reporting "started successfully".

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

## Key Invariants

- **Exchange timestamps only.** `Candle` and `Tick` timestamps come from the exchange. `time.Now()` is never used for price data.
- **`DataSource` is the only seam between backtest and live.** `cmd/backtest` uses `CSVReplay`; `cmd/engine` uses `BinanceFutures`. Everything downstream is identical.
- **`expandKlineToTicks` is the canonical tick-expansion helper** (`pkg/marketdata/klines.go`). Both `CSVReplay` (backtest) and `BinanceFutures` (REST backfill on startup) call it so interpolation is identical in both paths. Do not inline a second copy.
- **`BinanceFutures` backfills 96h of 1m klines on startup, paginated across 4 calls** by fetching `GET /fapi/v1/klines?startTime=...&limit=1500` from `fapi.binance.com` before opening the WebSocket. Sized so 4H-signal indicators (EMA21, BB20) prime during backfill; without this, each restart cost ~64h of cold-start blindness — see Bug 5. Backfill failure (any page) is non-fatal — engine logs a warning and proceeds with whatever ticks were successfully pushed before the failure.
- **Live engine MUST call `Runner.SetLiveMode(true)`** to suppress signals from candles whose `CloseTime` is older than `backfillStaleness` (90s). Without this gate, paginated backfill (96h × 60 1m klines → 24 closed 4H candles) would prime EMA21 mid-backfill and emit a spurious "signal" at a stale historical close, opening a position at a price hours/days old. `cmd/backtest` leaves the gate off so historical CSV replay still produces signals — the gate is live-only.
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
- ~~**Bug 4 — REST poll interval × symbol count exceeded Binance weight cap**~~: `restPollInterval=6s × 16 symbols × 20 weight/call = 3200 weight/min` vs Binance per-IP cap `2400/min`. CLAUDE.md doc was wrong (claimed 1200–1600 weight/min by implicitly assuming the wrong weight/call). Fleet equilibrated at ~50% wall-time in 60s rate-limit backoff — confirmed 2026-05-07 by observing 30 × 429/hour/engine, identical counts across all 16 engines (per-IP synchronization), and 154 × 60s / 27829s = 0.332 backoff fraction averaged over the run (steady-state 0.5 in the last 5 hours). Fix: bumped to 10s on 2026-05-07. New steady-state load 1920 weight/min with 480 weight/min headroom. Operational consequence of the bug: ~13% of forward-paper signals at risk of 0–60s entry delay (boundary tick blocked by backoff) — bounds adverse-fill bias for the 7.5h pre-fix forward window. No trades fired in that window so impact = 0.
- ~~**Bug 5 — Cold-start indicator blind period of ~64 h per restart**~~: `BinanceFutures.backfill` silently truncated to 1500 klines (= 25h of 1m data) regardless of the `backfill_hours` config, because `if limit > 1500 { limit = 1500 }` capped without pagination or warning. Combined with `EMA.Primed()` requiring 21 samples + `prevEma21 != 0` requiring one more candle, a 4H-signal-tf restart needed 22 closed 4H candles before any signal could fire — backfill provided ~6 → live runtime had to accumulate 16 more × 4h = **64h of blind period per restart**. Across the deploy churn 2026-05-05 → 2026-05-07, the strategy was structurally incapable of firing a signal continuously, masking it as "low n / quiet market". Fix 2026-05-07: paginated `backfill()` walks forward in `startTime`-chained 1500-call pages and the `BackfillHours` default bumped 48 → 96; channel buffer sized dynamically. To prevent the spurious-historical-signal failure mode introduced by paginated priming (EMA21 now primes mid-backfill — a cross there would emit a signal at a stale close price), `Runner.evaluateEntry` gates on `time.Since(c.CloseTime) > 90s` when `liveMode=true`. cmd/engine sets liveMode=true on live + shadow runners; cmd/backtest leaves it false so historical CSV replay continues to emit signals. Recovered ~62h of forward-paper time per restart.


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
