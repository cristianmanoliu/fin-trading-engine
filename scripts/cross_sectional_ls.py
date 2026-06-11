#!/usr/bin/env python3
"""cross_sectional_ls.py — dollar-neutral cross-sectional momentum backtest.

⚠️ RAW SCREEN — KNOWN-OPTIMISTIC. The headline numbers (Sharpe 1.49, cum +10,825%)
are INFLATED by three confounds: (1) ragged history — only 3 symbols exist on the
earliest day, so early top/bottom-20% is a degenerate 1-vs-1 bet; (2) fantasy
compounding (cum% assumes unconstrained reinvestment, no borrow/slippage on the alt
short leg); (3) 10bp/side is far too low for a daily-rebalanced alt book. HARDENED
(min ≥20 symbols, 35bp/side, per-period Sharpe): Sharpe drops to 0.67 and ALL the edge
is 2020-2021 — the factor is DEAD since 2022. Verdict + by-year table:
results/cross_sectional_verdict_2026-06-10.md. NOT a deployable edge.


THE one structurally-different bet from the engine's "short the downtrend"
directional class. Each rebalance: rank all symbols by trailing N-day return,
go LONG the top quantile and SHORT the bottom quantile, equal dollars per side
(dollar-neutral). This earns the SPREAD between winners and losers, not the
market direction — so it can (in principle) profit in bull years where the
always-short class bleeds, and have LOW correlation with the live edge.

Offline: reads the raw 1m CSVs in data/, resamples to daily closes, no engine
mode needed. Cost model: per-rebalance turnover × fee_bps both sides.

This is a SCREEN, not a deployment candidate. If its return stream is
low-correlation with a "short-BTC" proxy AND positive net of costs, it is the
first thing all session that could justify broad search. If not, the directional
class is confirmed as the only thing in this data and broad search is closed.
"""
import os, glob, math, sys
from collections import defaultdict

DATA = os.path.join(os.path.dirname(__file__), "..", "data")
FEE_BPS = 10.0          # round-trip taker, per side traded
LOOKBACKS = [7, 14, 30] # trailing-return ranking windows (days)
HOLD_DAYS = [1, 3, 7]   # rebalance frequency
QUANTILE = 0.2          # top/bottom 20%

def daily_closes():
    """symbol -> {date(int yyyymmdd): close}. Last 1m close of each UTC day."""
    out = defaultdict(dict)
    files = glob.glob(os.path.join(DATA, "*-1m-*.csv"))
    for fp in files:
        base = os.path.basename(fp)
        sym = base.split("-1m-")[0]
        try:
            for line in open(fp):
                parts = line.split(",")
                if len(parts) < 5:
                    continue
                try:
                    ts = int(parts[0])
                    close = float(parts[4])
                except ValueError:
                    continue
                # ms epoch -> UTC day key
                day = ts // 86400000
                out[sym][day] = close  # last write = last close of day
        except Exception:
            continue
    return out

def build_panel(closes):
    """Aligned sorted day list + per-symbol close arrays (None where missing)."""
    all_days = set()
    for d in closes.values():
        all_days.update(d.keys())
    days = sorted(all_days)
    syms = sorted(closes.keys())
    panel = {s: [closes[s].get(d) for d in days] for s in syms}
    return days, syms, panel

def trailing_return(panel, s, i, lb):
    arr = panel[s]
    if i - lb < 0:
        return None
    a, b = arr[i - lb], arr[i]
    if a is None or b is None or a <= 0:
        return None
    return b / a - 1.0

