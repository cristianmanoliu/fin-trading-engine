#!/usr/bin/env python3
"""cat_x_bybit_replication.py — apply the locked rule from
`results/cat_x_bybit_replication_decision_rule_2026-05-08.md`.

Pulls 4H klines from Bybit for deployed-16 symbols (cached on disk),
downsamples cached Binance 1m data to 4H, runs the same Python
4H-granularity backtester on both, compares per-window NET, applies
mechanical verdict.
"""

from __future__ import annotations

import csv
import json
import sys
import time
import urllib.error
import urllib.request
from datetime import datetime, timezone
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
BINANCE_1M_DIR = ROOT / "data"
BYBIT_CACHE_DIR = ROOT / "data" / "bybit"
BYBIT_CACHE_DIR.mkdir(exist_ok=True)

DEPLOYED_16 = [
    "ROSEUSDT", "BCHUSDT", "GRTUSDT", "1INCHUSDT", "ADAUSDT", "KAVAUSDT",
    "1000SHIBUSDT", "ENSUSDT", "XLMUSDT", "IMXUSDT", "ETCUSDT", "RUNEUSDT",
    "AVAXUSDT", "APTUSDT", "DOTUSDT", "FILUSDT",
]

# Naming alignment: project uses Binance names; Bybit name only differs for one.
BYBIT_NAME = {sym: sym for sym in DEPLOYED_16}
BYBIT_NAME["1000SHIBUSDT"] = "SHIB1000USDT"

# Locked walk-forward windows (subset comparable on Bybit).
WINDOWS = [
    ("W0",  datetime(2022, 5, 1, tzinfo=timezone.utc),  datetime(2023, 4, 30, tzinfo=timezone.utc)),
    ("W1",  datetime(2023, 5, 1, tzinfo=timezone.utc),  datetime(2024, 4, 30, tzinfo=timezone.utc)),
    ("W2",  datetime(2024, 5, 1, tzinfo=timezone.utc),  datetime(2025, 4, 30, tzinfo=timezone.utc)),
    ("W3",  datetime(2025, 5, 1, tzinfo=timezone.utc),  datetime(2026, 4, 30, tzinfo=timezone.utc)),
]

# Strategy / cost stack constants (locked, matches deployed)
EMA_FAST = 9
EMA_SLOW = 21
RR = 6.0
MAX_HOLD_HOURS = 504
STAKE_USD = 1000
FEE_BPS = 10.0
SLIP_BPS = 5.0


def fetch_bybit_4h(symbol: str, start_ms: int, end_ms: int) -> list[list]:
    """Fetch 4H klines from Bybit. Pagination via descending cursor."""
    bybit_sym = BYBIT_NAME[symbol]
    out: list[list] = []
    cursor_end = end_ms
    while cursor_end > start_ms:
        url = (f"https://api.bybit.com/v5/market/kline?"
               f"category=linear&symbol={bybit_sym}&interval=240&"
               f"start={start_ms}&end={cursor_end}&limit=1000")
        try:
            with urllib.request.urlopen(url, timeout=20) as r:
                d = json.loads(r.read())
        except (urllib.error.URLError, urllib.error.HTTPError) as e:
            print(f"  WARN: fetch error for {symbol}: {e}, retrying after 5s", file=sys.stderr)
            time.sleep(5)
            continue
        if d["retCode"] != 0:
            print(f"  WARN: API err for {symbol}: {d['retMsg']}", file=sys.stderr)
            break
        rows = d["result"]["list"]
        if not rows:
            break
        # Bybit returns descending. Sort ascending for our use.
        out.extend(rows)
        # The earliest open_time of this batch becomes the new end cursor.
        oldest_ts = int(rows[-1][0])
        if oldest_ts <= start_ms or len(rows) < 1000:
            break
        cursor_end = oldest_ts - 1
        time.sleep(0.1)  # throttle
    # Dedupe by timestamp + sort ascending.
    by_ts: dict[int, list] = {}
    for r in out:
        by_ts[int(r[0])] = r
    return sorted(by_ts.values(), key=lambda r: int(r[0]))


