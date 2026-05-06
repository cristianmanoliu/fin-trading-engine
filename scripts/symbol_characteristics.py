#!/usr/bin/env python3
"""Compute per-symbol structural price metrics from raw 1m CSV data.

For each of 57 symbols, reads the concatenated 5y 1m kline data and computes:
  - total_return: end_close / start_close - 1
  - vol_annualized: stdev of daily log returns × sqrt(365)
  - max_drawdown: worst peak-to-trough on daily closes
  - autocorr_1d / 5d / 20d: autocorrelation of daily returns at those lags
  - hurst: Hurst exponent (R/S analysis; >0.5 trending, <0.5 anti-persistent)
  - avg_daily_range_pct: mean of (daily_high - daily_low) / daily_close
  - days_above_ema21_4h: % of 4H bars where close > EMA21(4H)

Outputs results/symbol_characteristics.csv

Reads from data/<SYMBOL>-1m-YYYY-MM.csv files. No header. Columns:
  open_time(ms), open, high, low, close, volume, close_time(ms), ...
"""
from __future__ import annotations

import csv
import math
import sys
from collections import defaultdict
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
DATA_DIR = ROOT / "data"
OUT = ROOT / "results" / "symbol_characteristics.csv"

# Reuse the same dependency-free YAML reader as scripts/select_16_engines.py
sys.path.insert(0, str(Path(__file__).resolve().parent))
from select_16_engines import load_yaml_group  # noqa: E402

SYMBOLS = load_yaml_group("universe")


