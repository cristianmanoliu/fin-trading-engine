# trading-engine

> **⚠️ This README describes the LEGACY absorption/breakout strategy.** Current live strategy is **Strategy B (P4-Shorts-Only)** running on 32 symbols since 2026-05-05. See `CLAUDE.md` for the actual current state, deployment status, and operational commands. README rewrite is deferred until forward-paper validation completes (~2026-05-27).

A Go-based algorithmic trading engine implementing a multi-timeframe breakout/reversal strategy on Binance Futures. Tested on 8 instruments (BTC, ETH, SOL, BNB, XRP, LINK, LTC, DOGE) over 6 years.

## Strategy

Watches for **absorption** (rejection) and **breakout** patterns at key price levels:

- **Levels**: Previous Day High (PDH), Previous Day Low (PDL), and optional manual supply/demand zones
- **Timeframes**: 4H sets macro bias → 30m structural context → 5m entry trigger
- **Entry**: 2+ consecutive 5m candles with long wicks rejecting a level (absorption), or a momentum candle breaking through with >60% body/range ratio (breakout)
- **Target**: Session VWAP
- **Stop**: Beyond the wick extreme + buffer

Breakout entries are only taken aligned with 4H bias. Absorption (reversal) entries are taken in any bias.

## Backtest Results

All multi-instrument runs: `./scripts/full_analysis.sh SYMBOL 2020 2025 04`, $1,000 fixed stake/trade.
`yrs+` = number of calendar years the strategy was net profitable at that setting.

### BTCUSDT — 2024 only

| min_rr | Trades | Win% | Avg Win | Avg Loss | W/L  | Expectancy | Total PnL |
|--------|--------|------|---------|----------|------|------------|-----------|
| 0      | 207    | 45%  | 348 pts | 250 pts  | 1.39 | +21 pts    | +4,438 pts |
| 1.0    | 148    | 39%  | 538 pts | 237 pts  | 2.27 | +62 pts    | +9,121 pts |
| 2.0    | 103    | 38%  | 732 pts | 259 pts  | 2.82 | +116 pts   | +11,951 pts |
| **2.5**| **87** | **38%** | **818 pts** | **274 pts** | **2.98** | **+140 pts** | **+12,196 pts** |
| 3.0    | 75     | 37%  | 954 pts | 285 pts  | 3.34 | +177 pts   | +13,293 pts |

> Single-year figures only. Full multi-year BTC not run; global `min_rr: 1.0` setting applies.

### ETHUSDT — 2020–2025 (6 years)

| min_rr | Trades | Win%  | Exp/trade | Total pts  | Total $1k   | W/L  | yrs+ |
|--------|--------|-------|-----------|------------|-------------|------|------|
| 0      | 873    | 38.3% | +1.0 pts  | +884.9     | +$120,455   | 1.88 | 4/6  |
| 0.5    | 750    | 38.1% | +2.1 pts  | +1,539.3   | +$289,959   | 2.17 | 6/6  |
| 1.0    | 623    | 33.4% | +3.7 pts  | +2,293.7   | +$310,426   | 3.30 | 5/6  |
| **1.5**| **575**| **35.3%** | **+3.6 pts** | **+2,095.5** | **+$410,496** | **2.95** | **6/6** |
| 2.0    | 476    | 33.0% | +2.9 pts  | +1,402.3   | +$320,381   | 3.06 | 4/6  |
| 2.5    | 384    | 34.6% | +2.9 pts  | +1,106.3   | +$357,261   | 2.66 | 4/6  |
| 3.0    | 311    | 26.0% | +2.9 pts  | +902.7     | +$204,173   | 3.97 | 4/6  |

Per-instrument optimal: `min_rr: 1.5`. **Global setting: `min_rr: 1.0`** (see Cross-Instrument Summary).

### SOLUSDT — 2020–2025 (6 years, data starts Sep 2020)

