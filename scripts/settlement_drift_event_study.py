#!/usr/bin/env python3
"""settlement_drift_event_study.py — candidate #11: funding-settlement window drift.

EVENT STUDY, NOT an engine mode. Per `results/strategy_candidates_batch2_2026-06-10.md`
(#11) and `tasks/todo.md` Phase 0. The job is to answer ONE question cheaply, from
100%-local data: around funding settlements (00/08/16 UTC), when funding is at an
extreme, is there a systematic, tradeable price drift into/out of the settlement
timestamp — and does it clear realistic round-trip cost?

GOVERNING PRECEDENT: hold time here is 2-4h. The Purgatory −$40M fee-death
(`project_purgatory_method_feedeath`) means a short-hold perp idea is presumptively
fee-dead. So the DELIVERABLE of this script is the FEE-DEATH SCREEN, run FIRST: the
mean per-bucket expected gross move vs the round-trip cost floor. If the extreme
buckets' directional edge does not clear cost with margin, candidate #11 is dead and
NO engine mode gets built. We do not proceed to walk-forward / overfit / correlation
gates on a fee-dead signal.

================================  PRE-REGISTRATION  ============================
Frozen BEFORE running (project results/INDEX.md discipline). No post-hoc grid.

Settlement clock:   00:00, 08:00, 16:00 UTC (confirmed 8h cadence, all 57 syms).
Funding extreme:    bucket each settlement by the cross-sectional percentile of the
                    funding rate that settles AT that timestamp (the rate longs pay /
                    shorts receive). Buckets are computed PER SYMBOL over its own
                    history (a symbol's own funding distribution), so "extreme" means
                    extreme for that asset, not vs BTC.
Percentile bands:   EXTREME_HIGH = top 10% of |funding| with funding>0 (longs pay a lot)
                    EXTREME_LOW  = bottom 10% i.e. funding<0 most negative (shorts pay)
                    Also report deciles for the full distribution (diagnostic).
Event window:       pre  = (-120 min, 0]  returns leading INTO settlement
                    post = (0, +120 min]  returns AFTER settlement
                    Measured as log-return of close, anchored at the settlement minute.
Hypothesis (lit):   funding>0 extreme  -> longs trim INTO settlement (pre drift DOWN),
                                          re-add AFTER (post drift UP).  -> fade the payer.
                    funding<0 extreme  -> mirror (pre UP, post DOWN).
Tradeable variant:  the strongest single directional leg per extreme bucket — entry at
                    settlement-2h (high band) or the symmetric leg — held to the window
                    edge. We take the BEST-CASE leg (most favorable) for the screen; if
                    even the best-case leg is fee-dead, the candidate is dead a fortiori.
COST FLOOR:         round-trip taker = 10 bp fee + 5 bp slip = 15 bp (project standard
                    `--fee-bps 10 --stop-slippage-bps 5`). PASS the fee-death screen
                    requires mean per-trade |gross move| >= 2 x 15 = 30 bp on the
                    extreme bucket (2x margin so a noisy edge isn't called tradeable).
                    Report the raw bp so the margin is auditable either way.
Power:              n per extreme bucket must be >= 60 (project power floor) to be read.
===============================================================================

Output: prints a per-bucket table (mean pre/post move bp, std, n, t-stat, net-of-cost),
plus a fee-death VERDICT line. Writes nothing to results/ — the human reads the verdict
and records it. Pure stdlib + numpy (project convention: no pandas).
"""

import os, glob, csv, math, sys, argparse, datetime
from collections import defaultdict
import numpy as np

# ---- frozen constants (pre-registration above) ----
SETTLE_HOURS = {0, 8, 16}
WINDOW_MIN = 120                 # +/- 2h around settlement
EXTREME_PCT = 0.10               # top/bottom 10% of funding
COST_BP_ROUNDTRIP = 15.0         # 10bp fee + 5bp slip
FEE_DEATH_MARGIN = 2.0           # require 2x cost to call tradeable
POWER_FLOOR = 60                 # min n per bucket to read
MIN_PER_MIN = 60_000             # ms per minute

