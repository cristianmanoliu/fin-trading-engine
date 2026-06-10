#!/usr/bin/env python3
"""listing_drift_study.py — candidate #12: new-listing drift (short the hype unload).

DE-SURVIVORSHIP harness. Reads the full-universe listing data fetched by
`scripts/fetch_listing_data.sh` (every USDT perp EVER listed, incl. delisted — the
57 local CSVs are survivors and bias the test). Per `results/strategy_candidates_batch2_2026-06-10.md`
(#12) + `tasks/todo.md` Phase 0.

THE BET
-------
New Binance perp listings systematically underperform their first weeks (airdrop
farmers + hype unload + MM unwind). SHORT at close of listing-day N, hold M days,
exit at close. Net per trade = price drop − round-trip slip − funding bleed.

The killers the spec flagged, both modeled here:
  1. SURVIVORSHIP — fixed by the de-survivorship fetch (delisted dumped hardest).
  2. EARLY-LISTING FUNDING BLEED — new perps run deeply NEGATIVE funding, and a
     SHORT PAYS negative funding (you pay |funding| each settlement). 1000LUNC day-1
     was −21bp/8h ≈ −64bp/day. Funding accrued from real settled rates, signed for
     a short. This is the #1 way the price edge gets erased.
  3. LISTING SLIP — far worse than steady-state. Pre-registered at >= 25 bp round-trip.

================================  PRE-REGISTRATION  ============================
Frozen before running on the full universe (the survivor-only probe in
tasks/listing_drift_probe.py already showed N1/M7 mean +7.7% gross-of-funding;
this harness adds funding + de-survivorship + the honesty checks).
  N (listing day to short at close):   1, 2, 3
  M (hold days):                       3, 5, 7
  Slip (round-trip):                   25 bp  (listing-day liquidity haircut)
  Funding:                             accrued from real settled rates over the hold,
                                       SHORT pays negative funding (signed -rate sum)
  Power floor:                         >= 60 listings with usable data to read a cell
  Honesty (the #11 lesson):            report median + win-rate + drop-top-5% + by-year.
                                       A fat-tailed coin-flip is NOT an edge even if the
                                       mean is large.
  ACCEPT (proceed to gates):           a cell with n>=60, mean net >0, median net >0,
                                       win-rate > 50%, edge survives dropping top 5%,
                                       AND positive in a majority of readable years.
===============================================================================
"""
import os, glob, csv, math, sys, argparse, datetime
from collections import defaultdict
import numpy as np

KLINE_DIR = "data/listing/klines"
FUND_DIR = "data/listing/funding"
MS_DAY = 86_400_000
SLIP_BP = 25.0
N_GRID = [1, 2, 3]
M_GRID = [3, 5, 7]
POWER_FLOOR = 60