| min_rr | Trades | Win%  | Exp/trade | Total pts | Total $1k   | W/L  | yrs+ |
|--------|--------|-------|-----------|-----------|-------------|------|------|
| 0      | 1,005  | 44.9% | +0.5 pts  | +453.2    | +$473,735   | 1.90 | 4/6  |
| 0.5    | 870    | 40.5% | +0.5 pts  | +403.8    | +$534,912   | 2.27 | 6/6  |
| 1.0    | 749    | 40.7% | +0.6 pts  | +470.3    | +$664,730   | 2.47 | 5/6  |
| **1.5**| **622**| **39.5%** | **+0.6 pts** | **+378.1** | **+$778,554** | **2.43** | **6/6** |
| 2.0    | 546    | 37.4% | +0.6 pts  | +316.9    | +$643,747   | 2.56 | 6/6  |
| 2.5    | 455    | 36.7% | +0.9 pts  | +428.5    | +$709,565   | 3.30 | 5/6  |
| 3.0    | 376    | 34.0% | +1.4 pts  | +522.1    | +$554,267   | 4.89 | 6/6  |

Per-instrument optimal: `min_rr: 1.5`. **Global setting: `min_rr: 1.0`** (see Cross-Instrument Summary).

### Cross-Instrument Summary

`./scripts/cross_analysis.sh` was run across all 8 instruments (2020–2025, $1k fixed stake) to find the single `min_rr` that maximises combined total_$1k while being profitable every year on every symbol.

**Key finding: no single `min_rr` achieves 8/8 symbols profitable in every year.**

| min_rr | syms+ (all-years profitable) | Combined $1k (8 symbols) |
|--------|------------------------------|--------------------------|
| 0      | 2/8                          | —                        |
| 0.5    | 4/8                          | —                        |
| 1.0    | 5/8                          | ~+$3.8M                  |
| **1.5**| **6/8**                      | **~+$5.2M** ← highest    |
| 2.0    | 5/8                          | —                        |
| 2.5    | 4/8                          | —                        |
| 3.0    | 4/8                          | —                        |

**Decision: `min_rr: 1.0` globally** — second-best combined PnL, one more symbol profitable than 1.5, simpler to reason about, and avoids curve-fitting to the 6/8 optimum. All 8 per-symbol configs use this setting.

Use `./scripts/validate_all.sh` to confirm current settings across all instruments.

> PnL in price points per unit (pts) or dollars ($1k = $1,000 fixed risk per trade). Dollar values require `stake_usd: 1000` in the config.

## Architecture

```
Market Data (WebSocket / CSV)
        │
        ▼
  Candle Aggregator          ← builds 5m / 30m / 4H candles from ticks
        │
   ┌────┴────┐
   │         │
  5m        4H              ← 4H updates macro bias
  30m
   │
   ▼
Strategy Runner              ← single goroutine, owns all state, no mutexes
  ├── BiasTracker            ← Long / Short / Neutral from 4H
  ├── VWAP                  ← session VWAP, resets 00:00 UTC
  ├── DailyLevels           ← PDH / PDL, rolls at midnight
  └── EntryDetector         ← absorption + breakout logic
        │
        ▼
  Execution Stub            ← paper trades, tracks PnL
```

Event-driven channel pipeline. The strategy runner uses a `select` loop with single ownership of all mutable state — no mutex locking required.

## Project Layout

```
cmd/
  backtest/        CSV replay entry point
  engine/          Live Binance Futures WebSocket entry point
  journal_report/  Paper-live reconciliation tool (journal vs backtest)
pkg/
  models/          Tick, Candle, Signal, Zone, Direction
  marketdata/      DataSource interface, Binance WS + REST backfill, CSV replay, heartbeat
  aggregator/      Multi-timeframe candle builder
  indicators/      VWAP, PDH/PDL
  strategy/        Bias, entry detection, engine runner
  execution/       Paper trading stub with PnL summary and JSONL journal sink
config/            YAML config loader
configs/           Per-symbol configs (btcusdt.yaml … dogeusdt.yaml)
scripts/           Backtest, analysis, and paper-live orchestration
logs/              Paper-live log files and trade journals (git-ignored)
```

