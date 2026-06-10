#!/usr/bin/env python3
"""crossvenue_spread_study.py — candidate #13: cross-venue funding-spread carry.

Pure CSV arithmetic, no engine. Per `results/strategy_candidates_batch2_2026-06-10.md`
(#13) + `tasks/todo.md` Phase 2. The SAME perp settles different funding on different
venues. Long the low-funding venue + short the high-funding venue → price risk cancels
(delta-neutral across venues), collect the funding spread. Arbitrage-class stream,
structurally uncorrelated with everything else in the project.

INPUTS
  Binance funding: data/funding/<SYM>.csv          (funding_time_ms,funding_rate)
  Bybit   funding: data/xvenue/bybit/<SYM>.csv      (same schema; full history)
  OKX     funding: data/xvenue/okx/<SYM>.csv        (same schema; ~3mo only — API limit)

THE TRADE (per symbol, per 8h settlement where both venues have a rate)
  spread = funding_A - funding_B  (signed). If |spread| > cost threshold, put on the
  delta-neutral pair (long the venue paying less / receiving more, short the other) and
  collect |spread| that settlement. PnL per settlement = |spread| - per-settlement cost.

COST MODEL (the killer — 4 legs)
  Entering/exiting the pair = 4 taker legs (open long+short, close long+short). But a
  carry position is HELD across many settlements, so amortize entry/exit over the hold.
  Conservative steady-state model: charge a round-trip 4-leg cost ONCE per "spread
  regime" (each time the favored direction flips) — i.e. turnover cost only when the
  sign of the spread flips, not every settlement. Per-settlement we collect |spread|.
  Leg cost = FEE_BP per leg; a flip = 4 legs (close old pair + open new) = 4*FEE_BP.

================================  PRE-REGISTRATION  ============================
  Venue pair:        Binance vs Bybit (full history; primary). OKX cross-check on
                     its ~3mo window only (underpowered, reported separately).
  Settlement match:  align on funding timestamp (8h). Both venues must have a rate
                     within MATCH_MS of each other.
  Cost per leg:      7.5 bp (taker; Bybit/Binance maker-ish blend conservative -> use
                     7.5 so a 4-leg flip = 30 bp).
  Entry threshold:   only hold the pair when |spread| > THRESH (cover the per-settlement
                     share of cost). Sweep THRESH in {0, 1, 2, 5} bp.
  ACCEPT:            net annualized collected > 0 with Sharpe > 1.0 AND positive across
                     years AND capacity-honest (spreads on majors are thin/arbed).
  Honest framing (from spec): highest-probability-of-positive, LOWEST ceiling. We CANNOT
                     execute on Bybit/OKX today (Binance-only engine) — this answers
                     "does the edge exist", venue build only if it clears.
===============================================================================
"""
import os, glob, csv, sys, math, datetime
from collections import defaultdict
import numpy as np

BINANCE_DIR = "data/funding"
BYBIT_DIR = "data/xvenue/bybit"
OKX_DIR = "data/xvenue/okx"
FEE_BP = 7.5             # per leg; 4-leg flip = 30 bp
MATCH_MS = 60 * 60 * 1000  # align settlements within 1h
THRESHOLDS_BP = [0, 1, 2, 5]


