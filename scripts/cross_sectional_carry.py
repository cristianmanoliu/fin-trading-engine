#!/usr/bin/env python3
"""cross_sectional_carry.py — candidate #8: dollar-neutral cross-sectional CARRY.

Sibling of `cross_sectional_ls.py` (which ranks by trailing RETURN = momentum).
This one ranks by trailing FUNDING = carry. Per `results/strategy_candidates_2026-06-10.md`
(#8) and `tasks/todo.md` Phase 0. Reuses that script's panel/sharpe machinery —
do NOT inline a second copy of `daily_closes` / `build_panel` / `sharpe`.

THE BET
-------
Each rebalance: rank symbols by trailing N-day mean funding rate. A perp with very
NEGATIVE funding pays the long (shorts pay longs) → you are PAID to hold it long.
A perp with very POSITIVE funding pays the short → you are PAID to hold it short.
So the dollar-neutral carry book is:

    LONG  the bottom quantile (most-negative funding → collect funding holding long)
    SHORT the top quantile    (most-positive funding → collect funding holding short)

PnL per rebalance = price move of the L/S book  +  net funding COLLECTED over the hold.
Both legs are designed to be funding-positive; the price spread is the risk you carry
to harvest the funding. This is structurally distinct from momentum (#cross_sectional_ls)
AND from the live directional short — gate 3 (corr-to-LIVE) still applies.

HARDENED-AUDIT LENS (mirrors the momentum verdict, results/cross_sectional_verdict_2026-06-10.md)
- Min-universe gate: skip any rebalance with < MIN_UNIVERSE ranked symbols (the
  early-history ragged days are degenerate 1-vs-1 bets — they fabricated the momentum
  Sharpe before this gate).
- Realistic cost: 35 bp/side turnover (NOT 10) — daily alt rebalancing is expensive.
- Per-period (by-year) Sharpe + mean: a real carry edge is not concentrated in one
  era. The momentum factor died after 2021 on exactly this test.
- No compounding fantasy: report per-rebalance mean + annualized Sharpe, not cum%.

================================  PRE-REGISTRATION  ============================
Frozen before the run (results/INDEX.md discipline).
  Funding lookbacks (days):  7, 14, 30   (trailing mean funding rate)
  Hold / rebalance (days):   1, 3, 7
  Quantile:                  top/bottom 20%
  Min universe:              20 ranked symbols
  Cost:                      35 bp/side, charged on per-leg turnover
  Funding accrual:           summed actual settled funding over the hold, signed by
                             leg (long collects −funding, short collects +funding)
  ACCEPT (proceed to gates): best cell ann.Sharpe > 1.0 AND positive in a MAJORITY
                             of readable years (>= ceil(readable/2)+? — strict: ALL
                             readable years positive for a clean pass; majority =
                             MARGINAL). Mirrors the momentum bar that the factor failed.
===============================================================================
"""
import os, glob, csv, math, sys, datetime
from collections import defaultdict

# reuse the momentum script's machinery (same dir)
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from cross_sectional_ls import daily_closes, build_panel, sharpe, DATA  # noqa: E402

FUNDING_DIR = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "data", "funding")
FEE_BPS = 35.0            # hardened: 35bp/side (not 10)
LOOKBACKS = [7, 14, 30]  # trailing-funding ranking windows (days)
HOLD_DAYS = [1, 3, 7]
QUANTILE = 0.2
MIN_UNIVERSE = 20        # hardened: skip degenerate ragged-history days
MS_DAY = 86_400_000


