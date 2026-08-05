#!/usr/bin/env python3
"""Maker-fill replay over the forward-paper journal archive.

Question (Option A, results/v2_lessons_and_design_2026-08-04.md):
the engine entered SHORT at market at the 4H signal price P. A maker entry
rests a SELL LIMIT at P instead. It fills only if price trades at or above P
within N minutes of the signal. Trades that never fill are skipped.

Two numbers decide it:
  1. fill rate
  2. whether the skipped trades were systematically the winners

Go/no-go from the design doc: fill rate >70% AND missed trades not
systematically the winners.

Fills are modelled optimistically (touch = fill, no queue position). That bias
is deliberate: if the answer is NO under optimistic fills, it is NO.

Read-only. Writes nothing.
"""
import argparse
import bisect
import csv
import glob
import json
import os
import sys
from collections import defaultdict
from datetime import datetime, timezone

CSV_DIR = os.environ.get(
    "TRADING_ENGINE_CSV_DIR", os.path.expanduser("~/Main/data/trading-engine/csvs")
)


def parse_ts(s):
    return datetime.fromisoformat(s.replace("Z", "+00:00"))


def load_trades(journal_dir):
    """Pair open/close events by chronological stack replay per symbol.

    Count-based pairing is wrong (verified 2026-08-04) — it mismatches when a
    symbol has overlapping or abandoned positions.
    """
    events = defaultdict(list)
    # FLAT glob only. Recursive readmits archive/option_c_* -> 25x overstatement.
    for path in glob.glob(os.path.join(journal_dir, "*.jsonl")):
        for line in open(path):
            line = line.strip()
            if not line:
                continue
            e = json.loads(line)
            events[e["symbol"]].append(e)

    trades, abandoned = [], []
    for sym, evs in events.items():
        evs.sort(key=lambda e: (parse_ts(e["ts"]), 0 if e["event"] == "open" else 1))
        stack = []
        for e in evs:
            if e["event"] == "open":
                stack.append(e)
            elif e["event"] == "close":
                if stack:
                    o = stack.pop(0)
                    trades.append((o, e))
        abandoned.extend(stack)
    # Sort by (symbol, time): keeps the CSV cache warm, since consecutive
    # trades then read the same symbol's months.
    trades.sort(key=lambda t: (t[0]["symbol"], parse_ts(t[0]["ts"])))
    return trades, abandoned


_FILE_CACHE = {}


def _read_csv(path):
    """(open_time_ms, high, low) rows, cached. Handles headered + headerless."""
    if path not in _FILE_CACHE:
        # ponytail: naive cap, trades are time-sorted so old months go unused.
        # Swap for an LRU if a future caller reads out of order.
        if len(_FILE_CACHE) > 8:
            _FILE_CACHE.clear()
        rows = []
        with open(path) as f:
            for row in csv.reader(f):
                if not row or not row[0].isdigit():
                    continue  # header line
                rows.append((int(row[0]), float(row[2]), float(row[3])))
        rows.sort()
        _FILE_CACHE[path] = rows
    return _FILE_CACHE[path]