## Quickstart

**Prerequisites**: Go 1.22+, `jq`, `curl`, `unzip`

```bash
# Run a single-month backtest (auto-detects configs/btcusdt.yaml)
./scripts/run_backtest.sh BTCUSDT 2024 01

# Run a full year
./scripts/run_backtest.sh BTCUSDT 2024 01 12

# Multi-year heatmap with min_rr sweep for one instrument
./scripts/full_analysis.sh BTCUSDT 2020 2025 04

# Find optimal min_rr across all 8 instruments simultaneously
./scripts/cross_analysis.sh

# Validate all 8 instruments at their current settings
./scripts/validate_all.sh

# Connect to live Binance Futures feed (paper trading)
go run ./cmd/engine --config configs/btcusdt.yaml
```

## Configuration

`configs/default.yaml`:

```yaml
symbol: BTCUSDT

exchange:
  ws_url: wss://fstream.binance.com
  rest_url: https://fapi.binance.com  # optional; this is the default

strategy:
  proximity_pct: 0.001       # how close to a level counts as "near" (0.1%)
  wick_ratio: 2.0            # minimum wick/body ratio for absorption candles
  breakout_body_ratio: 0.6   # minimum body/range for a breakout candle
  absorption_candles: 2      # consecutive candles required to confirm
  stop_buffer_pct: 0.001     # buffer beyond wick extreme for stop placement
  min_rr: 1.0                # minimum reward/risk ratio; 0 = disabled
  stake_usd: 1000            # fixed USD risked per trade (required for dollar PnL output)
  backfill_hours: 48         # optional; hours of 1m klines to fetch on live engine startup

backtest:
  csv_path: ./data/BTCUSDT-1m-2024-01.csv

# Optional manual supply/demand zones
zones: []
# zones:
#   - low: 42000
#     high: 42500
#     type: supply
```

## Scripts