def daily_funding():
    """symbol -> {epoch_day: mean funding rate settled that UTC day}.

    Funding settles 3x/day (00/08/16 UTC). We collapse to a daily mean rate so it
    aligns with the daily-close panel. Also returns the per-day SUM (for accrual).
    """
    mean_out = defaultdict(dict)
    sum_out = defaultdict(dict)
    for fp in glob.glob(os.path.join(FUNDING_DIR, "*.csv")):
        sym = os.path.basename(fp)[:-4]
        bucket = defaultdict(list)
        with open(fp) as f:
            for row in csv.DictReader(f):
                try:
                    ts = int(row["funding_time_ms"])
                    fr = float(row["funding_rate"])
                except (ValueError, KeyError):
                    continue
                bucket[ts // MS_DAY].append(fr)
        for day, vals in bucket.items():
            mean_out[sym][day] = sum(vals) / len(vals)
            sum_out[sym][day] = sum(vals)
    return mean_out, sum_out


def trailing_funding(fmean, s, day, lb):
    """Mean of daily-mean funding over the lb days ENDING at `day` (inclusive of
    day-lb .. day-1; excludes `day` itself to avoid look-ahead at rebalance)."""
    vals = []
    for d in range(day - lb, day):
        v = fmean.get(s, {}).get(d)
        if v is not None:
            vals.append(v)
    if not vals:
        return None
    return sum(vals) / len(vals)


def hold_funding_collected(fsum, s, day, hold, side):
    """Net funding COLLECTED holding `s` over [day, day+hold), as a fraction of
    notional. A long PAYS positive funding, so it collects -funding; a short
    RECEIVES positive funding, so it collects +funding. side: +1 long, -1 short."""
    total = 0.0
    for d in range(day, day + hold):
        v = fsum.get(s, {}).get(d)
        if v is not None:
            total += v
    return -total if side == +1 else total


def run(panel, days, syms, fmean, fsum, day_index, lb, hold, fee_bps):
    """Per-rebalance net returns (price spread + funding accrual − cost).

    panel/days are indexed positionally; day_index[i] = epoch_day of column i so
    funding (keyed by epoch_day) aligns to the same column.
    """
    rets = []
    rets_year = defaultdict(list)
    prev_long, prev_short = set(), set()
    i = lb
    n = len(days)
    while i + hold < n:
        eday = day_index[i]
        scored = []
        for s in syms:
            f = trailing_funding(fmean, s, eday, lb)
            # require a live price too (need to trade it)
            if f is not None and panel[s][i] is not None:
                scored.append((f, s))
        if len(scored) < MIN_UNIVERSE:
            i += hold
            continue
        scored.sort()
        k = max(1, int(len(scored) * QUANTILE))
        longs = set(s for _, s in scored[:k])       # most NEGATIVE funding -> long, collect
        shorts = set(s for _, s in scored[-k:])     # most POSITIVE funding -> short, collect

        def price_fwd(s):
            a, b = panel[s][i], panel[s][i + hold]
            if a is None or b is None or a <= 0:
                return None
            return b / a - 1.0

        # long leg: +price move + funding collected; short leg: -price move + funding collected
        leg_rets = []
        for s in longs:
            p = price_fwd(s)
            if p is None:
                continue
            fund = hold_funding_collected(fsum, s, day_index[i], hold, +1)
            leg_rets.append(("L", p + fund))
        for s in shorts:
            p = price_fwd(s)
            if p is None:
                continue
            fund = hold_funding_collected(fsum, s, day_index[i], hold, -1)
            leg_rets.append(("S", -p + fund))
        lr = [r for side, r in leg_rets if side == "L"]
        sr = [r for side, r in leg_rets if side == "S"]
        if not lr or not sr:
            i += hold
            continue
        gross = (sum(lr) / len(lr) + sum(sr) / len(sr)) / 2.0  # equal dollars per side
        turnover = len(longs ^ prev_long) + len(shorts ^ prev_short)
        denom = (len(longs) + len(shorts)) or 1
        cost = (turnover / denom) * (fee_bps / 10000.0)
        net = gross - cost
        rets.append(net)
        yr = datetime.datetime.fromtimestamp(eday * MS_DAY / 1000, datetime.timezone.utc).year
        rets_year[yr].append(net)
        prev_long, prev_short = longs, shorts
        i += hold
    return rets, rets_year


def main():
    print("=" * 72)
    print("CROSS-SECTIONAL CARRY (dollar-neutral, rank by trailing funding) — #8")
    print("hardened: min-universe>=%d, %gbp/side, funding-accrued, by-year" % (MIN_UNIVERSE, FEE_BPS))
    print("=" * 72)
    print("loading daily closes...")
    closes = daily_closes()
    if not closes:
        print("NO DATA in data/ — abort."); sys.exit(1)
    days, syms, panel = build_panel(closes)
    print("loading daily funding...")
    fmean, fsum = daily_funding()
    print(f"panel: {len(syms)} symbols x {len(days)} days "
          f"({days[0]}..{days[-1]} epoch-days); funding syms={len(fmean)}\n")

    day_index = days  # build_panel returns days as the epoch-day list itself

    best = None
    best_year = None
    print(f"{'lb':>4} {'hold':>5} {'rebals':>7} {'mean%':>9} {'ann.Sharpe':>11}")
    for lb in LOOKBACKS:
        for hold in HOLD_DAYS:
            rets, rets_year = run(panel, days, syms, fmean, fsum, day_index, lb, hold, FEE_BPS)
            if not rets:
                continue
            ppy = 365.0 / hold
            sh = sharpe(rets, ppy)
            mean_pct = (sum(rets) / len(rets)) * 100
            print(f"{lb:>4} {hold:>5} {len(rets):>7} {mean_pct:>9.4f} {sh:>11.2f}")
            if best is None or sh > best[0]:
                best = (sh, lb, hold, mean_pct, len(rets))
                best_year = rets_year

    print()
    if not best:
        print("VERDICT: no valid rebalances — insufficient overlapping data.")
        print("=" * 72); return
    sh, lb, hold, mp, nr = best
    print(f"BEST cell: lb={lb}d hold={hold}d  ann.Sharpe={sh:.2f}  "
          f"mean/reb={mp:.4f}%  ({nr} rebalances)")
    print()
    print(f"  {'year':<6}{'rebals':>8}{'mean%/reb':>12}{'ann.Sharpe':>12}")
    print("  " + "-" * 36)
    read_years = 0; pos_years = 0
    for yr in sorted(best_year):
        a = best_year[yr]
        if len(a) < 2:
            print(f"  {yr:<6}{len(a):>8}{'(low-n)':>12}")
            continue
        ppy = 365.0 / hold
        ysh = sharpe(a, ppy)
        ymean = (sum(a) / len(a)) * 100
        read_years += 1
        if ymean > 0:
            pos_years += 1
        print(f"  {yr:<6}{len(a):>8}{ymean:>12.4f}{ysh:>12.2f}")
    print()
    print("=" * 72)
    print("VERDICT (candidate #8 — cross-sectional carry)")
    if sh > 1.0 and read_years >= 2 and pos_years == read_years:
        print(f"  CANDIDATE: ann.Sharpe {sh:.2f}>1 AND positive in all {read_years} years "
              f"-> proceed to walk-forward / overfit / corr-to-LIVE gates.")
    elif sh > 0.5 and pos_years >= (read_years + 1) // 2:
        print(f"  MARGINAL: Sharpe {sh:.2f}, positive in {pos_years}/{read_years} years. "
              f"Likely the same post-2021 decay as momentum — decompose before believing.")
    else:
        print(f"  NO-GO: Sharpe {sh:.2f}, positive in only {pos_years}/{read_years} years. "
              f"Carry does not pay net of 35bp on this universe (or concentrated pre-2022, "
              f"like momentum). Funding-carry family note: also test #13 cross-venue spread.")
    print("=" * 72)


if __name__ == "__main__":
    main()