def load(path):
    out = {}
    if not os.path.exists(path):
        return out
    with open(path) as f:
        r = csv.DictReader(f)
        for row in r:
            try:
                ts = int(row["funding_time_ms"]); rate = float(row["funding_rate"])
            except (ValueError, KeyError):
                continue
            out[(ts // MATCH_MS)] = rate  # bucket to 1h for alignment
    return out


def pair_spreads(sym, venue_dir):
    """Aligned list of (settle_ms_bucket, binance_rate, venue_rate) for a symbol."""
    b = load(os.path.join(BINANCE_DIR, f"{sym}.csv"))
    v = load(os.path.join(venue_dir, f"{sym}.csv"))
    if not b or not v:
        return []
    keys = sorted(set(b) & set(v))
    return [(k, b[k], v[k]) for k in keys]


def backtest(sym, venue_dir, thresh_bp):
    """Return (per-settlement net bp list, by-year dict) for the spread carry."""
    rows = pair_spreads(sym, venue_dir)
    if len(rows) < 10:
        return [], {}
    thresh = thresh_bp / 1e4
    nets = []
    byyear = defaultdict(list)
    prev_sign = 0
    for kbucket, br, vr in rows:
        spread = br - vr                 # signed
        aspread = abs(spread)
        if aspread <= thresh:
            # not in a position this settlement; if we were, count an exit flip
            if prev_sign != 0:
                nets.append(-2 * FEE_BP)  # close 2 legs
                prev_sign = 0
            continue
        sign = 1 if spread > 0 else -1
        # collect the spread this settlement (in bp)
        collect = aspread * 1e4
        cost = 0.0
        if sign != prev_sign:
            # flip: close old pair (if any) + open new pair
            legs = 4 if prev_sign != 0 else 2
            cost = legs * FEE_BP
        net = collect - cost
        nets.append(net)
        yr = datetime.datetime.fromtimestamp(kbucket * MATCH_MS / 1000,
                                             datetime.timezone.utc).year
        byyear[yr].append(net)
        prev_sign = sign
    return nets, byyear


def run_venue(venue_name, venue_dir, syms):
    print(f"\n{'='*78}\nBINANCE vs {venue_name.upper()} — cross-venue funding spread (#13)\n{'='*78}")
    print(f"{'thresh':>7} {'n_settle':>9} {'sum_net%':>9} {'mean_bp':>9} "
          f"{'ann.Sharpe':>11} {'pos_yrs':>8}")
    best = None
    for thr in THRESHOLDS_BP:
        all_nets = []
        all_year = defaultdict(list)
        nsym = 0
        for s in syms:
            nets, byyear = backtest(s, venue_dir, thr)
            if nets:
                nsym += 1
                all_nets.extend(nets)
                for y, vs in byyear.items():
                    all_year[y].extend(vs)
        if not all_nets:
            continue
        a = np.array(all_nets)
        # treat each settlement as a period; 3/day * 365 = 1095 periods/yr
        ppy = 1095
        mean = a.mean()
        sd = a.std(ddof=1) if len(a) > 1 else 0
        sharpe = (mean / sd * math.sqrt(ppy)) if sd > 0 else 0
        years = sorted(all_year)
        pos = sum(1 for y in years if np.mean(all_year[y]) > 0)
        sum_pct = a.sum() / 100.0  # bp -> % (sum of all settlement nets)
        print(f"{thr:>6}b {len(a):>9} {sum_pct:>9.2f} {mean:>9.3f} {sharpe:>11.2f} "
              f"{pos:>5}/{len(years)}")
        if best is None or sharpe > best[0]:
            best = (sharpe, thr, mean, len(a), pos, len(years), all_year)
    return best


def main():
    syms = sorted(os.path.basename(p)[:-4] for p in glob.glob(os.path.join(BYBIT_DIR, "*.csv")))
    if not syms:
        print("No Bybit data — run scripts/fetch_crossvenue_funding.py first.")
        sys.exit(1)
    print(f"symbols with Bybit data: {len(syms)}")

    best_by = run_venue("bybit", BYBIT_DIR, syms)
    okx_syms = sorted(os.path.basename(p)[:-4] for p in glob.glob(os.path.join(OKX_DIR, "*.csv")))
    best_ok = run_venue("okx", OKX_DIR, okx_syms) if okx_syms else None

    print(f"\n{'='*78}\nVERDICT (candidate #13)")
    for name, best in (("Binance-Bybit", best_by), ("Binance-OKX", best_ok)):
        if not best:
            print(f"  {name}: no data."); continue
        sharpe, thr, mean, n, pos, nyears, _ = best
        clean = sharpe > 1.0 and pos == nyears and mean > 0
        tag = ("CANDIDATE -> corr-to-LIVE + capacity check" if clean
               else "NO-GO (arbed/cost-eaten or regime-concentrated)")
        print(f"  {name}: best thresh={thr}bp Sharpe={sharpe:.2f} mean={mean:.3f}bp/settle "
              f"pos={pos}/{nyears}yr  -> {tag}")
    print("  NOTE: OKX history ~3mo only (API limit) — underpowered, recent-regime only.")
    print("  NOTE: cannot execute on Bybit/OKX today (Binance-only engine). This answers")
    print("        'does the spread edge exist', not 'can we capture it'.")
    print("="*78)


if __name__ == "__main__":
    main()
