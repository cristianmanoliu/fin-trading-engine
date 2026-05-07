#!/usr/bin/env python3
"""BTC-HODL benchmark for forward-paper go/no-go criteria.

Reads close events from stdin as TSV (ts, symbol, pnl_usd, outcome) — one row
per close — and computes, for the strategy aggregated as a whole:

  CUMULATIVE  : sum(pnl_usd) across all closes vs HODL_PnL = N × (P_now/P_first - 1)
                where N = --benchmark-notional and P is BTCUSDT spot/futures
                close. Implements the deploy criterion "Live PnL beats BTC HODL
                with $N notional over the same window."

  ROLLING 30D : non-overlapping 30-day windows from first_close_ts forward.
                For each window: strategy_NET vs HODL_PnL = N × (P_end/P_start - 1).
                Flag any pair of CONSECUTIVE windows where strategy underperforms
                HODL by more than --kill-threshold-usd. Implements the kill
                criterion "Two consecutive 30-day windows underperform BTC-HODL
                benchmark by >$5k each."

BTC daily klines are fetched from Binance USDT-M Futures
(fapi.binance.com/fapi/v1/klines). Daily close at 00:00:00 UTC is used as the
reference price; intraday timestamps are mapped to the most recent daily close.

Output is a single tab-separated line ready for shell consumption:

  cumulative_strategy_usd  cumulative_hodl_usd  cumulative_delta_usd \
  num_30d_windows  num_consecutive_underperf_pairs  kill_triggered  warning

`warning` is non-empty (a short string) when the benchmark could not be
computed (e.g. no closes, BTC API failure, window too short). Callers should
treat warning as informational; numeric fields stay 0 in that case.

Usage:
  cat closes.tsv | btc_hodl_benchmark.py \
      --benchmark-notional 32000 \
      --kill-threshold-usd 5000

Exit code is always 0 even on warning — the caller decides what to do.
"""

import argparse
import datetime as dt
import json
import sys
import urllib.request


BINANCE_KLINES_URL = "https://fapi.binance.com/fapi/v1/klines"


def fetch_btc_daily_closes(start_ts: dt.datetime, end_ts: dt.datetime) -> dict[dt.date, float]:
    """Fetch daily BTCUSDT closes from Binance covering [start_ts, end_ts].

    Returns a dict keyed by UTC date → close price. Daily klines from Binance
    are anchored at 00:00:00 UTC.
    """
    # Pad ±1 day so caller can look up either side of any close timestamp.
    start_ms = int((start_ts - dt.timedelta(days=1)).timestamp() * 1000)
    end_ms = int((end_ts + dt.timedelta(days=1)).timestamp() * 1000)
    url = f"{BINANCE_KLINES_URL}?symbol=BTCUSDT&interval=1d&startTime={start_ms}&endTime={end_ms}&limit=1500"
    req = urllib.request.Request(url, headers={"User-Agent": "btc_hodl_benchmark"})
    with urllib.request.urlopen(req, timeout=30) as resp:
        data = json.loads(resp.read())
    out: dict[dt.date, float] = {}
    for k in data:
        # k = [open_time_ms, open, high, low, close, volume, close_time_ms, ...]
        open_ms = k[0]
        close_price = float(k[4])
        out[dt.datetime.fromtimestamp(open_ms / 1000, dt.timezone.utc).date()] = close_price
    return out


def closest_close(prices: dict[dt.date, float], when: dt.datetime) -> float | None:
    """Return the daily close for `when`'s UTC date, falling back to the most
    recent prior date with data. None if no usable price exists."""
    target = when.astimezone(dt.timezone.utc).date()
    for delta in range(0, 8):  # try up to a week back
        candidate = target - dt.timedelta(days=delta)
        if candidate in prices:
            return prices[candidate]
    return None


