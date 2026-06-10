#!/usr/bin/env python3
"""basis_study.py — candidate #5: spot-perp basis dislocation.

Perp price − spot price = basis. Large positive basis = perp over spot = leveraged
longs crowded → mean-reversion down; large negative = shorts crowded → reversion up.
Per `results/strategy_candidates_2026-06-10.md` (#5). Uses Binance SPOT (same venue,
data/spot/, fetched from data.binance.vision) as the spot leg vs local Binance perp.

IMPORTANT: must be SAME-VENUE spot. Using Coinbase spot here just reproduces #16 (the
Coinbase-vs-Binance cross-venue premium dominates the difference, not the perp-spot
basis) — verified: Coinbase-spot version gave t=2.4/2.3 identical to #16, Binance-spot
version (the real basis) gives t=1.2/0.9 (weak). BTC+ETH.

================================  PRE-REGISTRATION  ============================
  Basis:    (binance_perp_close - binance_spot_close)/spot, daily.
  Signal:   90d rolling z-score of basis.
  Variant A (standalone fade): z>+1 (perp rich) -> SHORT, z<-1 (perp cheap) -> LONG.
  Variant B (gate live short): short-return conditional on basis-z sign — shorts win
            when perp is rich (z>0, longs crowded)?
  Cost:     15 bp round-trip.
  Honesty:  median + win + drop-top-5% + by-year.
  ACCEPT:   A Sharpe>1 + median>0 + tail + years; OR B clean significant year-robust.
===============================================================================
"""
import os, glob, csv, math, datetime
from collections import defaultdict
import numpy as np

SPOT_DIR = "data/spot"     # Binance SPOT (same venue) — isolates the true perp basis.
DATA = "data"              # NOTE: using Coinbase spot here would just reproduce #16
Z_WIN = 90                 # (the cross-venue premium), since the Coinbase-vs-Binance
COST_BP = 15.0             # price gap dominates. Same-venue spot is the real basis test.
SYMS = ["BTCUSDT", "ETHUSDT"]


def load_cb(sym):
    """Binance SPOT daily close (headerless archive klines: col0=open_ms, col4=close)."""
    out = {}
    p = os.path.join(SPOT_DIR, f"{sym}.csv")
    if not os.path.exists(p):
        return out
    with open(p) as f:
        r = csv.reader(f)
        for row in r:
            try:
                ot = int(row[0]); c = float(row[4])
            except (ValueError, IndexError):
                continue
            # some newer archive zips use microsecond timestamps (16 digits) — normalize
            if ot > 10**15:
                ot //= 1000
            if ot < 10**12 or ot > 10**13:   # skip anything not a plausible ms epoch
                continue
            d = datetime.datetime.fromtimestamp(ot / 1000, datetime.timezone.utc).strftime("%Y-%m-%d")
            out[d] = c
    return out


def perp_daily(sym):
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
    print("=" * 76)
    print("SPOT-PERP BASIS (#5) — Binance perp vs Binance SPOT (same-venue); BTC+ETH")
    print("=" * 76)
    any_cand = False
    for sym in SYMS:
        cb = load_cb(sym); perp = perp_daily(sym)
        days = sorted(set(cb) & set(perp))
        if len(days) < Z_WIN + 60:
            print(f"[{sym}] insufficient"); continue
        basis = {d: (perp[d] - cb[d]) / cb[d] for d in days if cb[d] > 0}
        bdays = sorted(basis)
        print(f"\n[{sym}] {bdays[0]}..{bdays[-1]} ({len(bdays)}d); "
              f"basis mean={np.mean(list(basis.values()))*1e4:+.1f}bp")
        # Variant A
        nets = []; byA = defaultdict(list)
        gate = defaultdict(lambda: {"hi": [], "lo": []}); ghi, glo = [], []
        for i in range(len(bdays) - 1):
            d, dn = bdays[i], bdays[i + 1]
            hist = [basis[bdays[j]] for j in range(max(0, i - Z_WIN), i)]
            if len(hist) < Z_WIN // 2 or perp[d] <= 0 or perp[dn] <= 0:
                continue
            mu, sd = np.mean(hist), np.std(hist, ddof=1)
            if sd <= 0:
                continue
            z = (basis[d] - mu) / sd
            fwd = math.log(perp[dn] / perp[d]) * 1e4
            if z >= 1:
                nets.append(-fwd - COST_BP); byA[int(d[:4])].append(-fwd - COST_BP)  # perp rich -> short
            elif z <= -1:
                nets.append(fwd - COST_BP); byA[int(d[:4])].append(fwd - COST_BP)    # perp cheap -> long
            short_ret = -fwd
            if z > 0:
                gate[int(d[:4])]["hi"].append(short_ret); ghi.append(short_ret)
            else:
                gate[int(d[:4])]["lo"].append(short_ret); glo.append(short_ret)
        if len(nets) >= 2:
            a = np.array(nets); srt = np.sort(a); k5 = max(1, int(len(srt)*0.05))
            sh = srt.mean()/srt.std(ddof=1)*math.sqrt(365)
            ry = py = 0
            for y in sorted(byA):
                arr = np.array(byA[y])
                if len(arr) < 20: continue
                ry += 1; py += 1 if arr.mean() > 0 else 0
            cand = sh > 1 and np.median(srt) > 0 and srt[:-k5].mean() > 0 and py >= (ry+1)//2
            any_cand = any_cand or cand
            print(f"  A fade: n={len(a)} mean={srt.mean():+.2f}bp med={np.median(srt):+.2f} "
                  f"win={(srt>0).mean():.0%} Sharpe={sh:.2f} drop5={srt[:-k5].mean():+.2f} "
                  f"yrs={py}/{ry} -> {'CAND' if cand else 'no-go'}")
        ghi, glo = np.array(ghi), np.array(glo)
        if len(ghi) > 20 and len(glo) > 20:
            se = math.sqrt(ghi.var(ddof=1)/len(ghi) + glo.var(ddof=1)/len(glo))
            tw = (ghi.mean() - glo.mean()) / se if se > 0 else 0
            yp = sum(1 for y in gate if len(gate[y]["hi"]) > 5 and len(gate[y]["lo"]) > 5
                     and np.mean(gate[y]["hi"]) > np.mean(gate[y]["lo"]))
            yt = sum(1 for y in gate if len(gate[y]["hi"]) > 5 and len(gate[y]["lo"]) > 5)
            print(f"  B gate: short-ret|basis-z>0 (perp rich)={ghi.mean():+.1f}bp vs z<0="
                  f"{glo.mean():+.1f}bp Welch t={tw:+.2f} split+ {yp}/{yt}yr "
                  f"-> {'SIGNIF' if tw>2 and yp>=(yt+1)//2 else 'weak'}")
    print("\n" + "=" * 76)
    print("VERDICT (candidate #5)")
    print(f"  {'CANDIDATE (see above)' if any_cand else 'NO-GO standalone (no variant-A clears the bar). Gate reported; usable only if tw>0 significant + year-robust.'}")
    print("=" * 76)


if __name__ == "__main__":
    main()
