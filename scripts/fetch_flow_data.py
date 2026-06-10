#!/usr/bin/env python3
"""fetch_flow_data.py — Coinbase premium (#16) + stablecoin supply (#17) data.

#16: Coinbase Exchange daily candles for BTC-USD/ETH-USD → data/flow/coinbase/<SYM>.csv
     (date,close). Paired with Binance spot/perp close (local) to form the premium.
#17: DefiLlama aggregate stablecoin circulating supply (USD) → data/flow/stablecoin.csv
     (date,total_usd). Daily history to 2020.

Stdlib only. Per `tasks/todo.md` Phase 1/3, batch-2 spec #16/#17.
"""
import os, json, csv, time, datetime, urllib.request

OUT = "data/flow"


def http(url, timeout=25):
    req = urllib.request.Request(url, headers={"User-Agent": "research/1.0"})
    with urllib.request.urlopen(req, timeout=timeout) as r:
        return r.read()


def fetch_coinbase(product):
    """Daily candles, paged in 300-candle windows (Coinbase cap)."""
    base = f"https://api.exchange.coinbase.com/products/{product}/candles?granularity=86400"
    start = datetime.datetime(2020, 1, 1, tzinfo=datetime.timezone.utc)
    end = datetime.datetime.now(datetime.timezone.utc)
    out = {}
    cur = start
    while cur < end:
        win_end = min(cur + datetime.timedelta(days=290), end)
        url = f"{base}&start={cur.isoformat()}&end={win_end.isoformat()}"
        try:
            data = json.loads(http(url))
        except Exception as e:
            print(f"  {product} window err: {e}")
            data = []
        for row in data:
            # [time, low, high, open, close, volume]
            d = datetime.datetime.fromtimestamp(row[0], datetime.timezone.utc).strftime("%Y-%m-%d")
            out[d] = row[4]
        cur = win_end
        time.sleep(0.3)
    return out


def fetch_stablecoin():
    url = "https://stablecoins.llama.fi/stablecoincharts/all?stablecoin=1"
    data = json.loads(http(url))
    out = {}
    for p in data:
        try:
            ts = int(p["date"])
            usd = p["totalCirculatingUSD"]["peggedUSD"]
        except (KeyError, ValueError, TypeError):
            continue
        d = datetime.datetime.fromtimestamp(ts, datetime.timezone.utc).strftime("%Y-%m-%d")
        out[d] = usd
    return out


def write(path, rows, cols):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w", newline="") as f:
        w = csv.writer(f); w.writerow(cols)
        for k in sorted(rows):
            w.writerow([k, rows[k]])


def main():
    for prod, sym in (("BTC-USD", "BTCUSDT"), ("ETH-USD", "ETHUSDT")):
        cb = fetch_coinbase(prod)
        p = os.path.join(OUT, "coinbase", f"{sym}.csv")
        write(p, cb, ["date", "cb_close"])
        days = sorted(cb)
        print(f"[coinbase] {sym}: {len(cb)} days {days[0]}..{days[-1]} -> {p}")
    sc = fetch_stablecoin()
    p = os.path.join(OUT, "stablecoin.csv")
    write(p, sc, ["date", "total_usd"])
    days = sorted(sc)
    print(f"[stablecoin] {len(sc)} days {days[0]}..{days[-1]} -> {p}")
    print("DONE.")


if __name__ == "__main__":
    main()
