# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

> # ⛔ PROJECT CLOSED — 2026-08-05
>
> **Nothing is running anywhere.** The Hetzner VPS (`178.105.24.230`) was
> **destroyed 2026-08-04**. All 16 engines, all crons (drift, digest, funding,
> weekly audit) and the local launchd timers are gone. **Zero real money was
> ever deployed, at any point.**
>
> Forward-paper was terminated early on 2026-08-04 at day 88 of ~112: **134
> trades, WR 17.9%, NET −$1,626.58**, costs **114% of gross**. Break-even was
> **8 bp**; the run paid **10 bp**. Both v2 revival routes closed NEGATIVE on
> 2026-08-05 (maker execution → adverse selection, −7.1pp winner-fill gap at
> n=2,882; wider stops → already answered 07-26/27). The perp EMA strategy
> class is **closed**; the search ended at N≈85 trials, PBO 0.52.
>
> **Everything below this banner is historical.** It describes a live system
> that no longer exists — every `ssh root@…`, `redeploy.sh`, `journal_fetch.sh`,
> drift-check and cron instruction is **inoperative**. Read it as the record of
> how the run was operated, not as instructions to follow.
>
> Start here instead: `results/INDEX.md` → `## Close-out 2026-08-04 → 08-05`,
> then `results/v2_lessons_and_design_2026-08-04.md` (the transferable lessons)
> and `results/viability_frontier_2026-07-27.md` (the screening rule that
> retroactively rejects every candidate this project ran).
>
> **Do not re-run Option A (maker) or Option B (wider stops)** — both are
> pre-registered and settled. Do not start a new search here.
>
> **Open security item:** a still-live Telegram bot token sits in this repo's
> git history (`deploy/install.sh` @ `0dfc9c5`). Current tree is clean and the
> repo is private; the token is shared with four other projects, so rotation is
> a coordinated decision. Documented and accepted, not fixed — see
> [`SECURITY_NOTE.md`](./SECURITY_NOTE.md).

## Current use (post-closure)

This repo is now a **research archive and method library**. The engine code still builds and backtests run against Binance's public API, but nothing is deployed.

- **Transferable method:** `docs/QUANT_METHOD.md` + `scripts/quant_honesty.py` (stdlib + numpy, `--selftest`). Copy into any new quant project.
- **Research spikes** happen here occasionally (see `results/INDEX.md` for the full catalog). Recent: broad instrument screen (27 tested, VIX calls sole survivor), crash overlay revival (4/4 pass, venue pending).
- **Build/test still works:** `go build ./...` and `go test ./...` pass. Backtests run via `go run ./cmd/backtest --config configs/btcusdt.yaml`.
- **Everything below is historical.** Operational commands (deploy, drift check, VPS SSH) target infrastructure that no longer exists.

**Operator-facing reference (HISTORICAL):** see `docs/OPERATOR_HANDBOOK.md` for the daily/weekly cadence, Telegram tier guide, and scenario playbook as they were when the run was live. **Both are historical as of 2026-08-05 — see the banner above.**

## Strategy status (2026-05-10 — HISTORICAL, see banner)

**Live:** 16 paper-trading engines on Hetzner VPS (1 live + **8 shadows** each). Journal-replay on startup prevents orphaned positions. KAVAUSDT + ENSUSDT additionally run a Layer 3 testnet shadow (see Layer 3 status below).
- **Live config:** `--signal-tf 4H --side-filter short --target-rr 6.0 --max-hold-hours 504 --funding-csv-dir data/funding --fee-bps 10 --stop-slippage-bps 5` (EMA 9/21). 6-window walk-forward validated.
- **Shadows (8, source: `deploy/systemd/paper-live@.service` `--shadow`):** `alt5-15-336` (EMA 5/15 mh336), `alt5-15-504` (5/15 mh504), `bb20` (Bollinger 20/2.0σ mh504 — only non-EMA), `alt5-21-504`, `alt7-14-504`, `alt10-30-504`, `alt12-26-504`, `alt21-50-504` (all EMA fast/slow mh504). The last 5 added 2026-05-26 (`ema_tf_exploratory_grid_2026-05-19`). All research-only, promotion-LOCKED. Shadows share the live engine's tick stream (zero extra REST weight).
- **Deployed:** 16 symbols in `configs/symbols.yaml:deployed`. Selection adds variance not edge. Rate-limit: 16 × 10s × 20w = 1920/min (cap 2400) — shadows add nothing (same in-process tick stream).

