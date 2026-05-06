# Market Data Recovery

## Why the data isn't in git

The `data/` directory contains ~12 GB of 1-minute kline CSVs (2,816 files: 57 symbols × ~64 months from 2020-01 through 2025-04). It is **deliberately excluded** from version control via `.gitignore`:

```
data/*.csv
```

Reasons:
1. **It's public.** Sourced from Binance's official archive at [data.binance.vision](https://data.binance.vision/?prefix=data/futures/um/monthly/klines/). Anyone can download it.
2. **It's large.** 12 GB raw, ~4.4 GB compressed (2.7× ratio for kline CSVs). Past GitHub's 100 MB per-file limit; would require Git LFS ($5/mo + bandwidth caps) or chunked GitHub Releases (3-4 manual 2 GB blobs, awkward to refresh as data grows monthly).
3. **It's reproducible.** The same data anyone downloads is byte-identical. Storing it adds nothing.

What **is** kept in git:
- `data/funding/` (10 MB) — per-symbol historical Binance funding-rate CSVs. Small, deploy-relevant (live engines need them), and refreshed via `scripts/refresh_funding.sh`.

## Recovery — one command

```bash
./scripts/download_data_all.sh
```

This batch-downloads all 2,816 CSVs from `data.binance.vision` to `./data/`. **Idempotent** — re-running skips files already on disk, so you can resume after a partial download.

**Expected:**
- Runtime: 5-15 min on a typical 50+ Mbps connection (8 concurrent workers by default)
- Disk: ~12 GB final
- Tools: `curl`, `unzip`, `bash` (all standard)

**Tuning:**
```bash
# Faster (more concurrency — Binance starts rate-limiting around 20)
PARALLEL=15 ./scripts/download_data_all.sh

# Subset (single symbol, partial date range)
./scripts/download_data_all.sh "BTCUSDT" 2024 01 2024 12

# Subset (multiple symbols, full range)
./scripts/download_data_all.sh "BTCUSDT ETHUSDT SOLUSDT"
```

## Recovery — single file (legacy script, kept for backwards compat)

```bash
./scripts/download_data.sh BTCUSDT 2024 01
```

Downloads exactly one symbol/month. Equivalent to a single iteration of the batch script. Use for ad-hoc replays or when you only need one file.

## What's actually downloaded — file format

Each CSV is the standard Binance Futures monthly kline export:

```csv
1704067200000,42100.5,42150.0,42050.0,42120.5,123.456,1704067259999,5202345.67,1234,67.890,2865432.10,0
```

Column order (no header row): `open_time_ms, open, high, low, close, volume, close_time_ms, quote_volume, trades, taker_buy_base, taker_buy_quote, ignore`.

The trading engine consumes these via `pkg/marketdata/replay.go` (CSVReplay) for backtests, or via `pkg/marketdata/binance.go` (BinanceFutures) for live mode where it backfills the most recent 48h directly from Binance's REST API on engine startup.

## Symbol universe

The 57 symbols covered by `download_data_all.sh` match the universe in:
- `scripts/p4_oos_persistence.sh:38`
- `scripts/p4_per_symbol.sh:31`
- `scripts/p4_quarterly.sh` (newest)

This list is the result of the cost-survivor battery (2026-05-05) — the universe surviving the realistic-cost filter. Symbols dropped from the original 8-symbol set (or 12-symbol pre-2026-05-05 deploy) are still downloadable individually if needed for archival comparison.

## Disk-space alternatives

If the 12 GB doesn't fit, two options:

1. **Download a subset.** Most analysis only needs the 16 deployed symbols × the last 2 years. That's ~160 files = ~700 MB.
   ```bash
   ./scripts/download_data_all.sh \
     "ROSEUSDT MKRUSDT GRTUSDT 1INCHUSDT ADAUSDT KAVAUSDT 1000SHIBUSDT ENSUSDT XLMUSDT IMXUSDT ETCUSDT RUNEUSDT AVAXUSDT FTMUSDT DOTUSDT FILUSDT" \
     2023 01 2025 04
   ```

2. **Stream-and-discard.** For one-off backtests, run the strategy directly against the .zip URL via a wrapper that pipes through `funzip`. Not currently scripted but trivial to add if needed.

## Refresh cadence

Binance publishes the previous month's full archive within a few days of month-end. To keep `data/` current:

```bash
# At the start of each month, fetch the month that just ended
./scripts/download_data_all.sh "" $(date -v-1m +%Y) $(date -v-1m +%m) $(date -v-1m +%Y) $(date -v-1m +%m)
```

(macOS `date` syntax shown. On Linux: `date -d "1 month ago" +%Y` etc.)

## When this doc gets stale

If `scripts/download_data_all.sh` stops working, the most likely cause is a Binance URL-format change. Check:
- The URL pattern in `scripts/download_data_all.sh:fetch_one` (currently `https://data.binance.vision/data/futures/um/monthly/klines/<SYMBOL>/1m/<SYMBOL>-1m-YYYY-MM.zip`)
- Browse the live archive at [data.binance.vision](https://data.binance.vision/?prefix=data/futures/um/monthly/klines/) to confirm the format
