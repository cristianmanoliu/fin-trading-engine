#!/usr/bin/env python3
"""coinbase_premium_study.py — candidate #16: Coinbase-Binance premium (US demand flow).

Coinbase spot premium over Binance = US institutional/retail demand pressure on the
regulated venue. Positive premium documented to lead returns at 1h-1d. Per
`results/strategy_candidates_batch2_2026-06-10.md` (#16). Cross-venue flow info, not in
the Binance series.

DATA: data/flow/coinbase/<SYM>.csv (cb daily close) + local Binance 1m klines (daily
close). Premium = (cb - binance)/binance. BTC+ETH (Coinbase majors).

================================  PRE-REGISTRATION  ============================
  Premium:    (coinbase_close - binance_close)/binance_close, daily.
  Signal:     90d rolling z-score of premium.
  Variant A (standalone): z>+1 -> LONG next day, z<-1 -> SHORT. (premium leads)
  Variant B (gate live short): short-return conditional on premium-z sign — does the
            live short time better when US is NOT bidding (premium<0)?
  Cost:       15 bp round-trip (variant A).
  Honesty:    median + win + drop-top-5% + by-year.
  ACCEPT:     A Sharpe>1 + median>0 + tail + years; OR B clean significant split (|t|>2)
              robust across years. Flow-family corr note: check vs #17/#18 if survives.
===============================================================================
"""
import os, glob, csv, math, datetime
from collections import defaultdict
import numpy as np

CB_DIR = "data/flow/coinbase"
DATA = "data"
Z_WIN = 90
COST_BP = 15.0
SYMS = ["BTCUSDT", "ETHUSDT"]


def load_cb(sym):
    out = {}
    p = os.path.join(CB_DIR, f"{sym}.csv")
    if not os.path.exists(p):
        return out
    with open(p) as f:
        for row in csv.DictReader(f):
            try:
                out[row["date"]] = float(row["cb_close"])
            except (ValueError, KeyError):
                continue
    return out


def load_binance_daily(sym):
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


def build(sym):
    cb = load_cb(sym); bi = load_binance_daily(sym)
    days = sorted(set(cb) & set(bi))
    if len(days) < Z_WIN + 60:
        return None
    prem = {d: (cb[d] - bi[d]) / bi[d] for d in days if bi[d] > 0}
    return days, bi, prem


def variant_a(days, bi, prem):
    nets = []; byyear = defaultdict(list)
    pdays = [d for d in days if d in prem]
    for i in range(len(pdays) - 1):
        d, dn = pdays[i], pdays[i + 1]
        hist = [prem[pdays[j]] for j in range(max(0, i - Z_WIN), i)]
        if len(hist) < Z_WIN // 2:
            continue
        mu, sd = np.mean(hist), np.std(hist, ddof=1)
        if sd <= 0 or bi[d] <= 0 or bi[dn] <= 0:
            continue
        z = (prem[d] - mu) / sd
        fwd = math.log(bi[dn] / bi[d]) * 1e4
        if z >= 1:
            net = fwd - COST_BP
        elif z <= -1:
            net = -fwd - COST_BP
        else:
            continue
        nets.append(net); byyear[int(d[:4])].append(net)
    return np.array(nets), byyear


def variant_b(days, bi, prem):
    """short next-day return conditional on premium-z sign, by year."""
    pdays = [d for d in days if d in prem]
    byyear = defaultdict(lambda: {"lo": [], "hi": []})
    lo_all, hi_all = [], []
    for i in range(len(pdays) - 1):
        d, dn = pdays[i], pdays[i + 1]
        hist = [prem[pdays[j]] for j in range(max(0, i - Z_WIN), i)]
        if len(hist) < Z_WIN // 2 or bi[d] <= 0 or bi[dn] <= 0:
            continue
        mu, sd = np.mean(hist), np.std(hist, ddof=1)
        if sd <= 0:
            continue
        z = (prem[d] - mu) / sd
        short_ret = -math.log(bi[dn] / bi[d]) * 1e4
        bucket = "lo" if z < 0 else "hi"   # lo = US not bidding -> short should win
        byyear[int(d[:4])][bucket].append(short_ret)
        (lo_all if z < 0 else hi_all).append(short_ret)
    return np.array(lo_all), np.array(hi_all), byyear


def main():
    print("=" * 76)
    print("COINBASE-BINANCE PREMIUM (#16) — US demand flow; BTC+ETH")
    print("=" * 76)
    any_cand = False
    for sym in SYMS:
        b = build(sym)
        if not b:
            print(f"[{sym}] insufficient overlap"); continue
        days, bi, prem = b
        print(f"\n[{sym}] {days[0]}..{days[-1]} ({len(prem)} premium days)")
        net, byyear = variant_a(days, bi, prem)
        if len(net) >= 2:
            srt = np.sort(net); k5 = max(1, int(len(srt) * 0.05))
            sh = srt.mean() / srt.std(ddof=1) * math.sqrt(365)
            ry = py = 0
            for y in sorted(byyear):
                a = np.array(byyear[y])
                if len(a) < 20: continue
                ry += 1; py += 1 if a.mean() > 0 else 0
            cand = sh > 1 and np.median(srt) > 0 and srt[:-k5].mean() > 0 and py >= (ry+1)//2
            any_cand = any_cand or cand
            print(f"  A standalone: n={len(srt)} mean={srt.mean():+.2f}bp med={np.median(srt):+.2f} "
                  f"win={(srt>0).mean():.0%} Sharpe={sh:.2f} drop5={srt[:-k5].mean():+.2f} "
                  f"yrs={py}/{ry} -> {'CAND' if cand else 'no-go'}")
        lo, hi, byB = variant_b(days, bi, prem)
        if len(lo) > 20 and len(hi) > 20:
            se = math.sqrt(lo.var(ddof=1)/len(lo) + hi.var(ddof=1)/len(hi))
            tw = (lo.mean() - hi.mean()) / se if se > 0 else 0
            yrs_pos = sum(1 for y in byB if len(byB[y]["lo"]) > 5 and len(byB[y]["hi"]) > 5
                          and np.mean(byB[y]["lo"]) > np.mean(byB[y]["hi"]))
            yrs_tot = sum(1 for y in byB if len(byB[y]["lo"]) > 5 and len(byB[y]["hi"]) > 5)
            print(f"  B gate: short-ret|prem-z<0 (US not bidding)={lo.mean():+.1f}bp vs z>0="
                  f"{hi.mean():+.1f}bp Welch t={tw:+.2f} split-positive {yrs_pos}/{yrs_tot}yr "
                  f"-> {'SIGNIF' if abs(tw)>2 and yrs_pos>=(yrs_tot+1)//2 else 'weak'}")
    print("\n" + "=" * 76)
    print("VERDICT (candidate #16)")
    print(f"  {'CANDIDATE (see above) -> corr-to-LIVE + flow-family corr' if any_cand else 'NO-GO standalone (no BTC/ETH variant-A clears the bar). Gate variant reported above; significant only if |t|>2 AND year-robust.'}")
    print("=" * 76)


if __name__ == "__main__":
    main()