**Validation:** SUPPORTIVE walk-forward. Mean +$130k/yr, 95% CI [−$111k, +$372k]. Bootstrap CI is tighter but **anchor to walk-forward** (2.8× wider — regime-variance dominates). See `results/bootstrap_ci_verdict_2026-05-07.md`.

**Real-money:** ZERO. BinanceLive code complete. Gated on forward-paper criteria (below) + Layer 2 testnet (`--executor binance_live_testnet`) + Layer 3 7d shadow parity (`--layer3-binance-testnet-journal-dir DIR`, verdict via `scripts/layer3_verdict.sh`). Per `results/real_money_executor_architecture_decision_rule_2026-05-08.md`.

**Layer 3 status (2026-06-11):** Layer 3 IS running on KAVAUSDT + ENSUSDT live engines via systemd drop-ins (`paper-live@{kava,ens}usdt.service.d/layer3.conf`, per `results/layer3_enablement_2026-05-19.md`) — Stub primary + BinanceLive(testnet) shadow on same ticks. Two zero-data incidents so far: (1) reconciler had no 418/429 backoff, self-extended a testnet IP ban 2026-05-19→29 (fixed: backoff + `post_deploy_check §8b` / `weekly_audit` Stage 9 guards); (2) the first-ever signal to reach the shadow (KAVAUSDT 2026-06-10 04:00) was blocked by SafetyGates Gate A — the locked notional-basis cap (1× stake) rejects ALL risk-sized orders (notional ≈ 20-50× stake) — and the gate-block Telegram alert was itself lost to a parse_mode=Markdown 400. Fixed 2026-06-11: Gate A → risk basis (qty × |entry−stop| ≤ N × stake), Gate B → 10× stake, Telegram → plain text; pre-reg amended (addendum in `real_money_executor_architecture_decision_rule_2026-05-08.md`, sanctioned by migration trigger #1). Parity data accrues from the NEXT KAVA/ENS signal (~0.2-0.3/d) → watch `/var/log/paper-live/journal/layer3/`; weekly `layer3_cron.sh` (Sun 10:00 UTC) flips from INPUT_ERROR to evaluable states once data lands. The standalone `testnet-engine@.service` path is RETIRED (Layer 3 wrap superseded it). See `docs/findings/2026-06-11.md`.

## Session logs & pre-registration catalog

Per-session narratives: `docs/findings/<date>.md`. Latest: `2026-07-28.md`.
Pre-registration catalog: `results/INDEX.md` (136 docs: 43 decision rules + 51 verdicts + 4 syntheses + operational/state docs). **Read INDEX's top banner and its `## Close-out 2026-08-04 → 08-05` section first — the project is closed and most of this file describes a run that no longer exists.**
Cumulative audit: 91 fail-opens closed across 8 sessions; pattern locks on silent-on-corrupt-input (5 impl) + ssh-failure-vs-data-failure (6 impl) + writer-equals-fixture (3 impl). See `docs/AUDIT_LENS.md`.

**2026-05-19 research arc:** 6 pre-registered walk-forward sweeps mapping the strategy parameter landscape (EMA periods × timeframe / entry filters / side filter / exit mechanisms). Cross-cutting findings: LIVE config survives every structural perturbation tested; W3 (2025-2026) is filter-hostile; trail beats MLTP at loose thresholds; 2026-05-07 SUPPORTIVE findings don't replicate at slip=5 (cost-model fragility). All sweeps research-only, non-actionable per locked decision rule. **Index: `results/research_synthesis_2026-05-19.md`.** For the "is there anything solid in backtest we never deployed?" question, the closed-backlog ledger is `docs/RESEARCH_BACKLOG.md` (answer: no — every positive finding is already a shadow; the strategy-class search is closed).

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

8 per-symbol YAMLs in `configs/` (BTC/ETH/BNB/SOL/XRP/LINK/LTC/DOGE) pin falsified Option C settings (`min_rr: 1.0, target_rr: 5.0, stake_usd: 1000`) — NOT migrated to P4-Combined. Candidate strategy runs via CLI overrides on `configs/default.yaml` (`--signal-tf 4H --target-rr 6.0 --side-filter short --max-hold-hours 504 --funding-csv-dir data/funding`). `run_backtest.sh` is legacy-wired; new P4-Combined sweeps use `scripts/run_p4_variant.sh` + `scripts/p4_oos_persistence.sh` (which build their own configs and skip the per-symbol YAMLs).

## Paper-Live (VPS)

The production run is on Hetzner CX23 at `178.105.24.230`. Use `deploy/` scripts to manage it.

```bash
# Sync code + rebuild + restart one symbol and tail log
./deploy/redeploy.sh             # btcusdt (default)
./deploy/redeploy.sh ethusdt     # specific symbol
./deploy/redeploy.sh all         # all 16, no tail

# Sync only (no restart)
./deploy/sync.sh

# Check all 16 engines
ssh root@178.105.24.230 'systemctl status "paper-live@*.service" --no-pager | grep -E "●|Active:"'

# Tail a log
ssh root@178.105.24.230 'tail -f /var/log/paper-live/btcusdt.log'

# View all trades (wins/losses/PnL summary) — passive monitoring
./scripts/paper_live_trades.sh root@178.105.24.230

# Run reconciliation (compares live journals vs backtest)
./scripts/paper_live_report.sh

# Post-deploy operational health audit — RUN AFTER EVERY redeploy.sh
./scripts/post_deploy_check.sh

# Pull VPS journals → local cache (idempotent rsync; remote READ-ONLY). Run before dashboard.
bash scripts/journal_fetch.sh                    # default VPS host
bash scripts/journal_fetch.sh root@178.105.24.230

# Local forward-paper monitoring UI (localhost:8080 only). Reads results/journal_cache/
# (populate via journal_fetch.sh first) + results/drift_check_history.jsonl.
# Routes: /status /cohorts /symbol/<SYM> /healthz
go run ./cmd/dashboard --journal-dir results/journal_cache --results-dir results --port 8080
```

### Journal cache layout + P&L recompute

`results/journal_cache/` (gitignored) after `journal_fetch.sh` holds THREE kinds of journal, only one of which is the live book:

| Path | What | In live book? |
|---|---|---|
| `results/journal_cache/*.jsonl` | live engines, one file per `SYMBOL-YYYY-MM` | **YES** |
| `results/journal_cache/shadow/<algo>/*.jsonl` | 8 research shadows | NO |
| `results/journal_cache/archive/option_c_*/` | **falsified** Option C (superseded 2026-05-07) | NO |

**P&L recompute recipe** (no committed script for the plain book summary): python3 over `results/journal_cache/*.jsonl` — **FLAT glob, non-recursive** — skip any path containing `shadow` / `layer3` / **`archive`**; sum `pnl_usd` where `event=="close"`; symbol = `basename.split('-')[0]`; fees/slip via `fee_usd` / `slip_usd`.

> **Trap (hit 2026-07-29):** a recursive `**/*.jsonl` glob filtered only on `shadow`/`layer3` silently readmits `archive/option_c_*` and reports **339 trades / +$86,376** instead of **126 / +$3,410** — a 25× overstatement that would corrupt any checkpoint fed by it. The flat glob is load-bearing. If you make it recursive, `archive` MUST be excluded. Sanity check: flat and recursive globs must agree; live trade count is ~126 (2026-07-29), not hundreds.

Other journal gotchas:
- **`outcome` labels LIE on max-hold force-closes.** A 504h force-close is written as `TARGET` even when the target was never touched (XLM 2026-07-28T04:00:00Z, +4.95R). Any close at exactly `HH:00:00Z` on a 504h boundary is suspect — classify by realized R, not the label. SHORT: `R = (entry − exit) / |entry − stop|`.
- **`scripts/maxhold_wave_classifier.py` shows only UPCOMING boundaries** — for already-closed trades read the journal close events directly.
- **`forward_paper_status.sh` reads `JOURNAL_DIR` from env** — always invoke as `env -u JOURNAL_DIR bash scripts/forward_paper_status.sh` or it silently reads the wrong directory.
- **Heredoc CWD trap:** shell state does not persist between Bash calls. Use absolute paths inside `python3 - <<'EOF'` heredocs; relative paths silently match zero files (symptom: `ZeroDivisionError` on a `100*w/n`).

**Post-deploy validation is mandatory.** After ANY `deploy/redeploy.sh`, run `scripts/post_deploy_check.sh`. 13 audit sections: engines active, watchdog timers, binary md5, tick freshness, ERROR logs, rate-limit, position recoveries, executor mode, funding-CSV staleness, disk/log size, drift cron freshness, restart-loop, live-config compliance. Sections 4/5/6 apply 5-min uptime gate. `STRICT=1` exits non-zero + fires Telegram WARN (CI-friendly).

**Daily digest** (Telegram, VPS cron 09:00 UTC). One-screen summary: trades/WR/PnL per cohort + open positions + verdict. `DRY_RUN=1` prints instead of sending. Log: `/var/log/paper-live/daily_digest.log`.
```bash
# View what would be sent (from local machine)
ssh root@178.105.24.230 '. /etc/paper-live/env && DRY_RUN=1 /opt/trading-engine/scripts/daily_digest.sh'
```

**Funding-CSV auto-refresh** (VPS cron, Sunday 03:00 UTC). Incrementally downloads Binance funding rates for deployed-16 symbols, validates no CSV is stale (>24h), sends Telegram on success/failure. Engines pick up new data on next restart. Log: `/var/log/paper-live/funding_refresh.log`.
```bash
# Install on VPS (one-time)
ssh root@178.105.24.230 'bash /opt/trading-engine/deploy/install_funding_cron.sh'
# Manual run (from local machine)
bash scripts/funding_refresh_cron.sh
```

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

**Candidate strategy (P4-Combined, post-fees backtest leader):** EMA9×EMA21 crossover on the **4H** signal timeframe, with a **wick-based stop** and **fixed 6:1 R:R** take-profit. A bearish 4H EMA cross opens a SHORT (longs are filtered out via `--side-filter short`). Any open short older than **504 hours (21 days)** is force-closed at the current tick price (the live `--max-hold-hours`; the original P4-Combined candidate used 336h). Funding cost is accrued from per-symbol historical Binance funding-rate CSVs (`data/funding/{SYMBOL}.csv`) — longs pay positive funding, shorts receive it; net aggregate over the 5y sample is roughly zero, not a benefit.

Cost-geometry rationale: on the 5m timeframe, wick stops are tight (~0.18% on BTC at p50) which forces ~500× implicit leverage to size each trade to a $1k stake, which makes round-trip taker fees eat the entire edge (Option C falsification). The 4H wick is wider, implicit leverage drops, fee per trade as a fraction of risked $ falls below the gross edge.

Two legacy entry types remain in code and are toggleable via `ema_mode: false` (neither is in the candidate or live path):

- **Absorption (reversal):** N consecutive 5m candles near a key level with wick/body ≥ `wick_ratio`, body closing away from the level. Allowed in any 4H bias.
- **Breakout (continuation):** 5m candle closes through a level with body/range ≥ `breakout_body_ratio`. Only taken aligned with 4H bias; blocked when bias is Neutral.

Key levels (used by absorption/breakout only): PDH, PDL, and optional manual `zones` from config.

## Telegram Notifier

`pkg/notify/telegram.go` — opt-in startup/shutdown alerts. Reads `TELEGRAM_BOT_TOKEN` and `TELEGRAM_CHAT_ID` from env (set in `/etc/paper-live/env` on VPS). When either is unset, `Send` is a no-op — same pattern as `JournalPath`. Test: `pkg/notify/telegram_test.go`.

`SendStructured` retries per tier: CRITICAL 3, WARN 2, INFO 1 (jittered exp backoff 1s→2s; 4xx skips retry). Final CRITICAL failure → `slog.Error` so post_deploy_check section 5 surfaces it. HTTP timeout 10s.

## Key Invariants

- **Exchange timestamps only.** `Candle` and `Tick` timestamps come from the exchange. `time.Now()` is never used for price data. Journal events follow this too: the `open` event's `ts` is `sig.Timestamp`, the `close` event's `ts` is the exit-tick time (via `journalTS(exitTime)` in `pkg/execution/stub.go` — falls back to `time.Now()` only for a zero exitTime, which the live journal-writing path never produces). `TestJournalEventTS_UsesExchangeTime` enforces this. Note: the journal *filename* month is still derived from `time.Now()` in `appendJournal` — fine for the real-time live writer, but a backdated close lands in the current-month file with a backdated event `ts`.
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
| `cmd/journal_report` | Per-symbol journal summary + reruns backtest over the same range to surface a live-vs-backtest drift column (`cli_test.go`) |

## Known Bugs

### Open

- `CSVReplay` double-close: stream goroutine defers `f.Close()` and `main.go` also calls `defer src.Close()`. Idempotent (second close returns harmless error). Skip.
- `run_backtest.sh` runs the binary twice per month (display + accumulate). Use `full_analysis.sh` for multi-month runs. Performance only.

### Fixed

Bugs 1–6 + pre-milestone fixes resolved. See commit history + `docs/findings/` for narratives.


## Forward-paper go/no-go criteria

Forward-paper started 2026-05-05 20:06 UTC, deployed-16 symbols. **Honest annual: ~$69k/yr at slip=25bp** (train-only-shortlist shows +70% look-ahead inflation on the $129k deployed-claim).

### Kill mechanism (decision-grade vs advisory)

Threshold-based criteria below are **advisory only** (30-40% FP under null). See `results/kill_bar_recal_verdict_2026-05-07.md`.

**Decision-grade kill: `scripts/live_vs_backtest_drift.py`** — Welch t + WR z-test, Bonferroni-corrected, α_family=0.001 (FP=12.1%, TP=100%). **Weekly cadence only** (daily → 28%/yr FP). Two firings 7+ days apart OR drift+threshold match = auto-kill.

**Canonical invocation: `scripts/run_drift_check.sh`** — wraps detector, persists history, evaluates two-firings rule. Exit codes: 0 CLEAN / 1 INVESTIGATION / 2 INSUFFICIENT / 3 ERROR / 4 AUTO-KILL / **5 HISTORY_CORRUPT** (malformed line in `drift_check_history.jsonl`; two-firings rule unevaluable until repaired). Use `--quiet` for cron.

> **DO NOT run `run_drift_check.sh` ad-hoc.** It **appends to `results/drift_check_history.jsonl`**. An off-cadence run manufactures a false 7-days-apart pair and can fabricate an exit-4 auto-kill. Weekly cron/launchd only. Read-only alternatives for manual inspection: `scripts/crosscheck9_losers_mae.py`, `scripts/drift_decompose.py` (both take `--live-dir results/journal_cache`).

**Scheduled: `scripts/weekly_audit.sh`** via launchd (Sunday 09:00). Stages: (1) drift, (2) `forward_paper_status.sh` snapshot, (3) `cmd/journal_validate` (Telegram CRITICAL on errors). Plist: `deploy/drift-check.launchd.plist`. Telegram alerts on codes 1/3 (WARN) + 4 (CRITICAL); 0/2 silent.

> **DISARMED 2026-08-06.** `com.tradingengine.drift-check` was still loaded in
> launchd and still firing weekly *after* the VPS was destroyed — its 2026-08-02
> run logged `drift_check: AUTO-KILL CANDIDATE` and `resolution: exit=4
> class=KILL` against a book that had already closed, and would have fired again
> 08-09. Unloaded (`launchctl bootout`) and the installed plist removed from
> `~/Library/LaunchAgents/`. The template stays in `deploy/` and is byte-identical
> to what was installed, so it is reinstallable if ever needed. **Do not reload it.**

**`cmd/journal_validate`** — cross-month consistency checker. Exit codes: 0 CLEAN / 1 WARN / 2 ERROR / 3 USAGE. `--exclude archive` skips pre-Bug-6.

### Statistical-power floor (before reading any signal)

P4-Combined backtests at WR 20.64% with 6:1 R:R. Breakeven WR ≈ 14.3% — only ~6.3pp cushion. To bound observed WR within ±10pp at 95% CI requires ~63 trades; ±5pp requires ~250 trades. Strategy B at 5,809/5y × 32/57 sym ≈ 1.8 trades/day → 60 days ≈ 108 trades = ~±7pp resolution. **Do not draw conclusions from < 60 calendar days of forward data.**

### Deploy real money (1/10th notional) — ALL must be true

At ~1.18 trades/day, 150 trades requires ~127 days; apply both thresholds literally (whichever comes second).

- ≥150 live trades AND ≥60 calendar days net-positive
- Realized fees ≤ 12 bp (journal writes `fee_usd`/`slip_usd`/`notional_usd` per close). **Paper-mode: PASS by construction** (Stub flat-rate); informational only at Layer 2+
- Realized stop-side slippage ≤ 20 bp on losers. Same paper-mode caveat
- Live PnL ≥ 60% of pro-rated honest-annual ($69k/yr × elapsed × 0.60)
- Live PnL beats BTC HODL at $16k notional (amended 2026-05-12; see `results/btc_hodl_notional_amendment_2026-05-12.md`)
- No single symbol >40% of cumulative PnL
- **`scripts/live_vs_backtest_drift.py` exit 0** — decision-grade gate; threshold criteria above are advisory only

### Kill triggers (advisory — confirm via drift detector before acting)

NOT auto-kills (30-40% FP). Each is an investigation trigger → run drift detector; if exit 1, kill.

- First 60 days net-negative
- Stop-side slippage > 25 bp (actual breakeven ~81bp; linear, no cliff — `results/slip_cliff_verdict_2026-05-07.md`)
- WR < 14% over ≥150 trades (below breakeven; low-power)
- Single symbol >40% of PnL (fires 41% in healthy windows; skepticism warranted)
- Train-only-shortlist re-run shows non-positive honest test
- Two consecutive 30d windows underperform BTC-HODL by >$5k each
- **Drift detector exit 1** at α_family=0.001 — decision-grade kill, no further confirmation needed

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

3 still-open (original 13 + 1 added 2026-06-10):

- **REST polling lag (10s)** — adverse on entry + stop. Instrumented per-tick (`Tick.LocalReceiptTS` → `Heartbeat` rolling ring → `lag_summary.sh` / `post_deploy_check.sh §4`). Tiers: ≤1s OK / ≤15s TYPICAL / ≤30s DEGRADED / >30s HIGH. Real fill-vs-model still requires Layer 2.
- **No real-money execution test** — sizing, limits, margin reuse, concurrent trades unmodeled. Projection $69–184k/yr depending on slip.
- **Real-money venue access BLOCKED — CONFIRMED PERMANENT-FOR-NOW (2026-06-10)** — operator's Binance account (ADGM/EEA entity, Romania) region-blocks futures. Binance support confirmed same day: **no planned EEA futures activation, no bypass, no timeline** ("urmăriți sursele oficiale"). The Binance real-money path (BinanceLive executor, kill_switch, STAGE_1) is therefore NOT executable for this operator. Paper run + Layer 2/3 testnet UNAFFECTED (testnet auth smoke passed 2026-06-10 with operator's demo.binance.com credentials) — execution-layer validation still completes on testnet. **Consequence: if forward-paper emits PROMOTE, the next milestone is an executor port to an EU-accessible venue (candidates: Kraken perps / OKX-EU / Hyperliquid) — this is now the DEFAULT real-money path, not a contingency.** Port = new executor + fresh Layer 2/3-equivalent validation + fee/funding model re-check, pre-registered. Do NOT start the port before the promote/kill verdict; a KILL verdict makes it moot.

Closed: **Funding-CSV staleness** — mitigated by `scripts/funding_refresh_cron.sh` (VPS cron, Sunday 03:00 UTC). `post_deploy_check.sh §9` still surfaces staleness if cron fails.