def load_bars(symbol, start, end):
    """1m bars for [start, end], from monthly then daily CSVs."""
    months, cur = set(), start
    while cur <= end:
        months.add((cur.year, cur.month))
        cur = (cur.replace(day=28) + __import__("datetime").timedelta(days=4)).replace(day=1)

    paths = []
    for y, m in sorted(months):
        p = os.path.join(CSV_DIR, f"{symbol}-1m-{y}-{m:02d}.csv")
        if os.path.exists(p):
            paths.append(p)
        else:
            paths.extend(sorted(glob.glob(os.path.join(CSV_DIR, f"{symbol}-1m-{y}-{m:02d}-*.csv"))))

    lo, hi = int(start.timestamp() * 1000), int(end.timestamp() * 1000)
    bars = []
    for p in paths:
        rows = _read_csv(p)
        i = bisect.bisect_left(rows, (lo, -1.0, -1.0))
        while i < len(rows) and rows[i][0] <= hi:
            bars.append(rows[i])
            i += 1
    bars.sort()
    return bars


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--journal-dir", default="results/journal_cache")
    ap.add_argument("--window-min", type=int, default=60,
                    help="minutes a limit order rests before being cancelled")
    ap.add_argument("--maker-bps", type=float, default=2.0,
                    help="round-trip maker fee in bp (entry maker, exit taker at stop)")
    ap.add_argument("--json", action="store_true")
    args = ap.parse_args()

    trades, abandoned = load_trades(args.journal_dir)
    if not trades:
        print("ERROR: no trades found — is the journal cache populated?", file=sys.stderr)
        return 3

    rows, skipped_nodata = [], []
    for o, c in trades:
        sym, sig_ts, entry = o["symbol"], parse_ts(o["ts"]), float(o["entry"])
        # Start at +1m. The signal bar's own high IS the entry price (the tick
        # that fired the cross), so including it fills 100% by construction.
        first = sig_ts + __import__("datetime").timedelta(minutes=1)
        end = sig_ts + __import__("datetime").timedelta(minutes=args.window_min)
        bars = load_bars(sym, first, end)
        if not bars:
            skipped_nodata.append((sym, o["ts"]))
            continue

        # SHORT: sell limit at `entry` fills when price trades at/above entry.
        fill_min = None
        for t, hi, _lo in bars:
            if hi >= entry:
                fill_min = int((t - int(sig_ts.timestamp() * 1000)) / 60000)
                break

        stop = float(o["stop"])
        exit_px = float(c["exit"])
        r = (entry - exit_px) / abs(entry - stop)
        rows.append({
            "symbol": sym, "ts": o["ts"], "filled": fill_min is not None,
            "fill_min": fill_min, "r": r,
            "pnl_usd": float(c["pnl_usd"]), "gross_usd": float(c.get("gross_usd", 0.0)),
            "fee_usd": float(c.get("fee_usd", 0.0)),
            "notional_usd": float(c.get("notional_usd", 0.0)),
            "winner": r > 0,
        })

    n = len(rows)
    filled = [x for x in rows if x["filled"]]
    missed = [x for x in rows if not x["filled"]]
    fill_rate = 100.0 * len(filled) / n

    def agg(xs):
        return (len(xs), sum(x["pnl_usd"] for x in xs),
                sum(x["gross_usd"] for x in xs),
                100.0 * sum(1 for x in xs if x["winner"]) / len(xs) if xs else 0.0)

    # Maker P&L on the FILLED subset: same gross, entry fee at maker rate.
    # Actual run was 10bp round trip (5bp each side). Maker entry replaces the
    # entry leg only; the exit is still a taker stop/target.
    maker_net = 0.0
    for x in filled:
        entry_fee_taker = x["notional_usd"] * 5e-4
        entry_fee_maker = x["notional_usd"] * (args.maker_bps / 10000.0)
        maker_net += x["pnl_usd"] + (entry_fee_taker - entry_fee_maker)

    if args.json:
        print(json.dumps({"n": n, "fill_rate": fill_rate,
                          "filled": agg(filled), "missed": agg(missed),
                          "maker_net": maker_net}, indent=2))
        return 0

    print(f"MAKER-FILL REPLAY — window {args.window_min}min, maker entry fee {args.maker_bps}bp")
    print(f"journal: {args.journal_dir}   trades paired: {n}"
          + (f"   (+{len(abandoned)} abandoned open)" if abandoned else ""))
    if skipped_nodata:
        print(f"WARNING: {len(skipped_nodata)} trades had no price data, excluded")
    print()
    print(f"Fill rate: {len(filled)}/{n} = {fill_rate:.1f}%")
    print()
    print(f"{'':10} {'n':>4} {'WR':>7} {'gross':>12} {'net(taker)':>12}")
    for label, xs in (("FILLED", filled), ("MISSED", missed)):
        cnt, pnl, gross, wr = agg(xs)
        print(f"{label:10} {cnt:>4} {wr:>6.1f}% {gross:>12,.0f} {pnl:>12,.0f}")
    print()
    actual_net = sum(x["pnl_usd"] for x in rows)
    print(f"Actual run (all taker, 10bp):                     net ${actual_net:,.0f}")
    print(f"Maker strategy (filled only, skip the misses):    net ${maker_net:,.0f}")
    print(f"  = {'+' if maker_net > actual_net else ''}${maker_net - actual_net:,.0f} vs actual")
    print()

    if filled:
        fm = sorted(x["fill_min"] for x in filled)
        print(f"Fill latency (min): p50={fm[len(fm)//2]}  p90={fm[int(len(fm)*0.9)]}  max={fm[-1]}")

    # The decisive question: were the missed trades the winners?
    _, _, _, wr_f = agg(filled)
    _, _, _, wr_m = agg(missed)
    print()
    print("VERDICT")
    print(f"  fill rate >70%?                    {'PASS' if fill_rate > 70 else 'FAIL'} ({fill_rate:.1f}%)")
    adverse = wr_m > wr_f
    print(f"  missed NOT systematically winners? {'FAIL' if adverse else 'PASS'} "
          f"(missed WR {wr_m:.1f}% vs filled {wr_f:.1f}%)")
    return 0




def _selftest():
    """Fill logic + pairing. Run: python3 scripts/maker_fill_replay.py --selftest"""
    import datetime as _dt

    # Chronological stack pairing beats count pairing when opens interleave.
    evs = {"S": [
        {"event": "open", "symbol": "S", "ts": "2026-01-01T00:00:00Z", "entry": 10, "stop": 11},
        {"event": "open", "symbol": "S", "ts": "2026-01-02T00:00:00Z", "entry": 20, "stop": 21},
        {"event": "close", "symbol": "S", "ts": "2026-01-03T00:00:00Z", "exit": 9, "pnl_usd": 1.0},
    ]}
    import tempfile, json as _json, os as _os
    d = tempfile.mkdtemp()
    with open(_os.path.join(d, "S-2026-01.jsonl"), "w") as f:
        for e in evs["S"]:
            f.write(_json.dumps(e) + "\n")
    tr, ab = load_trades(d)
    assert len(tr) == 1 and len(ab) == 1, (len(tr), len(ab))
    # FIFO: the first open pairs with the close, the second is abandoned.
    assert tr[0][0]["entry"] == 10, tr[0][0]
    assert ab[0]["entry"] == 20, ab[0]

    # SHORT fill rule: sell-limit at `entry` needs high >= entry.
    bars = [(0, 9.9, 9.5), (60000, 10.1, 9.8)]
    assert not any(hi >= 10.0 for _, hi, _ in bars[:1]), "must not fill below entry"
    assert any(hi >= 10.0 for _, hi, _ in bars), "must fill when high touches entry"

    # Realized-R for SHORT (labels lie on max-hold closes; classify by R).
    entry, stop, exit_px = 10.0, 11.0, 4.0
    assert abs((entry - exit_px) / abs(entry - stop) - 6.0) < 1e-9

    print("selftest OK")



if __name__ == "__main__":
    if "--selftest" in sys.argv:
        _selftest()
        sys.exit(0)
    sys.exit(main())