def parse_close_events(stream) -> list[tuple[dt.datetime, float, str]]:
    """Parse stdin TSV: ts, symbol, pnl_usd, outcome. Returns sorted (ts, pnl_usd, outcome)."""
    events: list[tuple[dt.datetime, float, str]] = []
    for line in stream:
        line = line.rstrip("\n")
        if not line:
            continue
        cols = line.split("\t")
        if len(cols) < 4:
            continue
        ts_str, _symbol, pnl_str, outcome = cols[0], cols[1], cols[2], cols[3]
        # Strip RFC3339 tz suffix variants — close events are always UTC.
        ts_str = ts_str.replace("Z", "+00:00")
        try:
            ts = dt.datetime.fromisoformat(ts_str)
        except ValueError:
            continue
        try:
            pnl = float(pnl_str)
        except ValueError:
            continue
        events.append((ts, pnl, outcome))
    events.sort(key=lambda e: e[0])
    return events


def emit(strategy_usd: float, hodl_usd: float, delta_usd: float,
         n_windows: int, n_kill_pairs: int, kill: bool, warning: str = "") -> None:
    print("\t".join([
        f"{strategy_usd:.2f}",
        f"{hodl_usd:.2f}",
        f"{delta_usd:.2f}",
        str(n_windows),
        str(n_kill_pairs),
        "1" if kill else "0",
        warning,
    ]))


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--benchmark-notional", type=float, required=True,
                    help="Notional ($) of the BTC HODL benchmark — typically matches deployed strategy notional.")
    ap.add_argument("--kill-threshold-usd", type=float, default=5000,
                    help="Per-window underperformance ($) that counts toward the consecutive-window kill rule.")
    args = ap.parse_args()

    events = parse_close_events(sys.stdin)
    if not events:
        emit(0.0, 0.0, 0.0, 0, 0, False, "no-closes")
        return 0

    first_ts = events[0][0]
    last_ts = events[-1][0]
    now = dt.datetime.now(dt.timezone.utc)
    end_for_fetch = max(last_ts, now)

    try:
        prices = fetch_btc_daily_closes(first_ts, end_for_fetch)
    except Exception as e:  # network, parse, anything
        emit(0.0, 0.0, 0.0, 0, 0, False, f"btc-fetch-failed:{type(e).__name__}")
        return 0

    p_first = closest_close(prices, first_ts)
    p_now = closest_close(prices, now)
    if p_first is None or p_now is None or p_first <= 0:
        emit(0.0, 0.0, 0.0, 0, 0, False, "no-btc-price")
        return 0

    # Cumulative: strategy NET vs HODL ($N × pct_return) over [first, now].
    strategy_total = sum(e[1] for e in events)
    hodl_total = args.benchmark_notional * (p_now / p_first - 1.0)
    delta_total = strategy_total - hodl_total

    # Rolling 30-day windows, non-overlapping, from first_ts.
    window_size = dt.timedelta(days=30)
    windows: list[tuple[dt.datetime, dt.datetime, float, float]] = []
    cursor = first_ts
    while cursor + window_size <= now:
        win_end = cursor + window_size
        win_strategy = sum(e[1] for e in events if cursor <= e[0] < win_end)
        p_start = closest_close(prices, cursor)
        p_end = closest_close(prices, win_end)
        if p_start and p_end and p_start > 0:
            win_hodl = args.benchmark_notional * (p_end / p_start - 1.0)
            windows.append((cursor, win_end, win_strategy, win_hodl))
        cursor = win_end

    # Kill rule: ANY pair of consecutive windows where (strategy − hodl) < −threshold in BOTH.
    kill_pairs = 0
    prev_underperf = False
    triggered = False
    for _, _, win_s, win_h in windows:
        underperf = (win_s - win_h) < -args.kill_threshold_usd
        if underperf and prev_underperf:
            kill_pairs += 1
            triggered = True
        prev_underperf = underperf

    emit(strategy_total, hodl_total, delta_total, len(windows), kill_pairs, triggered)
    return 0


if __name__ == "__main__":
    sys.exit(main())