def cached_or_pull_bybit(symbol: str) -> list[tuple[int, float, float, float, float]]:
    """Return list of (open_time_ms, open, high, low, close) for symbol,
    using on-disk cache."""
    cache = BYBIT_CACHE_DIR / f"{BYBIT_NAME[symbol]}-4h.csv"
    if cache.exists():
        out = []
        with cache.open() as f:
            reader = csv.reader(f)
            next(reader)  # header
            for row in reader:
                out.append((int(row[0]), float(row[1]), float(row[2]), float(row[3]), float(row[4])))
        return out
    # Pull range covering all comparable windows + 4H EMA priming buffer (~30 days back).
    start = datetime(2021, 1, 1, tzinfo=timezone.utc)
    end = datetime(2026, 5, 1, tzinfo=timezone.utc)
    print(f"  pulling Bybit 4H for {symbol} ({start.date()} → {end.date()})...")
    rows = fetch_bybit_4h(symbol, int(start.timestamp() * 1000), int(end.timestamp() * 1000))
    out = []
    with cache.open("w", newline="") as f:
        w = csv.writer(f)
        w.writerow(["open_time_ms", "open", "high", "low", "close"])
        for r in rows:
            ts = int(r[0])
            o, h, l, c = float(r[1]), float(r[2]), float(r[3]), float(r[4])
            w.writerow([ts, o, h, l, c])
            out.append((ts, o, h, l, c))
    print(f"    {len(out)} 4H candles cached")
    return out


