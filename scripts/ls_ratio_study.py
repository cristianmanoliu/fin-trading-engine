#!/usr/bin/env python3
"""ls_ratio_study.py — candidate #2: long/short-ratio extreme fade.

Pure CSV study (no engine). Reads data/metrics/<SYM>.csv (from download_metrics.sh)
for the crowd positioning ratios + local 1m klines for forward price. Per
`results/strategy_candidates_2026-06-10.md` (#2) + `tasks/todo.md` Phase 2.

THE BET
-------
Extreme crowd positioning mean-reverts. When the GLOBAL account long/short ratio is
very high (retail max-long), price tends to fall (fade the crowd) → SHORT; very low →
LONG. Variant: top-trader vs global DIVERGENCE (smart money opposite the crowd).

Metrics columns used:
  count_long_short_ratio        = global accounts long/short ratio
  sum_toptrader_long_short_ratio= top-trader positions long/short ratio
Signal at metric timestamp t (5min grid). Forward return measured t -> t+H on the
nearest 1m close (no look-ahead — entry at the bar AT/after t).

================================  PRE-REGISTRATION  ============================
  Signal A (crowd fade):  per-symbol percentile of global L/S ratio; top 10% -> SHORT,
                          bottom 10% -> LONG.
  Signal B (divergence):  z-score of (top_trader_ls - global_ls); top 10% (smart long /
                          crowd short) -> LONG, bottom 10% -> SHORT.
  Lookback (z/pctile):    7d rolling (2016 5-min bars).
  Forward horizon H:      1h, 4h, 24h.
  Cost:                   15 bp round-trip. Screen pass = 2x = 30 bp gross move.
  Honesty:                median + win + drop-top-5% + by-year (the #11 lesson).
  ACCEPT:                 screen pass AND median net >0 AND survives top-5% AND
                          positive majority of years AND (gate 3 later) corr-to-LIVE<0.5.
===============================================================================
"""
import os, glob, csv, math, sys, argparse, datetime
from collections import defaultdict
import numpy as np

METRICS_DIR = "data/metrics"
DATA = "data"
MIN_MS = 60_000
LOOKBACK = 2016          # 7d of 5-min bars
EXTREME_PCT = 0.10
HORIZONS_H = [1, 4, 24]  # hours
COST_BP = 15.0
PASS_BP = 30.0


def load_metrics(sym):
    """sorted list of (ts_ms, global_ls, top_ls)."""
    path = os.path.join(METRICS_DIR, f"{sym}.csv")
    out = []
    if not os.path.exists(path):
        return out
    with open(path) as f:
        r = csv.DictReader(f)
        for row in r:
            try:
                t = datetime.datetime.strptime(row["create_time"], "%Y-%m-%d %H:%M:%S")
                ts = int(t.replace(tzinfo=datetime.timezone.utc).timestamp() * 1000)
                g = float(row["count_long_short_ratio"])
                tp = float(row["sum_toptrader_long_short_ratio"])
            except (ValueError, KeyError):
                continue
            out.append((ts, g, tp))
    out.sort()
    return out


def load_closes(sym):
    """minute_ms -> close, all local 1m CSVs."""
    cl = {}
    for fp in sorted(glob.glob(os.path.join(DATA, f"{sym}-1m-*.csv"))):
        with open(fp) as f:
            r = csv.reader(f); next(r, None)
            for row in r:
                try:
                    cl[int(row[0])] = float(row[4])
                except (ValueError, IndexError):
                    continue
    return cl