Data is sourced from [data.binance.vision](https://data.binance.vision/data/futures/um/monthly/klines/BTCUSDT/1m/). All scripts skip months whose CSV already exists in `data/`.

---

### `download_data.sh` — fetch one month

Downloads and unzips a single month of 1-minute Binance klines.

```bash
./scripts/download_data.sh [SYMBOL] [YEAR] [MONTH]
# defaults: BTCUSDT 2024 01

./scripts/download_data.sh BTCUSDT 2023 06
# → data/BTCUSDT-1m-2023-06.csv
```

---

### `run_backtest.sh` — backtest one year (or part of it)

Downloads any missing months, runs the backtest for each month in sequence, then prints a combined summary. Uses `go run` (recompiles each invocation).

```bash
./scripts/run_backtest.sh [SYMBOL] [YEAR] [START_M] [END_M]
# defaults: BTCUSDT 2024 01 01  (single month)

./scripts/run_backtest.sh                        # Jan 2024
./scripts/run_backtest.sh BTCUSDT 2024 01 12     # full year 2024
./scripts/run_backtest.sh BTCUSDT 2023 06 12     # Jun–Dec 2023
```

Output: one block per month showing every signal, entry, exit, and PnL. A combined summary is printed at the end when more than one month is requested.

**Note:** runs the backtest binary **twice per month** — once for pretty-printing, once to capture summary stats for the combined total. Use `full_analysis.sh` for multi-year runs to avoid this overhead.

For 5 years, loop over years manually:

```bash
for year in 2020 2021 2022 2023 2024; do
  ./scripts/run_backtest.sh BTCUSDT $year 01 12
done
```

---

### `sweep.sh` — compare min_rr values across a date range

Runs the backtest for every combination of month × `min_rr` value in a single year and prints a comparison table. Useful for tuning the `min_rr` filter.

```bash
./scripts/sweep.sh [SYMBOL] [YEAR] [START_M] [END_M] ["rr values"]
# defaults: BTCUSDT 2024 01 01 "0 0.5 1.0 1.5 2.0 2.5 3.0"

./scripts/sweep.sh                               # Jan 2024, default rr range
./scripts/sweep.sh BTCUSDT 2024 01 12            # full year 2024
./scripts/sweep.sh BTCUSDT 2024 01 12 "1 2 3"   # custom rr values
```

Output columns: `min_rr | trades | win% | exp/trade | total_pnl | avg_win | avg_loss | W/L | filtered_out`

`filtered_out` shows how many trades were removed relative to `min_rr=0`, giving a sense of setup quality at each threshold.

---

### `cross_analysis.sh` — find the optimal min_rr across multiple instruments

Runs all `(symbol × month × min_rr)` combinations, compiles once, and finds the single `min_rr` that maximises combined total_$1k while being profitable every year on every symbol. This is the right tool for finding a setting that generalises across instruments rather than overfitting to one.

```bash
./scripts/cross_analysis.sh ["SYMBOLS"] [START_YEAR] [END_YEAR] [END_YEAR_MONTH] ["rr values"]
# defaults: "BTCUSDT ETHUSDT SOLUSDT BNBUSDT" 2020 2025 04 "0 0.5 1.0 1.5 2.0 2.5 3.0"

./scripts/cross_analysis.sh                                        # all 4 symbols, 2020–2025
./scripts/cross_analysis.sh "ETHUSDT SOLUSDT" 2021 2024 12        # two symbols, custom range
```

Output:
1. **Per-symbol breakdown** — trades, win%, total_$1k, yrs+ at each min_rr for each symbol
2. **Cross-instrument aggregate** — combined totals with a `syms+` column (how many symbols are profitable in all years)
3. **Recommendation** — the min_rr with the highest combined total_$1k where `syms+` equals the total symbol count

---

### `validate_all.sh` — confirm all 8 instruments at their fixed settings

Runs every instrument using its per-symbol config (no min_rr sweep). Reads min_rr from each config, downloads any missing data, and prints a year-by-year PnL heatmap with symbol rows and year columns.

```bash
./scripts/validate_all.sh
```

Use this after changing any per-symbol config to verify nothing regressed across the full instrument set.

---

### `full_analysis.sh` — multi-year heatmap (use this for 5-year runs)

The most powerful script. Compiles the binary **once**, downloads all missing data, then runs every `(year × month × min_rr)` combination. Outputs an expectancy heatmap per year and an overall summary table with an automatic `min_rr` recommendation.

```bash
./scripts/full_analysis.sh [SYMBOL] [START_YEAR] [END_YEAR] [END_YEAR_MONTH] ["rr values"]
# defaults: BTCUSDT 2020 2025 04 "0 0.5 1.0 1.5 2.0 2.5 3.0"

./scripts/full_analysis.sh                              # BTCUSDT 2020–2025
./scripts/full_analysis.sh BTCUSDT 2020 2024 12         # full 2020–2024
./scripts/full_analysis.sh BTCUSDT 2022 2024 12 "0 1 2 3"  # custom rr values
```

Output:
1. **Progress bar** — `[####...] 42% (59/140) rr=2.5 2023-07`
2. **Expectancy heatmap** — expectancy/trade in points, one column per year, one row per `min_rr`
3. **Overall summary table** — trades, win%, expectancy, total pts, total $ (if `stake_usd` set), avg win/loss, W/L ratio, and how many years were profitable (`yrs+`)
4. **Recommendation** — the `min_rr` with the highest expectancy that was profitable in **every** year tested

The `yrs+` column is the key signal: a setting that looks great overall but is negative in some years is curve-fitted to the good years. Prefer the row where `yrs+` equals the total year count.

## Output

Backtest output is structured JSON via `slog`, pretty-printed by `jq`. Key events:

```
SIGNAL  SHORT  entry=96400  stop=96650  target=95200  rr=4.80
TARGET  SHORT  entry=96400  exit=95180  pnl=1220pts
STOP    LONG   entry=43380  exit=43278  pnl=-102pts
SUMMARY  trades=87  wins=33  losses=54  win_rate=37.9%  total_pnl=12195pts  expectancy=140pts
```

## VPS Deployment

The engine runs on a Hetzner CX23 VPS (178.105.24.230, Falkenstein, Ubuntu 24.04). All deploy scripts live in `deploy/`.

```bash
# First-time server setup (run once as root on fresh VPS)
ssh root@<IP> 'bash -s' < ./deploy/setup.sh

# Sync code + rebuild binaries on VPS
./deploy/sync.sh

# Install systemd units, logrotate, Telegram credentials
ssh root@<IP> 'bash /opt/trading-engine/deploy/install.sh'

# Sync + rebuild + restart one symbol (default: btcusdt) and tail its log
./deploy/redeploy.sh
./deploy/redeploy.sh ethusdt

# Sync + rebuild + restart all 8 engines
./deploy/redeploy.sh all
```

**VPS paths:**
- Binaries: `/opt/trading-engine/bin/`
- Engine logs: `/var/log/paper-live/{symbol}.log`
- Trade journals: `/var/log/paper-live/journal/{SYMBOL}-YYYY-MM.jsonl`
- Credentials: `/etc/paper-live/env` (chmod 600)
- Watchdog state: `/var/lib/paper-live/alerted.state`

**Monitoring:**
```bash
# Status of all 8 engines
ssh root@178.105.24.230 'systemctl status "paper-live@*.service" --no-pager | grep -E "●|Active:"'

# Tail one symbol's log
ssh root@178.105.24.230 'tail -f /var/log/paper-live/btcusdt.log'

# Timer schedule
ssh root@178.105.24.230 'systemctl list-timers | grep paper-live'
```

---

## Paper-Live Validation

The 21-day validation run started **2026-05-03** on the VPS. All 8 engines are live on Binance Futures WebSocket in paper-trading mode. No real money, no API keys.

**Status:** 🟢 Running — Day 1 of 21

After backtesting, the next milestone is confirming real-time behaviour matches backtest expectations within tolerance.

**Prerequisites:** Go 1.22+, `jq`, `curl`, `unzip`, internet access to `fstream.binance.com` and `fapi.binance.com`.

```bash
# Start all 8 symbol engines (builds binary once, launches in background)
./scripts/paper_live_start.sh

# Check liveness, heartbeats, last trade time
./scripts/paper_live_status.sh

# Stop all engines gracefully
./scripts/paper_live_stop.sh

# Compare paper-live journals against backtest for the same period
./scripts/paper_live_report.sh
```

**What runs on startup:** Each engine fetches the last 48h of 1m klines from the Binance Futures REST API and replays them through the aggregator before opening the WebSocket. This primes `DailyLevels` (PDH/PDL) and the VWAP state so the engine can trade from the first live tick rather than waiting 24h for a UTC day boundary.

**Trade journals:** Every `position opened` / `position closed` event is appended as a JSON line to `logs/journal/{SYMBOL}-YYYY-MM.jsonl`. Journals survive engine restarts.

**Heartbeats:** Each engine logs a `"heartbeat"` JSON line every 60 seconds (escalated to `Warn` if the feed is silent for > 90s). WebSocket reconnect gaps are logged as `"ws gap closed"` with the gap duration.

**Reconciliation:** `paper_live_report.sh` reads the journals, runs the backtest binary over the same date range, and prints a side-by-side table. A drift of > 10% on trade count vs backtest signals a data or strategy bug — abort the observation window and investigate before continuing.

**Acceptable drift:** Small drift is expected due to (a) the backtest uses OHLC klines while the live feed uses aggTrade ticks, and (b) WebSocket gaps may cause missed signals. A < 5% trade-count delta and < 10% PnL delta are healthy. Larger gaps need investigation.

## Next Steps

- [x] Paper-live validation — 21-day run started 2026-05-03 (Day 1 of 21)
- [ ] Day 10: first reconciliation (`./scripts/paper_live_report.sh`) — target < 10% trade-count drift
- [ ] Day 21: final reconciliation and go/no-go decision for real-execution workstream
- [ ] Wire real order execution (Binance REST signed API) in `pkg/execution/` — deferred until paper-live passes
- [ ] Add trailing stop logic (trail to breakeven after price moves 1R in favour)
- [ ] Add tests: `EntryDetector` absorption/breakout sequences, `DailyLevels` day-roll, `VWAP` session reset

## Known Issues & Technical Debt

Findings from a full codebase review (2026-05-03).

### Bugs

| Severity | Location | Issue |
|----------|----------|-------|
| High | `go.mod:3` | `go 1.26.2` is an invalid Go version (does not exist). Likely `1.22.2`. |
| Medium | `marketdata/replay.go:129`, `cmd/*/main.go` | `CSVReplay` double-close: the `stream` goroutine defers `f.Close()` and `main.go` calls `defer src.Close()` — both close the same `*os.File`. |
| Low | `scripts/run_backtest.sh:54,67` | Backtest is compiled and run twice per month (once for display, once for summary accumulation). Doubles run time for long date ranges. |
| Low | `go.mod:5-9` | All dependencies marked `// indirect`; they are direct imports. Fix with `go mod tidy`. |

### Dead Code

- `DailyLevels.LevelDirection` (`pkg/indicators/levels.go:70`) — defined but never called. Direction is determined independently in `checkAbsorption`.
- `bias.Allows(side, false)` in `checkAbsorption` (`pkg/strategy/entry.go:125`) — always returns `true` because absorption trades are unconditionally allowed. The check is a no-op.

### Design Notes

- **Duplicate `main.go`**: `cmd/backtest/main.go` and `cmd/engine/main.go` share ~90% identical fan-out + wiring code. Extracting a `run(cfg, src)` function into `cmd/internal` would prevent drift.
- **Sequential fan-out**: Ticks are sent to `aggTicks` then `stratTicks` in sequence. If the aggregator stalls, the strategy runner's tick channel also starves. Buffers of 1000 make this unlikely in practice.
- **No signal deduplication**: Two consecutive 5m candles satisfying absorption at the same level emit two signals; the second is rejected by the executor (position already open) but adds log noise.
- **`hasAbsorptionWick` doji floor** (`pkg/strategy/entry.go:235`): Uses absolute `0.0001` body floor. Fine for BTC; would break for sub-cent instruments.
- **Proximity is close-relative, not level-relative** (`pkg/strategy/entry.go:83`): `prox = last.Close * pct` rather than `level * pct`. Negligible difference for BTC but semantically imprecise.

### Missing Tests

Zero test files exist. High-value targets:

| Package | What to test |
|---------|-------------|
| `pkg/strategy` | `EntryDetector` absorption + breakout with crafted candle sequences |
| `pkg/indicators` | `DailyLevels` day-roll at midnight; `VWAP` session reset |
| `pkg/aggregator` | Candle boundary alignment across all three timeframes |
| `pkg/execution` | `Stub.Summary` PnL math (expectancy, win-rate, W/L ratio) |

### Dependency Note

`gorilla/websocket` has been effectively unmaintained since 2023. For production use, consider migrating to [`coder/websocket`](https://pkg.go.dev/github.com/coder/websocket) or [`nhooyr.io/websocket`](https://pkg.go.dev/nhooyr.io/websocket).
