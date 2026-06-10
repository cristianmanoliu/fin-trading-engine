#!/usr/bin/env python3
"""stablecoin_supply_study.py — candidate #17: stablecoin supply impulse (liquidity tide).

USDT+USDC aggregate supply growth = new dry powder entering crypto; contraction =
liquidity drain. Supply growth leads returns weekly-monthly. Per
`results/strategy_candidates_batch2_2026-06-10.md` (#17). POWER-FLAGGED: ~3 regime
flips in 5y ≈ 3 independent bets — gate-context only, never standalone alpha.

DATA: data/flow/stablecoin.csv (total_usd daily, DefiLlama) + local BTC daily close.

================================  PRE-REGISTRATION  ============================
  Signal:  30d aggregate-supply growth, z-scored over 180d.
  Variant A (standalone, expected-to-fail per power flag): z>+X long bias, z<-X short.
  Variant B (regime gate on live short): short-return conditional on supply-z sign —
            shorts should do better when liquidity is contracting (z<0).
  Honesty: by-year + the explicit power caveat (≈3 independent regimes).
  ACCEPT:  variant B clean significant split robust across the few regimes. Standalone
           A is pre-registered as underpowered — reported for completeness only.
===============================================================================
"""
import os, glob, csv, math, datetime
from collections import defaultdict
import numpy as np

SC = "data/flow/stablecoin.csv"
DATA = "data"
GROWTH_WIN = 30
Z_WIN = 180


def load_sc():
    out = {}
    with open(SC) as f:
        for row in csv.DictReader(f):
            try:
                out[row["date"]] = float(row["total_usd"])
            except (ValueError, KeyError):
                continue
    return out


def binance_daily(sym):
    out = {}
    for fp in sorted(glob.glob(os.path.join(DATA, f"{sym}-1m-*.csv"))):
        with open(fp) as f:
            r = csv.reader(f); next(r, None)
            for row in r:
                try:
                    ot = int(row[0]); c = float(row[4])
                except (ValueError, IndexError):
                    continue
                d = datetime.datetime.fromtimestamp(ot / 1000, datetime.timezone.utc).strftime("%Y-%m-%d")
                out[d] = c
    return out


def main():
    sc = load_sc()
    bi = binance_daily("BTCUSDT")
    days = sorted(set(sc) & set(bi))
    if len(days) < Z_WIN + GROWTH_WIN + 60:
        print("insufficient overlap"); return
    # supply growth (30d) then z (180d)
    scv = {d: sc[d] for d in days}
    growth = {}
    for i in range(GROWTH_WIN, len(days)):
        a = scv[days[i - GROWTH_WIN]]; b = scv[days[i]]
        if a > 0:
            growth[days[i]] = (b - a) / a
    gdays = sorted(growth)
    z = {}
    for i in range(Z_WIN, len(gdays)):
        h = [growth[gdays[j]] for j in range(i - Z_WIN, i)]
        mu, sd = np.mean(h), np.std(h, ddof=1)
        if sd > 0:
            z[gdays[i]] = (growth[gdays[i]] - mu) / sd

    print("=" * 74)
    print(f"STABLECOIN SUPPLY IMPULSE (#17) — liquidity tide; vs BTC")
    print(f"POWER-FLAGGED: ~3 independent regimes in 5y. Gate-context only.")
    print("=" * 74)
    print(f"  supply {sc[days[0]]/1e9:.1f}B ({days[0]}) -> {sc[days[-1]]/1e9:.1f}B ({days[-1]})")

    # Variant A standalone (expected weak)
    zdays = sorted(z)
    nets = []; byyearA = defaultdict(list)
    for i in range(len(zdays) - 1):
        d, dn = zdays[i], zdays[i + 1]
        if bi[d] <= 0 or bi[dn] <= 0:
            continue
        fwd = math.log(bi[dn] / bi[d]) * 1e4
        if z[d] >= 1:
            net = fwd
        elif z[d] <= -1:
            net = -fwd
        else:
            continue
        nets.append(net - 15); byyearA[int(d[:4])].append(net - 15)
    if nets:
        a = np.array(nets); srt = np.sort(a); k5 = max(1, int(len(srt)*0.05))
        sh = srt.mean()/srt.std(ddof=1)*math.sqrt(365)
        print(f"\n  A standalone: n={len(a)} mean={srt.mean():+.2f}bp med={np.median(srt):+.2f} "
              f"Sharpe={sh:.2f} drop5={srt[:-k5].mean():+.2f} (underpowered by design)")

    # Variant B gate
    byB = defaultdict(lambda: {"lo": [], "hi": []})
    lo_all, hi_all = [], []
    for i in range(len(zdays) - 1):
        d, dn = zdays[i], zdays[i + 1]
        if bi[d] <= 0 or bi[dn] <= 0:
            continue
        short_ret = -math.log(bi[dn] / bi[d]) * 1e4
        bucket = "lo" if z[d] < 0 else "hi"   # lo = liquidity contracting -> short wins?
        byB[int(d[:4])][bucket].append(short_ret)
        (lo_all if z[d] < 0 else hi_all).append(short_ret)
    lo = np.array(lo_all); hi = np.array(hi_all)
    if len(lo) > 20 and len(hi) > 20:
        se = math.sqrt(lo.var(ddof=1)/len(lo) + hi.var(ddof=1)/len(hi))
        tw = (lo.mean() - hi.mean()) / se if se > 0 else 0
        yp = sum(1 for y in byB if len(byB[y]["lo"]) > 5 and len(byB[y]["hi"]) > 5
                 and np.mean(byB[y]["lo"]) > np.mean(byB[y]["hi"]))
        yt = sum(1 for y in byB if len(byB[y]["lo"]) > 5 and len(byB[y]["hi"]) > 5)
        print(f"\n  B gate: short-ret|supply-z<0 (contracting)={lo.mean():+.1f}bp vs z>0 "
              f"(expanding)={hi.mean():+.1f}bp Welch t={tw:+.2f} split-positive {yp}/{yt}yr")
        sig = abs(tw) > 2 and yp >= (yt + 1)//2
        print(f"\n{'='*74}")
        print("VERDICT (candidate #17)")
        if sig:
            print(f"  WEAK-GATE SIGNAL: t={tw:.2f}, {yp}/{yt}yr — but POWER-FLAGGED "
                  f"(~3 regimes); treat as supporting context only, never standalone.")
        else:
            print(f"  NO-GO: gate t={tw:.2f}, {yp}/{yt}yr — no clean liquidity-regime split. "
                  f"Stablecoin supply impulse adds no usable short-timing signal "
                  f"(underpowered as the power flag predicted).")
        print("=" * 74)


if __name__ == "__main__":
    main()