def load_daily(sym):
    """epoch_day -> close, from <SYM>-1d.csv (skips repeated headers from concat)."""
    path = os.path.join(KLINE_DIR, f"{sym}-1d.csv")
    days = {}
    if not os.path.exists(path):
        return days
    with open(path) as f:
        for line in f:
            p = line.split(",")
            if len(p) < 5:
                continue
            try:
                ot = int(p[0]); c = float(p[4])
            except ValueError:
                continue  # header rows land here
            days[ot // MS_DAY] = c
    return days


def load_funding(sym):
    """list of (settle_ms, rate) from archive schema
    `calc_time,funding_interval_hours,last_funding_rate`. Skips headers."""
    path = os.path.join(FUND_DIR, f"{sym}.csv")
    out = []
    if not os.path.exists(path):
        return out
    with open(path) as f:
        for line in f:
            p = line.strip().split(",")
            if len(p) < 3:
                continue
            try:
                ts = int(p[0]); rate = float(p[2])
            except ValueError:
                continue  # header
            out.append((ts, rate))
    out.sort()
    return out


def funding_bleed_for_short(funding, start_ms, end_ms):
    """Net funding a SHORT collects over [start_ms, end_ms). Short receives +rate
    (when funding positive longs pay shorts) and PAYS when rate negative. Return as
    a fraction of notional (sum of settled rates in window)."""
    total = 0.0
    for ts, rate in funding:
        if start_ms <= ts < end_ms:
            total += rate     # short collects +rate; negative rate => short pays
    return total


def run_cell(syms, N, M):
    """Return (nets array, by-year dict) for shorting at close of day N, hold M."""
    nets = []
    byyear = defaultdict(list)
    slip = SLIP_BP / 1e4
    for sym in syms:
        d = load_daily(sym)
        if len(d) < N + M + 1:
            continue
        days = sorted(d.keys())
        entry_day = days[N]
        exit_day = days[N + M]
        ec = d[entry_day]; xc = d[exit_day]
        if ec <= 0 or xc <= 0:
            continue
        # SHORT price pnl: (entry - exit)/entry
        price = (ec - xc) / ec
        # funding over the hold window [entry close, exit close)
        start_ms = (entry_day + 1) * MS_DAY  # close of entry_day ~ start of next day
        end_ms = (exit_day + 1) * MS_DAY
        fund = funding_bleed_for_short(load_funding(sym), start_ms, end_ms)
        net = price + fund - slip
        nets.append(net)
        yr = datetime.datetime.fromtimestamp(entry_day * MS_DAY / 1000,
                                             datetime.timezone.utc).year
        byyear[yr].append(net)
    return np.array(nets), byyear


def cell_stats(nets):
    n = len(nets)
    if n == 0:
        return None
    srt = np.sort(nets)
    mean = srt.mean()
    med = float(np.median(srt))
    wr = float((srt > 0).mean())
    t = mean / (srt.std(ddof=1) / math.sqrt(n)) if n > 1 else float("nan")
    k5 = max(1, int(n * 0.05))
    drop5 = srt[:-k5].mean()
    return dict(n=n, mean=mean, med=med, wr=wr, t=t, drop5=drop5)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--limit", type=int, default=0)
    args = ap.parse_args()

    syms = sorted(os.path.basename(p)[:-7] for p in glob.glob(os.path.join(KLINE_DIR, "*-1d.csv")))
    if args.limit:
        syms = syms[: args.limit]
    if not syms:
        print(f"NO DATA in {KLINE_DIR}/ — run scripts/fetch_listing_data.sh first.")
        sys.exit(1)

    print("=" * 78)
    print(f"NEW-LISTING DRIFT (#12) — de-survivorship, {len(syms)} symbols ever listed")
    print(f"short@close day N, hold M, slip {SLIP_BP}bp r/t, funding-accrued (short pays -rate)")
    print("=" * 78)
    print(f"{'N':>2} {'M':>2} {'n':>5} {'mean%':>8} {'med%':>8} {'win':>5} "
          f"{'t':>6} {'drop5%':>8}  verdict")
    print("-" * 78)

    best = None
    results = {}
    for N in N_GRID:
        for M in M_GRID:
            nets, byyear = run_cell(syms, N, M)
            st = cell_stats(nets)
            if st is None:
                continue
            results[(N, M)] = (st, byyear, nets)
            tradeable = (st["n"] >= POWER_FLOOR and st["mean"] > 0 and st["med"] > 0
                         and st["wr"] > 0.50 and st["drop5"] > 0)
            tag = "TRADEABLE?" if tradeable else ("low-n" if st["n"] < POWER_FLOOR else "weak")
            print(f"{N:>2} {M:>2} {st['n']:>5} {st['mean']*100:>8.2f} {st['med']*100:>8.2f} "
                  f"{st['wr']:>5.0%} {st['t']:>6.2f} {st['drop5']*100:>8.2f}  {tag}")
            score = st["mean"] if (st["n"] >= POWER_FLOOR and st["med"] > 0) else -1e9
            if best is None or score > best[0]:
                best = (score, N, M)

    # by-year decomposition for the best readable cell
    if best and best[0] > -1e8:
        _, N, M = best
        st, byyear, nets = results[(N, M)]
        print()
        print("=" * 78)
        print(f"BEST readable cell N={N} M={M}: by-year decomposition + honesty")
        print(f"  pooled: n={st['n']} mean={st['mean']*100:+.2f}% median={st['med']*100:+.2f}% "
              f"win={st['wr']:.0%} t={st['t']:.2f} drop-top-5%={st['drop5']*100:+.2f}%")
        print()
        print(f"  {'year':<6}{'n':>6}{'mean%':>10}{'med%':>10}{'win':>7}")
        print("  " + "-" * 39)
        read_years = pos_years = 0
        for yr in sorted(byyear):
            a = np.array(byyear[yr])
            if len(a) < 10:
                print(f"  {yr:<6}{len(a):>6}{'(low-n)':>10}")
                continue
            read_years += 1
            if a.mean() > 0:
                pos_years += 1
            print(f"  {yr:<6}{len(a):>6}{a.mean()*100:>10.2f}{np.median(a)*100:>10.2f}{(a>0).mean():>7.0%}")
        print()
        print("=" * 78)
        print("VERDICT (candidate #12)")
        tail_ok = st["med"] > 0 and st["drop5"] > 0
        if not tail_ok:
            print("  TAIL-MIRAGE / NO-GO: median<=0 or edge inverts dropping top 5%. "
                  "Like #11, the mean is a few violent dumps. No engine mode.")
        elif read_years >= 2 and pos_years >= (read_years + 1) // 2 and st["wr"] > 0.5:
            print(f"  CANDIDATE: net-positive, median>0, win {st['wr']:.0%}, survives top-5% drop, "
                  f"positive in {pos_years}/{read_years} years -> proceed to walk-forward / "
                  f"overfit / corr-to-LIVE gates. (De-survivorship raises confidence.)")
        else:
            print(f"  MARGINAL: survives tail but only {pos_years}/{read_years} years positive. "
                  f"Regime-fragile — decompose before any build.")
        print("=" * 78)
    else:
        print("\nNo readable cell (all < power floor). Need more fetched symbols.")


if __name__ == "__main__":
    main()
