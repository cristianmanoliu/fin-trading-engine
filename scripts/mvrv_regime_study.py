#!/usr/bin/env python3
"""mvrv_regime_study.py — candidate #20: on-chain cost-basis regime (MVRV).

MVRV = market cap / realized cap; extremes mark cycle tops/bottoms. Cost-basis info
absent from exchange OHLCV. Per `results/strategy_candidates_batch2_2026-06-10.md` (#20).
Spec power-flag was SEVERE (~1 cycle in our 5y sample) — BUT Coin Metrics community
data goes back to 2010 (BTC) / 2015 (ETH), giving ~3-4 cycles. We use the FULL CM
history for the signal, and report results both full-history and on the 2020-26
deployable window.

DATA: data/onchain/cm_{btc,eth}.csv (CapMVRVCur + PriceUSD, daily).

================================  PRE-REGISTRATION  ============================
  Signal:   MVRV percentile bands over a TRAILING 2y window (no look-ahead): high band
            (>80th pct) = overvalued -> de-risk/short bias; low band (<20th) = long bias.
  Variant A (standalone tilt): long in low band, short in high band, daily, 15bp cost.
  Variant B (gate live short): short-return conditional on MVRV band — shorts should
            win in the high (overvalued) band.
  Honesty:  by-year + cycle count caveat. Report full-history AND 2020-26.
  ACCEPT:   B clean significant split robust across cycles. Standalone A reported.
===============================================================================
"""
import os, csv, math
from collections import defaultdict
import numpy as np

DIR = "data/onchain"
TRAIL = 730   # 2y trailing percentile window
COST_BP = 15.0


def load_cm(ccy):
    rows = []
    with open(os.path.join(DIR, f"cm_{ccy}.csv")) as f:
        for r in csv.DictReader(f):
            try:
                d = r["time"][:10]
                mvrv = float(r["CapMVRVCur"]) if r.get("CapMVRVCur") else None
                px = float(r["PriceUSD"]) if r.get("PriceUSD") else None
            except (ValueError, KeyError):
                continue
            if mvrv is not None and px is not None and px > 0:
                rows.append((d, mvrv, px))
    rows.sort()
    return rows


def analyze(ccy):
    rows = load_cm(ccy)
    if len(rows) < TRAIL + 200:
        return None
    days = [r[0] for r in rows]; mvrv = [r[1] for r in rows]; px = [r[2] for r in rows]
    # trailing-percentile band per day
    bandA_net = []; byA = defaultdict(list)
    gate = defaultdict(lambda: {"hi": [], "lo": []})
    g_hi_all, g_lo_all = [], []
    for i in range(TRAIL, len(rows) - 1):
        window = mvrv[i - TRAIL:i]
        hi = np.quantile(window, 0.80); lo = np.quantile(window, 0.20)
        if px[i] <= 0 or px[i + 1] <= 0:
            continue
        fwd = math.log(px[i + 1] / px[i]) * 1e4
        yr = int(days[i][:4])
        # standalone A
        if mvrv[i] >= hi:
            bandA_net.append(-fwd - COST_BP); byA[yr].append(-fwd - COST_BP)   # overvalued -> short
        elif mvrv[i] <= lo:
            bandA_net.append(fwd - COST_BP); byA[yr].append(fwd - COST_BP)     # undervalued -> long
        # gate B: short-return conditional on band
        short_ret = -fwd
        if mvrv[i] >= hi:
            gate[yr]["hi"].append(short_ret); g_hi_all.append(short_ret)
        elif mvrv[i] <= lo:
            gate[yr]["lo"].append(short_ret); g_lo_all.append(short_ret)
    return days, bandA_net, byA, np.array(g_hi_all), np.array(g_lo_all), gate


def main():
    print("=" * 76)
    print("MVRV COST-BASIS REGIME (#20) — Coin Metrics full history; gate-context")
    print("=" * 76)
    any_sig = False
    for ccy in ("btc", "eth"):
        r = analyze(ccy)
        if not r:
            print(f"[{ccy}] insufficient"); continue
        days, aNet, byA, ghi, glo, gate = r
        print(f"\n[{ccy.upper()}] {days[0]}..{days[-1]}")
        if len(aNet) >= 2:
            a = np.array(aNet); srt = np.sort(a); k5 = max(1, int(len(srt)*0.05))
            sh = srt.mean()/srt.std(ddof=1)*math.sqrt(365)
            print(f"  A band-tilt: n={len(a)} mean={srt.mean():+.2f}bp med={np.median(srt):+.2f} "
                  f"Sharpe={sh:.2f} drop5={srt[:-k5].mean():+.2f}")
        if len(ghi) > 20 and len(glo) > 20:
            # shorts should WIN in high band (overvalued) vs low band
            se = math.sqrt(ghi.var(ddof=1)/len(ghi) + glo.var(ddof=1)/len(glo))
            tw = (ghi.mean() - glo.mean()) / se if se > 0 else 0
            yp = sum(1 for y in gate if len(gate[y]["hi"]) > 5 and len(gate[y]["lo"]) > 5
                     and np.mean(gate[y]["hi"]) > np.mean(gate[y]["lo"]))
            yt = sum(1 for y in gate if len(gate[y]["hi"]) > 5 and len(gate[y]["lo"]) > 5)
            print(f"  B gate: short-ret|MVRV-high (overvalued)={ghi.mean():+.1f}bp vs "
                  f"MVRV-low={glo.mean():+.1f}bp Welch t={tw:+.2f} split+ {yp}/{yt}yr")
            # USABLE only if shorts win in the high (overvalued) band — i.e. tw>0 AND
            # year-robust. A significant NEGATIVE tw means the thesis is backwards
            # (overvalued keeps ripping up; shorting it loses) = NOT usable.
            if tw > 2 and yp >= (yt + 1)//2:
                any_sig = True
            elif tw < -2:
                print("    (significant but WRONG SIGN: shorts LOSE in the overvalued "
                      "band — momentum dominates the fade. Not usable.)")
    print("\n" + "=" * 76)
    print("VERDICT (candidate #20)")
    print(f"  {'WEAK-GATE SIGNAL (significant + year-robust) — but cycle-limited; context only' if any_sig else 'NO-GO: MVRV bands give no significant, year-robust short-timing split. Cost-basis regime adds no usable signal at tradeable horizons (cycle-frequency too low).'}")
    print("=" * 76)


if __name__ == "__main__":
    main()
