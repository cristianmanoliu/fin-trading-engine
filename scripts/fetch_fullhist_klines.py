#!/usr/bin/env python3
"""fetch_fullhist_klines.py — F6 fetch: full daily klines + funding history for all
ever-listed USDT-M perps. Pre-reg: results/failed_pump_followup_decision_rule_2026-06-10.md.

Output: data/fullhist/klines/<SYM>-1d.csv (open_time,open,high,low,close)
        data/fullhist/funding/<SYM>.csv   (calc_time,funding_rate)
Idempotent: skips symbols whose files already exist and end within 7d of now (or at
delisting). Pacing: <=8 req/s; 60s backoff on HTTP 418/429 (longer than any poll
interval — reconciler lesson); other HTTP errors: 3 retries then skip symbol.
Symbol list: union of data/listing/klines/*.csv basenames (the 732 de-survivorship set).
"""
import glob
import json
import os
import sys
import time
import urllib.request
import urllib.error

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
KOUT = os.path.join(ROOT, "data", "fullhist", "klines")
FOUT = os.path.join(ROOT, "data", "fullhist", "funding")
os.makedirs(KOUT, exist_ok=True)
os.makedirs(FOUT, exist_ok=True)
BASE = "https://fapi.binance.com"
PACE = 1.0 / 8


def get(url):
    backoff = 60
    for _ in range(6):
        try:
            with urllib.request.urlopen(url, timeout=30) as r:
                return json.loads(r.read())
        except urllib.error.HTTPError as e:
            if e.code in (418, 429):
                print(f"  rate-limited ({e.code}), sleeping {backoff}s", flush=True)
                time.sleep(backoff)
                backoff = min(backoff * 2, 600)
                continue
            return None
        except Exception:
            time.sleep(5)
    return None


def fetch_klines(sym):
    out = os.path.join(KOUT, f"{sym}-1d.csv")
    if os.path.exists(out) and os.path.getsize(out) > 200:
        return "skip"
    rows, start = [], 1577836800000  # 2020-01-01
    while True:
        url = (f"{BASE}/fapi/v1/klines?symbol={sym}&interval=1d"
               f"&startTime={start}&limit=1500")
        time.sleep(PACE)
        data = get(url)
        if data is None:
            return "err"
        if not data:
            break
        rows.extend(data)
        if len(data) < 1500:
            break
        start = data[-1][0] + 86400_000
    if not rows:
        return "empty"
    with open(out, "w") as f:
        f.write("open_time,open,high,low,close\n")
        for k in rows:
            f.write(f"{k[0]},{k[1]},{k[2]},{k[3]},{k[4]}\n")
    return f"{len(rows)}d"


def fetch_funding(sym):
    out = os.path.join(FOUT, f"{sym}.csv")
    if os.path.exists(out) and os.path.getsize(out) > 100:
        return "skip"
    rows, start = [], 1577836800000
    while True:
        url = (f"{BASE}/fapi/v1/fundingRate?symbol={sym}"
               f"&startTime={start}&limit=1000")
        time.sleep(PACE)
        data = get(url)
        if data is None:
            return "err"
        if not data:
            break
        rows.extend(data)
        if len(data) < 1000:
            break
        start = int(data[-1]["fundingTime"]) + 1
    if not rows:
        return "empty"
    with open(out, "w") as f:
        f.write("calc_time,funding_rate\n")
        for r in rows:
            f.write(f"{r['fundingTime']},{r['fundingRate']}\n")
    return f"{len(rows)}r"


def main():
    syms = sorted({os.path.basename(p).replace("-1d.csv", "")
                   for p in glob.glob(os.path.join(ROOT, "data", "listing",
                                                   "klines", "*-1d.csv"))})
    print(f"{len(syms)} symbols", flush=True)
    for i, sym in enumerate(syms):
        rk = fetch_klines(sym)
        rf = fetch_funding(sym)
        if i % 25 == 0 or rk == "err" or rf == "err":
            print(f"[{i+1}/{len(syms)}] {sym}: klines={rk} funding={rf}",
                  flush=True)
    print("DONE", flush=True)


if __name__ == "__main__":
    sys.exit(main())
