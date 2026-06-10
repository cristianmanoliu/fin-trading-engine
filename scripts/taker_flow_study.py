#!/usr/bin/env python3
"""taker_flow_study.py — candidate #4: taker-flow imbalance (v1, no fetch).

Klines field 9 (`taker_buy_volume`) is already in every local 1m CSV. Taker-buy /
total-volume = the aggressor imbalance (what fraction of volume hit the ask vs the
bid). Per `results/strategy_candidates_2026-06-10.md` (#4) + `tasks/todo.md` Phase 0.

THE BET
-------
Sustained taker-BUY pressure (aggressors lifting offers) leads price up; sustained
taker-SELL pressure leads down. A rolling imbalance z-score should have directional
predictive power on the next bar(s).

CAVEAT (why this might be a price-copy): taker volume is derived from the same kline
stream. It is NOT a price LEVEL, but it may still be highly correlated with realized
return (aggressive buying IS the up-move). So gate 3 (corr-to-LIVE) and the
"is-it-just-momentum" check matter especially here. We screen on a horizon AHEAD of
the signal bar (no look-ahead) and report fee-death + by-year + tail, same lens as #11.

Resampling: 1m -> chosen bar (default 1H) to get a tradeable horizon and cut noise.
Signal at bar close t uses only data <= t; forward return measured t -> t+H.

================================  PRE-REGISTRATION  ============================
  Bar:                1H (resample 1m)
  Imbalance:          taker_buy_vol / total_vol per bar, minus 0.5 (centered)
  Lookback (z):       24 bars (1 day) rolling mean/std of imbalance
  Signal extremes:    top/bottom 10% of z-score (per symbol)
  Forward horizon H:  1, 4, 24 bars
  Direction:          high taker-buy z -> LONG; low (taker-sell) z -> SHORT
  Cost floor:         15 bp round-trip (10 fee + 5 slip)
  Pass screen:        |mean forward move| on an extreme bucket >= 2x cost = 30 bp
  Honesty:            median + win-rate + drop-top-5% + by-year (the #11 lesson).
  ACCEPT:             screen pass AND median net >0 AND survives top-5% drop AND
                      positive majority of years.
===============================================================================
"""
import os, glob, csv, math, sys, argparse, datetime
from collections import defaultdict
import numpy as np

DATA = "data"
MIN_MS = 60_000
BAR_MIN = 60                 # 1H bars
LOOKBACK = 24                # bars for z-score
EXTREME_PCT = 0.10
HORIZONS = [1, 4, 24]        # bars
COST_BP = 15.0
PASS_BP = 30.0