def run(panel, days, syms, lb, hold, fee_bps):
    """Returns list of per-rebalance net returns (fraction)."""
    rets = []
    prev_long, prev_short = set(), set()
    i = lb
    n = len(days)
    while i + hold < n:
        scored = []
        for s in syms:
            r = trailing_return(panel, s, i, lb)
            if r is not None:
                scored.append((r, s))
        if len(scored) < 10:
            i += hold
            continue
        scored.sort()
        k = max(1, int(len(scored) * QUANTILE))
        shorts = set(s for _, s in scored[:k])     # weakest -> short
        longs = set(s for _, s in scored[-k:])     # strongest -> long
        # forward return over hold days, dollar-neutral (avg long - avg short)
        def fwd(s):
            a, b = panel[s][i], panel[s][i + hold]
            if a is None or b is None or a <= 0:
                return None
            return b / a - 1.0
        lr = [fwd(s) for s in longs]; lr = [x for x in lr if x is not None]
        sr = [fwd(s) for s in shorts]; sr = [x for x in sr if x is not None]
        if not lr or not sr:
            i += hold
            continue
        gross = (sum(lr) / len(lr)) - (sum(sr) / len(sr))
        # turnover cost: symbols entering/leaving each leg pay round-trip fee
        turnover = len(longs ^ prev_long) + len(shorts ^ prev_short)
        denom = (len(longs) + len(shorts)) or 1
        cost = (turnover / denom) * (fee_bps / 10000.0)
        rets.append(gross - cost)
        prev_long, prev_short = longs, shorts
        i += hold
    return rets

def sharpe(rets, periods_per_year):
    if len(rets) < 2:
        return 0.0
    m = sum(rets) / len(rets)
    sd = math.sqrt(sum((x - m) ** 2 for x in rets) / (len(rets) - 1))
    if sd == 0:
        return 0.0
    return (m / sd) * math.sqrt(periods_per_year)

def main():
    print("=" * 72)
    print("CROSS-SECTIONAL LONG-SHORT (dollar-neutral momentum) — offline screen")
    print("=" * 72)
    print("loading daily closes from raw 1m CSVs...")
    closes = daily_closes()
    if not closes:
        print("NO DATA found in data/ — abort.")
        sys.exit(1)
    days, syms, panel = build_panel(closes)
    print(f"panel: {len(syms)} symbols × {len(days)} days "
          f"({days[0]} .. {days[-1]} epoch-days)")
    print(f"cost: {FEE_BPS}bp/side turnover; quantile top/bottom {int(QUANTILE*100)}%\n")

    best = None
    print(f"{'lookback':>9} {'hold':>5} {'rebals':>7} {'mean%':>8} {'ann.Sharpe':>11} "
          f"{'cum%':>9}")
    for lb in LOOKBACKS:
        for hold in HOLD_DAYS:
            rets = run(panel, days, syms, lb, hold, FEE_BPS)
            if not rets:
                continue
            ppy = 365.0 / hold
            sh = sharpe(rets, ppy)
            cum = 1.0
            for r in rets:
                cum *= (1 + r)
            cum_pct = (cum - 1) * 100
            mean_pct = (sum(rets) / len(rets)) * 100
            print(f"{lb:>9} {hold:>5} {len(rets):>7} {mean_pct:>8.3f} {sh:>11.2f} "
                  f"{cum_pct:>9.1f}")
            if best is None or sh > best[0]:
                best = (sh, lb, hold, cum_pct, mean_pct, len(rets))

    print()
    if best:
        sh, lb, hold, cum, mp, nr = best
        print(f"BEST: lookback={lb}d hold={hold}d  ann.Sharpe={sh:.2f}  "
              f"cum={cum:.1f}%  ({nr} rebalances)")
        print()
        if sh > 1.0 and cum > 0:
            print("VERDICT: cross-sectional L/S shows a POSITIVE, non-trivial-Sharpe stream.")
            print("  This is structurally different from the directional short class —")
            print("  WORTH a fresh pre-reg (build engine mode + walk-forward + overfit gate +")
            print("  correlation-to-LIVE). The first session result that could justify search.")
        elif cum > 0:
            print("VERDICT: marginally positive but low Sharpe — weak/ambiguous. Likely")
            print("  arbitraged or cost-eaten at realistic turnover. Not a clear candidate.")
        else:
            print("VERDICT: NEGATIVE — cross-sectional momentum does not pay on this universe")
            print("  net of costs. The directional class is confirmed as the only edge in the")
            print("  data; broad search closed (the one new axis also yields nothing).")
    else:
        print("VERDICT: no valid rebalances — insufficient overlapping data.")
    print("=" * 72)

if __name__ == "__main__":
    main()