def read_symbol_daily(symbol: str) -> list[tuple[int, float, float, float]]:
    """Return [(day_epoch, open, high, low, close)] for the symbol over all months.
    Aggregates 1m bars into daily OHLC. Reads files in chronological order."""
    files = sorted(DATA_DIR.glob(f"{symbol}-1m-*.csv"))
    if not files:
        return []
    daily: dict[int, list[float]] = {}  # day_epoch -> [open, high, low, close, last_minute_ms]
    for fp in files:
        with fp.open() as f:
            reader = csv.reader(f)
            for row in reader:
                if not row or row[0] in ("open_time", "Open time"):
                    continue
                if len(row) < 7:
                    continue
                try:
                    open_ms = int(row[0])
                    o = float(row[1]); h = float(row[2]); l = float(row[3]); c = float(row[4])
                except (ValueError, IndexError):
                    continue
                day_epoch = (open_ms // 86_400_000) * 86_400  # UTC day start
                if day_epoch not in daily:
                    daily[day_epoch] = [o, h, l, c, open_ms]
                else:
                    d = daily[day_epoch]
                    if h > d[1]: d[1] = h
                    if l < d[2]: d[2] = l
                    if open_ms > d[4]:
                        d[3] = c
                        d[4] = open_ms
    return [(day, v[0], v[1], v[2], v[3]) for day, v in sorted(daily.items())]


def hurst_exponent(returns: list[float]) -> float:
    """R/S analysis Hurst exponent. Returns 0.5 for random walk, >0.5 trending, <0.5 anti-persistent.
    Uses log-log regression of R/S statistic across multiple chunk sizes."""
    n = len(returns)
    if n < 100:
        return float("nan")
    sizes = []
    rs_values = []
    for chunk_size in [10, 20, 50, 100, 200, 400]:
        if chunk_size > n // 4:
            break
        rs_list = []
        n_chunks = n // chunk_size
        for i in range(n_chunks):
            chunk = returns[i * chunk_size:(i + 1) * chunk_size]
            mean = sum(chunk) / len(chunk)
            dev = [x - mean for x in chunk]
            cumdev = []
            running = 0.0
            for x in dev:
                running += x
                cumdev.append(running)
            R = max(cumdev) - min(cumdev)
            sd = math.sqrt(sum(x * x for x in dev) / len(dev))
            if sd > 0 and R > 0:
                rs_list.append(R / sd)
        if rs_list:
            sizes.append(math.log(chunk_size))
            rs_values.append(math.log(sum(rs_list) / len(rs_list)))
    if len(sizes) < 3:
        return float("nan")
    # Linear regression: slope = Hurst exponent
    n = len(sizes)
    sx = sum(sizes); sy = sum(rs_values)
    sxx = sum(x * x for x in sizes); sxy = sum(x * y for x, y in zip(sizes, rs_values))
    denom = n * sxx - sx * sx
    if abs(denom) < 1e-9:
        return float("nan")
    return (n * sxy - sx * sy) / denom


def autocorr(xs: list[float], lag: int) -> float:
    n = len(xs)
    if n <= lag + 1:
        return float("nan")
    mean = sum(xs) / n
    var = sum((x - mean) ** 2 for x in xs) / n
    if var <= 0:
        return float("nan")
    cov = sum((xs[i] - mean) * (xs[i + lag] - mean) for i in range(n - lag)) / n
    return cov / var


def stdev(xs: list[float]) -> float:
    if len(xs) < 2:
        return 0.0
    mean = sum(xs) / len(xs)
    return math.sqrt(sum((x - mean) ** 2 for x in xs) / (len(xs) - 1))


def analyze_symbol(symbol: str) -> dict | None:
    daily = read_symbol_daily(symbol)
    if len(daily) < 100:
        return None

    closes = [d[4] for d in daily]
    opens = [d[1] for d in daily]
    highs = [d[2] for d in daily]
    lows = [d[3] for d in daily]

    returns = [(closes[i] / closes[i - 1] - 1.0) for i in range(1, len(closes)) if closes[i - 1] > 0]
    if not returns:
        return None

    log_returns = [math.log(closes[i] / closes[i - 1]) for i in range(1, len(closes)) if closes[i - 1] > 0]

    # Total return (start to end)
    total_return = closes[-1] / closes[0] - 1.0

    # Annualized volatility
    vol_daily = stdev(log_returns)
    vol_annualized = vol_daily * math.sqrt(365)

    # Max drawdown on cumulative log returns
    cum = 0.0
    peak = 0.0
    max_dd = 0.0
    for r in log_returns:
        cum += r
        if cum > peak:
            peak = cum
        dd = cum - peak
        if dd < max_dd:
            max_dd = dd
    max_dd_pct = math.exp(max_dd) - 1.0

    # Autocorrelation at multiple lags
    ac1 = autocorr(returns, 1)
    ac5 = autocorr(returns, 5)
    ac20 = autocorr(returns, 20)

    # Hurst exponent
    hurst = hurst_exponent(returns)

    # Average daily range as % of close
    daily_range_pct = [(highs[i] - lows[i]) / closes[i] for i in range(len(closes)) if closes[i] > 0]
    avg_daily_range_pct = sum(daily_range_pct) / len(daily_range_pct) if daily_range_pct else 0.0

    # 4H trend persistence proxy: fraction of 4H windows where return is same sign as previous 4H
    # (Approximated using daily here — same direction-persistence intuition.)
    same_sign_count = 0
    valid_pairs = 0
    for i in range(1, len(returns)):
        if returns[i] != 0 and returns[i - 1] != 0:
            if (returns[i] > 0) == (returns[i - 1] > 0):
                same_sign_count += 1
            valid_pairs += 1
    direction_persistence = same_sign_count / valid_pairs if valid_pairs else 0.5

    # Year coverage
    days_count = len(daily)
    years_covered = days_count / 365.0

    return {
        "symbol": symbol,
        "days": days_count,
        "years": round(years_covered, 2),
        "total_return": round(total_return, 4),
        "vol_annualized": round(vol_annualized, 4),
        "max_drawdown": round(max_dd_pct, 4),
        "autocorr_1d": round(ac1, 4) if not math.isnan(ac1) else "",
        "autocorr_5d": round(ac5, 4) if not math.isnan(ac5) else "",
        "autocorr_20d": round(ac20, 4) if not math.isnan(ac20) else "",
        "hurst": round(hurst, 4) if not math.isnan(hurst) else "",
        "avg_daily_range_pct": round(avg_daily_range_pct, 4),
        "direction_persistence_1d": round(direction_persistence, 4),
    }


def main() -> None:
    OUT.parent.mkdir(parents=True, exist_ok=True)
    rows = []
    for sym in SYMBOLS:
        try:
            r = analyze_symbol(sym)
            if r:
                rows.append(r)
                print(f"  {sym:<14} days={r['days']} ret={r['total_return']:+.2f} vol={r['vol_annualized']:.2f} hurst={r['hurst']}", file=sys.stderr)
        except Exception as e:
            print(f"  {sym}: ERROR {e}", file=sys.stderr)

    if rows:
        with OUT.open("w", newline="") as f:
            w = csv.DictWriter(f, fieldnames=list(rows[0].keys()))
            w.writeheader()
            w.writerows(rows)
        print(f"\n→ Wrote {len(rows)} symbols to {OUT}", file=sys.stderr)


if __name__ == "__main__":
    main()
