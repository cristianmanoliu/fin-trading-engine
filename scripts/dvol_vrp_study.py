#!/usr/bin/env python3
"""dvol_vrp_study.py — candidate #15: volatility-risk-premium (DVOL vs realized).

VRP = implied vol (Deribit DVOL) − realized vol (from price). High VRP = market
overpaying for protection = fear extreme → contrarian LONG bias. Negative VRP
(RV>IV) = complacency/shock → flat/short. Options-market info, absent from any price
series. Per `results/strategy_candidates_batch2_2026-06-10.md` (#15) + Phase 2.

Data: data/dvol/{BTC,ETH}.csv (date,dvol_close, ~2021-03..2026, daily) +
local 1m klines for the price → realized vol. BTC+ETH only (DVOL coverage).

================================  PRE-REGISTRATION  ============================
  Realized vol:   trailing 30d close-to-close, annualized (×sqrt(365)), in vol points
                  (DVOL is also annualized vol %). VRP = DVOL − RV_annualized_pct.
  VRP z-score:    90d rolling mean/std of VRP.
  Variant A (standalone): z>+1 -> LONG next day (hold 1d, daily rebal); z<-1 -> SHORT.
                  contrarian-long thesis: high VRP (fear) precedes mean-revert up.
  Variant B (regime gate on LIVE short): the live config is shorts-only. Gate = only
                  allow a (hypothetical) short when VRP-z < 0 (complacency) — i.e. does
                  VRP regime improve a short book's timing? Reported as forward-return
                  conditional means, NOT a full engine re-run.
  Cost:           15 bp round-trip per daily trade (variant A).
  Honesty:        median + win + drop-top-5% + by-year.
  ACCEPT:         variant A net Sharpe>1 + positive median + survives top-5% + majority
                  years; OR variant B shows a clean, significant regime split.
===============================================================================
"""
import os, glob, csv, math, sys, datetime
from collections import defaultdict
import numpy as np

DVOL_DIR = "data/dvol"
DATA = "data"
MS_DAY = 86_400_000
RV_WINDOW = 30
Z_WINDOW = 90
COST_BP = 15.0
MAP = {"BTC": "BTCUSDT", "ETH": "ETHUSDT"}


def load_dvol(ccy):
    out = {}
    with open(os.path.join(DVOL_DIR, f"{ccy}.csv")) as f:
        for row in csv.DictReader(f):
            try:
                out[row["date"]] = float(row["dvol_close"])
            except (ValueError, KeyError):
                continue
    return out


def daily_closes(sym):
    days = {}
    for fp in sorted(glob.glob(os.path.join(DATA, f"{sym}-1m-*.csv"))):
        with open(fp) as f:
            r = csv.reader(f); next(r, None)
            for row in r:
                try:
                    ot = int(row[0]); c = float(row[4])
                except (ValueError, IndexError):
                    continue
                day = datetime.datetime.fromtimestamp(ot / 1000, datetime.timezone.utc).strftime("%Y-%m-%d")
                days[day] = c   # last close of day
    return days


def realized_vol(closes_sorted, i, window):
    """annualized close-to-close vol over [i-window, i], in pct points."""
    if i - window < 0:
        return None
    seg = closes_sorted[i - window:i + 1]
    rets = []
    for a, b in zip(seg[:-1], seg[1:]):
        if a > 0 and b > 0:
            rets.append(math.log(b / a))
    if len(rets) < window // 2:
        return None
    sd = np.std(rets, ddof=1)
    return sd * math.sqrt(365) * 100.0   # pct, annualized


def build(ccy):
    dvol = load_dvol(ccy)
    closes = daily_closes(MAP[ccy])
    days = sorted(set(dvol) & set(closes))
    if len(days) < Z_WINDOW + RV_WINDOW + 30:
        return None
    cl = [closes[d] for d in days]
    vrp = []
    for i in range(len(days)):
        rv = realized_vol(cl, i, RV_WINDOW)
        vrp.append(None if rv is None else dvol[days[i]] - rv)
    return days, cl, vrp