def downsample_binance_1m_to_4h(symbol: str) -> list[tuple[int, float, float, float, float]]:
    """Read all monthly 1m CSVs for symbol; aggregate to 4H."""
    files = sorted(BINANCE_1M_DIR.glob(f"{symbol}-1m-*.csv"))
    if not files:
        return []
    bucket_size_ms = 4 * 60 * 60 * 1000  # 4 hours
    buckets: dict[int, dict] = {}
    for fp in files:
        with fp.open() as f:
            reader = csv.reader(f)
            next(reader)  # header
            for row in reader:
                ts = int(row[0])
                bucket_ts = (ts // bucket_size_ms) * bucket_size_ms
                o, h, l, c = float(row[1]), float(row[2]), float(row[3]), float(row[4])
                b = buckets.get(bucket_ts)
                if b is None:
                    buckets[bucket_ts] = {"open": o, "high": h, "low": l, "close": c, "first_ts": ts, "last_ts": ts}
                else:
                    if ts < b["first_ts"]:
                        b["open"] = o
                        b["first_ts"] = ts
                    if ts > b["last_ts"]:
                        b["close"] = c
                        b["last_ts"] = ts
                    if h > b["high"]:
                        b["high"] = h
                    if l < b["low"]:
                        b["low"] = l
    out = []
    for ts in sorted(buckets):
        b = buckets[ts]
        out.append((ts, b["open"], b["high"], b["low"], b["close"]))
    return out


def ema(values: list[float], period: int) -> list[float]:
    """Compute EMA series (start with SMA seed at index period-1)."""
    if len(values) < period:
        return [0.0] * len(values)
    out = [0.0] * len(values)
    seed = sum(values[:period]) / period
    out[period - 1] = seed
    k = 2.0 / (period + 1)
    for i in range(period, len(values)):
        out[i] = (values[i] - out[i - 1]) * k + out[i - 1]
    return out


def backtest_4h(candles: list[tuple[int, float, float, float, float]], symbol: str) -> list[dict]:
    """Run the 4H-granularity backtester. Returns list of closed trades."""
    if len(candles) < EMA_SLOW + 5:
        return []
    closes = [c[4] for c in candles]
    e_fast = ema(closes, EMA_FAST)
    e_slow = ema(closes, EMA_SLOW)
    trades: list[dict] = []
    open_pos: dict | None = None
    for i in range(EMA_SLOW + 1, len(candles)):
        ts_ms, o, h, l, c = candles[i]
        ts = datetime.fromtimestamp(ts_ms / 1000, timezone.utc)
        # First check exit conditions on open position.
        if open_pos is not None:
            sig = open_pos
            # Same-bar resolution for SHORT: pessimistic = high-first ordering →
            # if H ≥ stop AND L ≤ target both true, use STOP.
            stop_hit = h >= sig["stop"]
            target_hit = l <= sig["target"]
            age_h = (ts - sig["entry_ts"]).total_seconds() / 3600
            if stop_hit and target_hit:
                exit_price = sig["stop"]
                won = False
                outcome = "STOP"
            elif stop_hit:
                exit_price = sig["stop"]
                won = False
                outcome = "STOP"
            elif target_hit:
                exit_price = sig["target"]
                won = True
                outcome = "TARGET"
            elif age_h >= MAX_HOLD_HOURS:
                exit_price = c  # force-close at this candle's close
                won = exit_price < sig["entry"]
                outcome = "MAXHOLD"
            else:
                continue  # position still open, no signal change
            stop_dist = abs(sig["entry"] - sig["stop_orig"])
            units = STAKE_USD / stop_dist if stop_dist > 0 else 0
            gross = units * (sig["entry"] - exit_price)  # SHORT: entry - exit
            notional = units * sig["entry"]
            fee = FEE_BPS / 10000 * notional
            slip = SLIP_BPS / 10000 * notional if not won else 0
            pnl = gross - fee - slip
            trades.append({
                "ts": sig["entry_ts"],
                "symbol": symbol,
                "entry": sig["entry"],
                "exit": exit_price,
                "side": "SHORT",
                "outcome": outcome,
                "pnl_usd": pnl,
            })
            open_pos = None
        # Then check for new entry signal: bearish 4H EMA cross at this candle close.
        if open_pos is None and e_slow[i] > 0 and e_slow[i - 1] > 0:
            crossed_down = (e_fast[i - 1] >= e_slow[i - 1]) and (e_fast[i] < e_slow[i])
            if crossed_down:
                # Stop = high of just-closed candle; target = entry - 6× stop_dist
                entry_price = c
                stop_price = h  # wick stop
                if stop_price <= entry_price:
                    continue  # invalid signal (no wick above close)
                stop_dist = stop_price - entry_price
                target_price = entry_price - RR * stop_dist
                if target_price <= 0:
                    continue
                open_pos = {
                    "entry_ts": ts,
                    "entry": entry_price,
                    "stop": stop_price,
                    "stop_orig": stop_price,
                    "target": target_price,
                }
    return trades


def fmt_dollar(x: float) -> str:
    sign = "-" if x < 0 else "+"
    return f"{sign}${abs(x):,.0f}"


def aggregate_per_window(trades: list[dict]) -> dict:
    """Map window label → {n, net, wins}."""
    out: dict = {}
    for label, start, end in WINDOWS:
        wt = [t for t in trades if start <= t["ts"] < end]
        wins = sum(1 for t in wt if t["outcome"] == "TARGET")
        out[label] = {
            "n": len(wt),
            "net": sum(t["pnl_usd"] for t in wt),
            "wins": wins,
        }
    return out


def main() -> int:
    print("CAT X — CROSS-EXCHANGE OOS REPLICATION ON BYBIT (locked)")
    print("=" * 80)
    print("Pre-reg:  cat_x_bybit_replication_decision_rule_2026-05-08.md")
    print(f"Strategy: 4H short EMA{EMA_FAST}/{EMA_SLOW}, RR={RR}, mh={MAX_HOLD_HOURS}h, "
          f"fee={FEE_BPS}bp slip={SLIP_BPS}bp, no funding")
    print()

    print("Loading data...")
    bybit_data: dict[str, list] = {}
    binance_data: dict[str, list] = {}
    for sym in DEPLOYED_16:
        bybit_data[sym] = cached_or_pull_bybit(sym)
        binance_data[sym] = downsample_binance_1m_to_4h(sym)
        n_by = len(bybit_data[sym])
        n_bn = len(binance_data[sym])
        print(f"  {sym:<14} bybit 4H: {n_by:>5}  binance-1m→4H: {n_bn:>5}")
    print()

    # Backtest each.
    print("Running backtests...")
    bybit_trades: list[dict] = []
    binance_trades: list[dict] = []
    for sym in DEPLOYED_16:
        bt_b = backtest_4h(bybit_data[sym], sym)
        bt_n = backtest_4h(binance_data[sym], sym)
        bybit_trades.extend(bt_b)
        binance_trades.extend(bt_n)
    print(f"  Bybit:   {len(bybit_trades):,} trades")
    print(f"  Binance: {len(binance_trades):,} trades")
    print()

    by_w = aggregate_per_window(bybit_trades)
    bn_w = aggregate_per_window(binance_trades)

    # Per-window comparison.
    print(f"  {'Win':<5} {'Range':<24} {'Binance-4H N':>12} {'Binance-4H NET':>16} "
          f"{'Bybit N':>9} {'Bybit NET':>14} {'sign-match':>11}")
    print(f"  {'-'*5} {'-'*24} {'-'*12} {'-'*16} {'-'*9} {'-'*14} {'-'*11}")
    sign_matches = 0
    valid_windows = 0
    for label, start, end in WINDOWS:
        b = bn_w[label]; y = by_w[label]
        b_sign = b["net"] > 0
        y_sign = y["net"] > 0
        if b["n"] > 5 and y["n"] > 5:
            valid_windows += 1
            if b_sign == y_sign:
                sign_matches += 1
            sm = "✓" if b_sign == y_sign else "✗"
        else:
            sm = "(low n)"
        print(f"  {label:<5} {start.date()}→{end.date()} {b['n']:>12} "
              f"{fmt_dollar(b['net']):>16} {y['n']:>9} {fmt_dollar(y['net']):>14} {sm:>11}")
    print()

    binance_total = sum(bn_w[l]["net"] for l, *_ in [(w[0], w[1], w[2]) for w in WINDOWS])
    bybit_total = sum(by_w[l]["net"] for l, *_ in [(w[0], w[1], w[2]) for w in WINDOWS])
    ratio = bybit_total / binance_total if binance_total != 0 else 0

    print("Aggregate (across all 4 comparable windows):")
    print(f"  Binance-4H total NET   : {fmt_dollar(binance_total)}")
    print(f"  Bybit total NET        : {fmt_dollar(bybit_total)}")
    print(f"  Bybit / Binance ratio  : {ratio:.2f}×")
    print(f"  Sign-matched windows   : {sign_matches}/{valid_windows}")
    print()

    # Apply locked rule.
    print("=" * 80)
    print("DECISION RULE EVALUATION (locked):")
    print()

    robust = (bybit_total >= 0.5 * binance_total) and (sign_matches >= 3) and (bybit_total > 0)
    weak = (bybit_total > 0) and (sign_matches >= 2)

    print(f"  ROBUST              Bybit ≥ 0.5× Binance: {ratio:.2f}× {'✓' if (bybit_total >= 0.5 * binance_total and binance_total > 0) else '✗'} | "
          f"≥3/4 sign-match: {sign_matches}/4 {'✓' if sign_matches >= 3 else '✗'} | "
          f"Bybit > 0: {fmt_dollar(bybit_total)} {'✓' if bybit_total > 0 else '✗'}")
    print(f"  WEAK_REPLICATION    Bybit > 0: {fmt_dollar(bybit_total)} {'✓' if bybit_total > 0 else '✗'} | "
          f"≥2/4 sign-match: {sign_matches}/4 {'✓' if sign_matches >= 2 else '✗'}")
    print(f"  BINANCE-SPECIFIC    Bybit ≤ 0: {fmt_dollar(bybit_total)} {'✓' if bybit_total <= 0 else '✗'} | "
          f"<2/4 sign-match: {sign_matches}/4 {'✓' if sign_matches < 2 else '✗'} (OR triggers)")

    if robust:
        verdict = "ROBUST"
    elif weak:
        verdict = "WEAK_REPLICATION"
    else:
        verdict = "BINANCE-SPECIFIC"

    print()
    print("=" * 80)
    print(f"VERDICT: {verdict}")
    print("=" * 80)

    return 0


if __name__ == "__main__":
    sys.exit(main())