def fwd_bp(closes, ts, h_hours):
    a = closes.get((ts // MIN_MS) * MIN_MS)
    b = closes.get(((ts + h_hours * 3600_000) // MIN_MS) * MIN_MS)
    if a is None or b is None or a <= 0 or b <= 0:
        return None
    return math.log(b / a) * 1e4


def analyze(sym, bucketsA, bucketsB):
    m = load_metrics(sym)
    if len(m) < LOOKBACK + 300:
        return
    closes = load_closes(sym)
    if not closes:
        return
    ts = np.array([x[0] for x in m])
    g = np.array([x[1] for x in m])
    tp = np.array([x[2] for x in m])
    div = tp - g
    n = len(m)
    for i in range(LOOKBACK, n):
        gw = g[i - LOOKBACK:i]
        dw = div[i - LOOKBACK:i]
        if len(gw) < LOOKBACK // 2:
            continue
        g_hi = np.quantile(gw, 1 - EXTREME_PCT); g_lo = np.quantile(gw, EXTREME_PCT)
        dmu, dsd = dw.mean(), dw.std(ddof=1)
        for H in HORIZONS_H:
            f = fwd_bp(closes, ts[i], H)
            if f is None:
                continue
            yr = datetime.datetime.fromtimestamp(ts[i] / 1000, datetime.timezone.utc).year
            # Signal A — crowd fade: high global L/S -> SHORT (trade=-f), low -> LONG (+f)
            if g[i] >= g_hi:
                bucketsA[H].append((-f - COST_BP, yr, abs(f)))
            elif g[i] <= g_lo:
                bucketsA[H].append((f - COST_BP, yr, abs(f)))
            # Signal B — divergence: smart-money-long vs crowd (high div) -> LONG
            if dsd > 0:
                z = (div[i] - dmu) / dsd
                dz_hi = 1.28  # ~top 10% of a normal
                if z >= dz_hi:
                    bucketsB[H].append((f - COST_BP, yr, abs(f)))
                elif z <= -dz_hi:
                    bucketsB[H].append((-f - COST_BP, yr, abs(f)))


def report(name, buckets):
    print(f"\n--- Signal {name} ---")
    print(f"{'H(h)':>5} {'n':>8} {'gross_bp':>9} {'mean_net':>9} {'med_net':>9} "
          f"{'win':>5} {'t':>6} {'drop5':>8}  screen")
    best = None
    for H in sorted(buckets):
        rows = buckets[H]
        if len(rows) < 60:
            continue
        net = np.array([r[0] for r in rows])
        gross = np.mean([r[2] for r in rows])
        yrs = np.array([r[1] for r in rows])
        srt = np.sort(net)
        k5 = max(1, int(len(srt) * 0.05))
        med = float(np.median(srt)); wr = float((srt > 0).mean())
        t = srt.mean() / (srt.std(ddof=1) / math.sqrt(len(srt)))
        drop5 = srt[:-k5].mean()
        screen = "PASS" if gross >= PASS_BP else ("cost" if gross >= COST_BP else "DEAD")
        print(f"{H:>5} {len(net):>8} {gross:>9.2f} {srt.mean():>9.2f} {med:>9.2f} "
              f"{wr:>5.0%} {t:>6.2f} {drop5:>8.2f}  {screen}")
        byyear = defaultdict(list)
        for v, y in zip(net, yrs):
            byyear[int(y)].append(v)
        score = srt.mean() if (gross >= PASS_BP and med > 0 and drop5 > 0) else -1e9
        if best is None or score > best[0]:
            best = (score, H, dict(n=len(net), mean=srt.mean(), med=med, wr=wr,
                                   gross=gross, drop5=drop5, byyear=byyear))
    return best


def main():
    ap = argparse.ArgumentParser(); ap.add_argument("--limit", type=int, default=0)
    args = ap.parse_args()
    syms = sorted(os.path.basename(p)[:-4] for p in glob.glob(os.path.join(METRICS_DIR, "*.csv")))
    if args.limit:
        syms = syms[: args.limit]
    if not syms:
        print("No metrics — run scripts/download_metrics.sh first."); sys.exit(1)

    print("=" * 80)
    print(f"L/S-RATIO EXTREME FADE (#2) — {len(syms)} symbols")
    print(f"crowd-fade + top/global divergence; cost {COST_BP}bp; screen {PASS_BP}bp")
    print("=" * 80)
    bA = {H: [] for H in HORIZONS_H}; bB = {H: [] for H in HORIZONS_H}
    for i, s in enumerate(syms, 1):
        analyze(s, bA, bB)
        print(f"  [{i}/{len(syms)}] {s}", file=sys.stderr)

    bestA = report("A (crowd fade)", bA)
    bestB = report("B (top/global divergence)", bB)

    print("\n" + "=" * 80)
    print("VERDICT (candidate #2)")
    for nm, best in (("A crowd-fade", bestA), ("B divergence", bestB)):
        if not best or best[0] < -1e8:
            g = best[2]["gross"] if best else 0
            print(f"  {nm}: NO-GO (best gross {g:.1f}bp < {PASS_BP}bp screen or tail-fails).")
            continue
        _, H, st = best
        ry = py = 0
        for y in sorted(st["byyear"]):
            a = np.array(st["byyear"][y])
            if len(a) < 30: continue
            ry += 1; py += 1 if a.mean() > 0 else 0
        verdict = ("CANDIDATE -> corr-to-LIVE gate" if py >= (ry + 1)//2 and st["wr"] > 0.5
                   else "MARGINAL/regime-fragile")
        print(f"  {nm}: H={H}h gross={st['gross']:.1f}bp mean_net={st['mean']:.2f} "
              f"med={st['med']:.2f} win={st['wr']:.0%} drop5={st['drop5']:.2f} "
              f"yrs={py}/{ry} -> {verdict}")
    print("=" * 80)


if __name__ == "__main__":
    main()
