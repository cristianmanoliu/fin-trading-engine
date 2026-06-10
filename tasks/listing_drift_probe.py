#!/usr/bin/env python3
"""TEMP probe — survivor-only new-listing drift (#12 go/no-go for the full fetch).

Lower bound on the short edge: delisted symbols dumped harder, so survivors
understate it. For each local symbol: listing day = first 1m candle. Short at
close of day N, hold M days, exit at close. ret = (entry-exit)/entry - 25bp slip.
No funding (probe). If even this lower bound is flat/negative, the full
de-survivorship fetch is still worth it (delisted are worse); if strongly
positive, fetch is clearly justified.
"""
import glob, os, csv, math, sys
import numpy as np

MS_DAY = 86_400_000
DATA = "data"
SLIP = 25 / 1e4


def daily_ohlc(sym):
    days = {}
    for fp in sorted(glob.glob(f"{DATA}/{sym}-1m-*.csv")):
        with open(fp) as f:
            r = csv.reader(f)
            next(r, None)
            for row in r:
                try:
                    ot = int(row[0]); o = float(row[1]); c = float(row[4])
                except (ValueError, IndexError):
                    continue
                d = ot // MS_DAY
                if d not in days:
                    days[d] = [o, c]
                days[d][1] = c
    return days


def main():
    syms = sorted(set(os.path.basename(p).split("-1m-")[0]
                      for p in glob.glob(f"{DATA}/*-1m-*.csv")))
    print(f"{len(syms)} survivor symbols", flush=True)
    cache = {s: daily_ohlc(s) for s in syms}
    for N in (1, 2, 3):
        for M in (3, 5, 7):
            rets = []
            for s in syms:
                d = cache[s]
                if not d:
                    continue
                days = sorted(d.keys())
                if len(days) < N + M + 1:
                    continue
                ec = d[days[N]][1]
                xc = d[days[N + M]][1]
                if ec <= 0 or xc <= 0:
                    continue
                rets.append((ec - xc) / ec - SLIP)  # SHORT
            if rets:
                a = np.array(rets)
                t = a.mean() / (a.std(ddof=1) / math.sqrt(len(a))) if len(a) > 1 else float("nan")
                print(f"N={N} M={M}  n={len(a):2d}  mean={a.mean()*100:+6.2f}%  "
                      f"median={np.median(a)*100:+6.2f}%  win={(a>0).mean():.0%}  t={t:+.2f}",
                      flush=True)


if __name__ == "__main__":
    main()