def variant_a(days, cl, vrp):
    """contrarian daily: z>+1 long, z<-1 short, hold 1d. Return net bp list + byyear."""
    nets = []; byyear = defaultdict(list)
    for i in range(len(days) - 1):
        if vrp[i] is None:
            continue
        hist = [v for v in vrp[max(0, i - Z_WINDOW):i] if v is not None]
        if len(hist) < Z_WINDOW // 2:
            continue
        mu, sd = np.mean(hist), np.std(hist, ddof=1)
        if sd <= 0:
            continue
        z = (vrp[i] - mu) / sd
        if cl[i] <= 0 or cl[i + 1] <= 0:
            continue
        fwd = math.log(cl[i + 1] / cl[i]) * 1e4
        if z >= 1:
            net = fwd - COST_BP        # LONG
        elif z <= -1:
            net = -fwd - COST_BP       # SHORT
        else:
            continue
        nets.append(net)
        yr = int(days[i][:4]); byyear[yr].append(net)
    return np.array(nets), byyear


def variant_b(days, cl, vrp):
    """regime split: forward 1d SHORT return conditional on VRP-z sign.
    Tests whether a short book times better in complacency (z<0) vs fear (z>0)."""
    short_when_low = []; short_when_high = []
    for i in range(len(days) - 1):
        if vrp[i] is None or cl[i] <= 0 or cl[i + 1] <= 0:
            continue
        hist = [v for v in vrp[max(0, i - Z_WINDOW):i] if v is not None]
        if len(hist) < Z_WINDOW // 2:
            continue
        mu, sd = np.mean(hist), np.std(hist, ddof=1)
        if sd <= 0:
            continue
        z = (vrp[i] - mu) / sd
        short_ret = -math.log(cl[i + 1] / cl[i]) * 1e4   # short pnl bp
        (short_when_low if z < 0 else short_when_high).append(short_ret)
    return np.array(short_when_low), np.array(short_when_high)


def stats(net):
    if len(net) < 2:
        return None
    srt = np.sort(net); k5 = max(1, int(len(srt) * 0.05))
    return dict(n=len(srt), mean=srt.mean(), med=float(np.median(srt)),
                wr=float((srt > 0).mean()),
                sharpe=srt.mean() / srt.std(ddof=1) * math.sqrt(365),
                t=srt.mean() / (srt.std(ddof=1) / math.sqrt(len(srt))),
                drop5=srt[:-k5].mean())


def main():
    print("=" * 78)
    print("DVOL VRP (#15) — implied(DVOL) minus realized vol; BTC+ETH")
    print("=" * 78)
    any_candidate = False
    for ccy in ("BTC", "ETH"):
        b = build(ccy)
        if not b:
            print(f"[{ccy}] insufficient overlap"); continue
        days, cl, vrp = b
        print(f"\n[{ccy}] {days[0]}..{days[-1]} ({len(days)}d)")
        # Variant A
        net, byyear = variant_a(days, cl, vrp)
        st = stats(net)
        if st:
            print(f"  A standalone: n={st['n']} mean={st['mean']:+.2f}bp med={st['med']:+.2f} "
                  f"win={st['wr']:.0%} Sharpe={st['sharpe']:.2f} t={st['t']:.2f} "
                  f"drop5={st['drop5']:+.2f}")
            ry = py = 0
            for y in sorted(byyear):
                a = np.array(byyear[y])
                if len(a) < 30: continue
                ry += 1; py += 1 if a.mean() > 0 else 0
            cand = st["sharpe"] > 1 and st["med"] > 0 and st["drop5"] > 0 and py >= (ry+1)//2
            print(f"    by-year positive {py}/{ry}  -> {'CANDIDATE' if cand else 'no-go'}")
            any_candidate = any_candidate or cand
        # Variant B
        lo, hi = variant_b(days, cl, vrp)
        if len(lo) > 30 and len(hi) > 30:
            from math import sqrt
            # Welch t between the two short-return regimes
            mlo, mhi = lo.mean(), hi.mean()
            se = sqrt(lo.var(ddof=1)/len(lo) + hi.var(ddof=1)/len(hi))
            tw = (mlo - mhi) / se if se > 0 else 0
            print(f"  B regime gate: short-ret | VRP-z<0 (complacency) mean={mlo:+.2f}bp (n={len(lo)}) "
                  f"vs z>0 (fear) mean={mhi:+.2f}bp (n={len(hi)})  Welch t={tw:+.2f}")
            print(f"    -> {'SIGNIFICANT split' if abs(tw)>2 else 'no clean split'}")

    print("\n" + "=" * 78)
    print("VERDICT (candidate #15)")
    print(f"  {'CANDIDATE found (see above) -> corr-to-LIVE + overfit gates' if any_candidate else 'NO-GO standalone (no BTC/ETH variant-A clears Sharpe>1 + median + tail + years)'}")
    print("  (Variant B regime split reported above — significant only if |t|>2 AND economically useful.)")
    print("=" * 78)


if __name__ == "__main__":
    main()