def load_bars(sym):
    """Resample 1m -> BAR_MIN bars. Return sorted list of
    (bar_open_ms, close, taker_buy_vol, total_vol)."""
    agg = {}  # bar_ms -> [close, tbv, tot]
    for fp in sorted(glob.glob(os.path.join(DATA, f"{sym}-1m-*.csv"))):
        with open(fp) as f:
            r = csv.reader(f); next(r, None)
            for row in r:
                try:
                    ot = int(row[0]); c = float(row[4]); vol = float(row[5])
                    tbv = float(row[9])
                except (ValueError, IndexError):
                    continue
                bar = (ot // (BAR_MIN * MIN_MS)) * (BAR_MIN * MIN_MS)
                if bar not in agg:
                    agg[bar] = [c, 0.0, 0.0]
                agg[bar][0] = c          # last close in bar
                agg[bar][1] += tbv
                agg[bar][2] += vol
    out = [(k, v[0], v[1], v[2]) for k, v in sorted(agg.items())]
    return out


def analyze(sym, buckets):
    bars = load_bars(sym)
    if len(bars) < LOOKBACK + max(HORIZONS) + 5:
        return
    ms = np.array([b[0] for b in bars])
    close = np.array([b[1] for b in bars])
    tbv = np.array([b[2] for b in bars])
    tot = np.array([b[3] for b in bars])
    with np.errstate(divide="ignore", invalid="ignore"):
        imb = np.where(tot > 0, tbv / tot - 0.5, np.nan)

    # rolling z of imbalance using only past LOOKBACK bars (no look-ahead)
    n = len(bars)
    for i in range(LOOKBACK, n - max(HORIZONS)):
        window = imb[i - LOOKBACK:i]
        w = window[~np.isnan(window)]
        if len(w) < LOOKBACK // 2 or np.isnan(imb[i]):
            continue
        mu, sd = w.mean(), w.std(ddof=1)
        if sd <= 0:
            continue
        z = (imb[i] - mu) / sd
        # per-symbol extreme thresholds accumulate later; store z + fwd returns
        for H in HORIZONS:
            if close[i] <= 0 or close[i + H] <= 0:
                continue
            fwd = math.log(close[i + H] / close[i]) * 1e4   # bp, signed
            yr = datetime.datetime.fromtimestamp(ms[i] / 1000, datetime.timezone.utc).year
            buckets[H].append((z, fwd, yr))


def cell_report(H, rows):
    """rows: list of (z, fwd_bp, year). Bucket by z extremes, directional trade."""
    if not rows:
        return None
    z = np.array([r[0] for r in rows])
    fwd = np.array([r[1] for r in rows])
    yr = np.array([r[2] for r in rows])
    hi = np.quantile(z, 1 - EXTREME_PCT)
    lo = np.quantile(z, EXTREME_PCT)
    # high z -> LONG (trade return = +fwd - cost); low z -> SHORT (-fwd - cost)
    long_mask = z >= hi
    short_mask = z <= lo
    long_net = fwd[long_mask] - COST_BP
    short_net = -fwd[short_mask] - COST_BP
    net = np.concatenate([long_net, short_net])
    yrs = np.concatenate([yr[long_mask], yr[short_mask]])
    if len(net) < 2:
        return None
    srt = np.sort(net)
    k5 = max(1, int(len(srt) * 0.05))
    byyear = defaultdict(list)
    for v, y in zip(net, yrs):
        byyear[int(y)].append(v)
    # gross |move| for the fee-death screen (best leg)
    gross = max(abs(fwd[long_mask].mean()) if long_mask.any() else 0,
                abs(fwd[short_mask].mean()) if short_mask.any() else 0)
    return dict(n=len(net), mean=srt.mean(), med=float(np.median(srt)),
                wr=float((srt > 0).mean()),
                t=srt.mean() / (srt.std(ddof=1) / math.sqrt(len(srt))),
                drop5=srt[:-k5].mean(), gross=gross, byyear=byyear)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--limit", type=int, default=0)
    args = ap.parse_args()
    syms = sorted(set(os.path.basename(p).split("-1m-")[0]
                      for p in glob.glob(os.path.join(DATA, "*-1m-*.csv"))))
    if args.limit:
        syms = syms[: args.limit]

    print("=" * 80)
    print(f"TAKER-FLOW IMBALANCE (#4 v1) — {len(syms)} symbols, {BAR_MIN}m bars, "
          f"z-lookback {LOOKBACK}")
    print(f"extreme {EXTREME_PCT:.0%} z -> directional; cost {COST_BP}bp; screen pass {PASS_BP}bp")
    print("=" * 80)

    buckets = {H: [] for H in HORIZONS}
    for i, s in enumerate(syms, 1):
        analyze(s, buckets)
        print(f"  [{i}/{len(syms)}] {s}", file=sys.stderr)

    print(f"{'H(bars)':>8} {'n':>8} {'gross_bp':>9} {'mean_net':>9} {'med_net':>9} "
          f"{'win':>5} {'t':>6} {'drop5':>8}  screen")
    print("-" * 80)
    best = None
    reports = {}
    for H in HORIZONS:
        rep = cell_report(H, buckets[H])
        if rep is None:
            continue
        reports[H] = rep
        screen = "PASS" if rep["gross"] >= PASS_BP else ("cost" if rep["gross"] >= COST_BP else "DEAD")
        print(f"{H:>8} {rep['n']:>8} {rep['gross']:>9.2f} {rep['mean']:>9.2f} "
              f"{rep['med']:>9.2f} {rep['wr']:>5.0%} {rep['t']:>6.2f} {rep['drop5']:>8.2f}  {screen}")
        if best is None or rep["mean"] > best[1]["mean"]:
            best = (H, rep)

    if not best:
        print("\nNo readable horizon."); return
    H, rep = best
    print()
    print("=" * 80)
    print(f"BEST horizon H={H} bars: by-year + honesty")
    print(f"  pooled n={rep['n']} mean_net={rep['mean']:+.2f}bp med={rep['med']:+.2f}bp "
          f"win={rep['wr']:.0%} t={rep['t']:.2f} drop-top-5%={rep['drop5']:+.2f}bp")
    print()
    print(f"  {'year':<6}{'n':>8}{'mean_net':>12}{'win':>7}")
    print("  " + "-" * 33)
    ry = py = 0
    for y in sorted(rep["byyear"]):
        a = np.array(rep["byyear"][y])
        if len(a) < 30:
            print(f"  {y:<6}{len(a):>8}{'(low-n)':>12}"); continue
        ry += 1
        if a.mean() > 0:
            py += 1
        print(f"  {y:<6}{len(a):>8}{a.mean():>12.2f}{(a>0).mean():>7.0%}")
    print()
    print("=" * 80)
    print("VERDICT (candidate #4 taker-flow v1)")
    tail_ok = rep["med"] > 0 and rep["drop5"] > 0
    if rep["gross"] < PASS_BP:
        print(f"  FEE-DEAD: gross move {rep['gross']:.1f}bp < {PASS_BP}bp screen. No engine mode.")
    elif not tail_ok:
        print("  TAIL-MIRAGE / NO-GO: median<=0 or inverts dropping top 5%. No engine mode.")
    elif ry >= 2 and py >= (ry + 1) // 2 and rep["wr"] > 0.5:
        print(f"  CANDIDATE: passes screen + median>0 + win {rep['wr']:.0%} + {py}/{ry} years "
              f"-> walk-forward / overfit / CORR-TO-LIVE (taker-flow is price-adjacent — "
              f"corr check is decisive).")
    else:
        print(f"  MARGINAL/NO-GO: only {py}/{ry} years positive. Likely a momentum copy.")
    print("=" * 80)


if __name__ == "__main__":
    main()
