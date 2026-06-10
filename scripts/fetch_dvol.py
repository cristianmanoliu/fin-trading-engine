#!/usr/bin/env python3
"""fetch_dvol.py — Deribit DVOL (implied vol index) history for candidate #15.

Pulls daily DVOL close for BTC + ETH from Deribit public API, writes
data/dvol/<CCY>.csv (date,dvol_close). History starts ~2022 (not 2021 as the spec
hoped — verified empty before 2022). Stdlib only. Per `tasks/todo.md` Phase 1 #15.
"""
import json, os, csv, time, datetime, urllib.request

OUT = "data/dvol"
API = "https://www.deribit.com/api/v2/public/get_volatility_index_data"


def fetch_ccy(ccy):
    # daily resolution = 43200 is 12h; use 43200 then take daily; actually pull
    # hourly-ish then resample to daily close. Use res=43200 (12h) and keep last/day.
    start = int(datetime.datetime(2021, 1, 1, tzinfo=datetime.timezone.utc).timestamp() * 1000)
    now = int(time.time() * 1000)
    rows = []
    # the API caps points per call; page in ~90-day windows
    win = 90 * 86400 * 1000
    s = start
    while s < now:
        e = min(s + win, now)
        url = f"{API}?currency={ccy}&start_timestamp={s}&end_timestamp={e}&resolution=43200"
        try:
            d = json.loads(urllib.request.urlopen(url, timeout=20).read())
            data = d.get("result", {}).get("data", [])
        except Exception as ex:
            print(f"  {ccy} window err: {ex}")
            data = []
        for row in data:
            ts = row[0]; close = row[4]
            rows.append((ts, close))
        s = e
        time.sleep(0.1)
    # daily close: keep last point of each UTC day
    daily = {}
    for ts, c in sorted(rows):
        day = datetime.datetime.fromtimestamp(ts / 1000, datetime.timezone.utc).strftime("%Y-%m-%d")
        daily[day] = c
    return daily


def main():
    os.makedirs(OUT, exist_ok=True)
    for ccy in ("BTC", "ETH"):
        daily = fetch_ccy(ccy)
        path = os.path.join(OUT, f"{ccy}.csv")
        with open(path, "w", newline="") as f:
            w = csv.writer(f)
            w.writerow(["date", "dvol_close"])
            for day in sorted(daily):
                w.writerow([day, daily[day]])
        days = sorted(daily)
        print(f"[{ccy}] {len(daily)} days {days[0]}..{days[-1]} -> {path}")
    print("DONE.")


if __name__ == "__main__":
    main()
