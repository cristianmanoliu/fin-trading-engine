#!/usr/bin/env python3
"""fetch_crossvenue_funding.py — Bybit + OKX funding history for candidate #13.

Pulls full funding-rate history (paginated) from Bybit (v5 linear) and OKX (SWAP)
public REST for a symbol set, writes data/xvenue/{bybit,okx}/<SYM>.csv with the
SAME schema as the local Binance funding CSVs (funding_time_ms,funding_rate) so the
#13 harness can diff them directly. Per `tasks/todo.md` Phase 1 + batch-2 spec #13.

Stdlib only (urllib) — no pandas/requests. Idempotent: skips a venue/symbol CSV
that already exists. Polite pacing between calls.

Usage: fetch_crossvenue_funding.py [SYM1 SYM2 ...]   (default: deployed set)
"""
import os, sys, json, csv, time, urllib.request, urllib.parse

OUT = "data/xvenue"
DEPLOYED = ["BTCUSDT", "ETHUSDT", "BCHUSDT", "ADAUSDT", "AVAXUSDT", "DOTUSDT",
            "FILUSDT", "ETCUSDT", "XLMUSDT", "TRXUSDT", "GRTUSDT", "RUNEUSDT",
            "APTUSDT", "CHZUSDT", "SANDUSDT", "1INCHUSDT", "KAVAUSDT", "ENSUSDT",
            "IMXUSDT", "ROSEUSDT", "1000SHIBUSDT"]


def http_json(url, timeout=20):
    req = urllib.request.Request(url, headers={"User-Agent": "research/1.0"})
    with urllib.request.urlopen(req, timeout=timeout) as r:
        return json.loads(r.read().decode())


def fetch_bybit(sym):
    """Bybit v5 linear funding history, paginated backwards via endTime."""
    rows = []
    end = None
    for _ in range(200):  # hard cap on pages
        url = ("https://api.bybit.com/v5/market/funding/history?category=linear"
               f"&symbol={sym}&limit=200")
        if end is not None:
            url += f"&endTime={end}"
        try:
            d = http_json(url)
        except Exception as e:
            print(f"    bybit {sym} page err: {e}", file=sys.stderr)
            break
        lst = d.get("result", {}).get("list", [])
        if not lst:
            break
        for x in lst:
            rows.append((int(x["fundingRateTimestamp"]), float(x["fundingRate"])))
        # next page: older than the oldest we got
        oldest = min(int(x["fundingRateTimestamp"]) for x in lst)
        if end is not None and oldest >= end:
            break
        end = oldest - 1
        if len(lst) < 200:
            break
        time.sleep(0.15)
    return rows


def fetch_okx(sym):
    """OKX SWAP funding history, paginated backwards via `before`/after ts."""
    inst = sym.replace("USDT", "-USDT-SWAP")
    rows = []
    after = None
    for _ in range(200):
        url = ("https://www.okx.com/api/v5/public/funding-rate-history?"
               f"instId={inst}&limit=100")
        if after is not None:
            url += f"&after={after}"
        try:
            d = http_json(url)
        except Exception as e:
            print(f"    okx {sym} page err: {e}", file=sys.stderr)
            break
        lst = d.get("data", [])
        if not lst:
            break
        for x in lst:
            rows.append((int(x["fundingTime"]), float(x["fundingRate"])))
        oldest = min(int(x["fundingTime"]) for x in lst)
        if after is not None and oldest >= after:
            break
        after = oldest
        if len(lst) < 100:
            break
        time.sleep(0.15)
    return rows


def write_csv(path, rows):
    rows = sorted(set(rows))
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w", newline="") as f:
        w = csv.writer(f)
        w.writerow(["funding_time_ms", "funding_rate"])
        for ts, rate in rows:
            w.writerow([ts, rate])
    return len(rows)


def main():
    syms = sys.argv[1:] or DEPLOYED
    for sym in syms:
        for venue, fn in (("bybit", fetch_bybit), ("okx", fetch_okx)):
            path = os.path.join(OUT, venue, f"{sym}.csv")
            if os.path.exists(path) and os.path.getsize(path) > 50:
                print(f"[{venue}] {sym} exists, skip")
                continue
            rows = fn(sym)
            n = write_csv(path, rows) if rows else 0
            print(f"[{venue}] {sym}: {n} funding rows -> {path}")
            time.sleep(0.2)
    print("DONE.")


if __name__ == "__main__":
    main()