DATA_DIR = "data"
FUNDING_DIR = "data/funding"


def load_funding(sym):
    """Return dict: settlement_ms -> funding_rate (float). 8h cadence."""
    path = os.path.join(FUNDING_DIR, f"{sym}.csv")
    out = {}
    with open(path) as f:
        r = csv.DictReader(f)
        for row in r:
            ts = int(row["funding_time_ms"])
            # snap to the settlement minute (some rows have +2ms jitter)
            ts = (ts // MIN_PER_MIN) * MIN_PER_MIN
            try:
                out[ts] = float(row["funding_rate"])
            except ValueError:
                continue
    return out


def load_closes(sym):
    """Return dict: minute_open_ms -> close (float), across all monthly CSVs.

    Keyed on the 1m candle open_time (ms), which lands exactly on the minute.
    Settlement at HH:00 corresponds to the candle whose open_time == that minute.
    """
    closes = {}
    files = sorted(glob.glob(os.path.join(DATA_DIR, f"{sym}-1m-*.csv")))
    for fp in files:
        with open(fp) as f:
            r = csv.reader(f)
            next(r, None)  # consume header row
            for row in r:
                if not row:
                    continue
                try:
                    ot = int(row[0])
                    c = float(row[4])
                except (ValueError, IndexError):
                    continue
                closes[ot] = c
    return closes, len(files)


def window_return_bp(closes, settle_ms, lo_min, hi_min):
    """log-return * 1e4 (bp) of close from settle+lo_min to settle+hi_min.

    Requires both anchor minutes present. Returns None if either is missing.
    lo_min/hi_min are signed minute offsets (e.g. -120, 0, +120).
    """
    a = closes.get(settle_ms + lo_min * MIN_PER_MIN)
    b = closes.get(settle_ms + hi_min * MIN_PER_MIN)
    if a is None or b is None or a <= 0 or b <= 0:
        return None
    return math.log(b / a) * 1e4


def tstat(arr):
    a = np.asarray(arr, dtype=float)
    if len(a) < 2:
        return float("nan")
    return float(a.mean() / (a.std(ddof=1) / math.sqrt(len(a)) + 1e-12))


def analyze_symbol(sym, agg, byyear):
    """Populate agg[bucket][leg] lists with bp moves for this symbol.

    byyear[year] -> list of per-event NET bp for the headline tradeable rule
    (EXTREME_LOW/pre, long into settlement). Used for gate-4 decomposition +
    a sign-honest per-trade net check (we COMMIT to the long sign here, no
    post-hoc abs()).
    """
    funding = load_funding(sym)
    if not funding:
        return 0
    closes, nfiles = load_closes(sym)
    if not closes:
        return 0

    # per-symbol funding extreme thresholds
    rates = np.array(list(funding.values()), dtype=float)
    pos = rates[rates > 0]
    neg = rates[rates < 0]
    hi_thr = np.quantile(pos, 1 - EXTREME_PCT) if len(pos) else float("inf")
    lo_thr = np.quantile(neg, EXTREME_PCT) if len(neg) else float("-inf")

    used = 0
    for settle_ms, fr in funding.items():
        # bucket
        if fr >= hi_thr and fr > 0:
            bucket = "EXTREME_HIGH"   # longs pay a lot
        elif fr <= lo_thr and fr < 0:
            bucket = "EXTREME_LOW"    # shorts pay a lot
        else:
            bucket = "MID"
        pre = window_return_bp(closes, settle_ms, -WINDOW_MIN, 0)
        post = window_return_bp(closes, settle_ms, 0, WINDOW_MIN)
        if pre is not None:
            agg[bucket]["pre"].append(pre)
        if post is not None:
            agg[bucket]["post"].append(post)
        if pre is not None or post is not None:
            used += 1
        # headline tradeable rule: EXTREME_LOW, go LONG into settlement (pre leg).
        # Net per trade = +pre_return - round-trip cost. Sign COMMITTED (long).
        if bucket == "EXTREME_LOW" and pre is not None:
            year = datetime.datetime.fromtimestamp(
                settle_ms / 1000, datetime.timezone.utc).year
            byyear[year].append(pre - COST_BP_ROUNDTRIP)
    return used


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--symbols", default="ALL",
                    help="comma list or ALL (default: every funding CSV)")
    ap.add_argument("--limit", type=int, default=0,
                    help="cap number of symbols (debug)")
    args = ap.parse_args()

    if args.symbols == "ALL":
        syms = sorted(
            os.path.basename(p)[:-4]
            for p in glob.glob(os.path.join(FUNDING_DIR, "*.csv"))
        )
    else:
        syms = [s.strip() for s in args.symbols.split(",") if s.strip()]
    if args.limit:
        syms = syms[: args.limit]

    agg = {b: {"pre": [], "post": []} for b in ("EXTREME_HIGH", "EXTREME_LOW", "MID")}
    byyear = defaultdict(list)

    print(f"# settlement-drift event study (#11) — {len(syms)} symbols")
    print(f"# window +/-{WINDOW_MIN}min | extreme={EXTREME_PCT:.0%} | "
          f"cost floor={COST_BP_ROUNDTRIP}bp | pass>= {COST_BP_ROUNDTRIP*FEE_DEATH_MARGIN}bp\n")

    total_events = 0
    for i, sym in enumerate(syms, 1):
        try:
            n = analyze_symbol(sym, agg, byyear)
        except FileNotFoundError:
            n = 0
        total_events += n
        print(f"  [{i:>2}/{len(syms)}] {sym:<14} events={n}", file=sys.stderr)

    print(f"\nTotal settlement events with data: {total_events}\n")
    hdr = f"{'bucket':<14}{'leg':<6}{'n':>7}{'mean_bp':>10}{'std_bp':>9}{'t':>8}{'net_bp':>9}"
    print(hdr)
    print("-" * len(hdr))

    # fee-death assessment on the extreme buckets' best directional leg
    pass_bp = COST_BP_ROUNDTRIP * FEE_DEATH_MARGIN
    best_abs = 0.0
    best_desc = "none"
    for bucket in ("EXTREME_HIGH", "EXTREME_LOW", "MID"):
        for leg in ("pre", "post"):
            arr = agg[bucket][leg]
            n = len(arr)
            if n == 0:
                continue
            a = np.asarray(arr, dtype=float)
            mean = a.mean()
            std = a.std(ddof=1) if n > 1 else float("nan")
            t = tstat(a)
            # a directional trade nets |mean| minus round-trip cost (we get to pick
            # the favorable sign per bucket — best case for the screen)
            net = abs(mean) - COST_BP_ROUNDTRIP
            print(f"{bucket:<14}{leg:<6}{n:>7}{mean:>10.2f}{std:>9.1f}{t:>8.2f}{net:>9.2f}")
            if bucket.startswith("EXTREME") and n >= POWER_FLOOR and abs(mean) > best_abs:
                best_abs = abs(mean)
                best_desc = f"{bucket}/{leg} mean={mean:.2f}bp n={n}"

    print()
    print("=" * 64)
    print("FEE-DEATH SCREEN (candidate #11) — abs() best-case, screen only")
    print(f"  best extreme-bucket directional leg: {best_desc}")
    print(f"  best |gross move|: {best_abs:.2f} bp")
    print(f"  round-trip cost floor: {COST_BP_ROUNDTRIP:.1f} bp")
    print(f"  pass threshold (2x margin): {pass_bp:.1f} bp")
    if best_abs >= pass_bp:
        print(f"  SCREEN: SURVIVES ({best_abs:.2f} >= {pass_bp:.1f})")
    elif best_abs >= COST_BP_ROUNDTRIP:
        print(f"  SCREEN: MARGINAL ({COST_BP_ROUNDTRIP:.1f} <= {best_abs:.2f} < {pass_bp:.1f})")
    else:
        print(f"  SCREEN: FEE-DEAD ({best_abs:.2f} < {COST_BP_ROUNDTRIP:.1f})")
    print("=" * 64)

    # ---- HONEST TEST: committed-sign net + gate-4 by-year decomposition ----
    # Headline tradeable rule frozen pre-run: EXTREME_LOW, LONG into settlement,
    # exit at settlement. NET bp already has round-trip cost subtracted, sign
    # committed (no abs()). A real edge is net-positive AND not concentrated in
    # one regime/year.
    all_net = np.array([v for lst in byyear.values() for v in lst], dtype=float)
    print()
    print("=" * 64)
    print("HONEST TEST — rule: EXTREME_LOW / LONG-into-settlement / exit at settle")
    print(f"  net = pre_return - {COST_BP_ROUNDTRIP}bp roundtrip, sign COMMITTED (no abs)")
    tail_ok = True
    if len(all_net) >= 2:
        srt = np.sort(all_net)
        m = srt.mean(); t = tstat(srt); n = len(srt)
        wr = float((srt > 0).mean()); med = float(np.median(srt))
        print(f"  pooled: n={n}  mean_net={m:.2f}bp  median={med:.2f}bp  "
              f"t={t:.2f}  win_rate={wr:.1%}")
        # TAIL-DEPENDENCE: a real edge is not carried by a handful of violent
        # tail events. Drop the top 1% / 5% of trades and the mean must survive.
        k1 = max(1, int(n * 0.01)); k5 = max(1, int(n * 0.05))
        m1 = srt[:-k1].mean(); m5 = srt[:-k5].mean()
        tot = srt.sum()
        top_share = (srt[-k1:].sum() / tot) if tot != 0 else float("nan")
        print(f"  tail: drop-top-1%({k1})={m1:.2f}bp  drop-top-5%({k5})={m5:.2f}bp  "
              f"top-1%-share={top_share:.1%}")
        # FAIL the tail test if median is non-positive (typical trade loses) OR
        # the edge inverts when the top 5% is removed OR top-1% carries >50% PnL.
        tail_ok = (med > 0) and (m5 > 0) and (top_share < 0.50)
    print()
    print(f"  {'year':<6}{'n':>7}{'mean_net_bp':>14}{'t':>8}{'win%':>8}")
    print("  " + "-" * 41)
    pos_years = 0; read_years = 0
    for yr in sorted(byyear):
        a = np.asarray(byyear[yr], dtype=float)
        if len(a) == 0:
            continue
        m = a.mean(); t = tstat(a); wr = float((a > 0).mean())
        flag = ""
        if len(a) >= POWER_FLOOR:
            read_years += 1
            if m > 0:
                pos_years += 1
        else:
            flag = "  (low-n, ignore)"
        print(f"  {yr:<6}{len(a):>7}{m:>14.2f}{t:>8.2f}{wr:>7.1%}{flag}")
    print()
    print("=" * 64)
    print("VERDICT (candidate #11)")
    pooled_pos = len(all_net) >= 2 and all_net.mean() > 0
    if not pooled_pos:
        print("  FEE-DEAD: committed-sign pooled net <= 0. No engine mode.")
    elif not tail_ok:
        print("  TAIL-MIRAGE: pooled mean is carried by a few violent tail events "
              "(median<=0 OR edge inverts dropping top 5% OR top-1% > half the PnL). "
              "Per-trade this is a coin-flip + lottery ticket, un-tradeable under "
              "real slippage. No engine mode. (Crash-window mirage, tail-hidden.)")
    elif read_years >= 2 and pos_years == read_years:
        print(f"  CANDIDATE: net-positive AND positive in all {read_years} readable "
              f"years -> proceed to walk-forward / overfit / corr gates.")
    elif read_years >= 2 and pos_years >= max(2, read_years - 1):
        print(f"  MARGINAL: net-positive but {read_years-pos_years}/{read_years} "
              f"readable years negative -> regime-fragile; gate-4 borderline. "
              f"Decompose before building.")
    else:
        print(f"  REGIME ARTIFACT: net-positive pooled but only {pos_years}/{read_years} "
              f"readable years positive -> concentrated, fails gate 4 (crash-window "
              f"mirage, like every prior finding). No engine mode.")
    print("=" * 64)


if __name__ == "__main__":
    main()
