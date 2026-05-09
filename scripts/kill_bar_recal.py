#!/usr/bin/env python3
"""kill_bar_recal.py — apply the locked recalibration rule from
`results/kill_bar_recal_decision_rule_2026-05-07.md` to the kill bar.

Procedure:
  1. Run B=1,000 Monte Carlo paths under scenario=null, horizon=90d.
     Collect per-path raw statistic values (total_pnl, max_sym_pct,
     hodl_delta_cum, all monthly deltas, WR-when-eligible).
  2. Set new thresholds from the null distribution:
       PNL    : 10th percentile of total_pnl
       WR     : 10th percentile of WR (when n_trades ≥ 150)
       SYM    : 90th percentile of max_sym_pct
       HODL_C : 10th percentile of hodl_delta cumulative
       HODL_W : 10th percentile of single-window deltas (flat over paths)
  3. Re-run Monte Carlo at all 4 horizons × 4 scenarios with new thresholds.
  4. Apply locked acceptance bands at 90d verification:
       FP(null) ≤ 20%, TP(deg50) ≥ 50%, TP(dead) ≥ 80%
  5. Verdict tier (CLEAN / PARTIAL / MOSTLY_DISCARDED / NO_KILL_BAR).

Imports loaders + BTC fetch from kill_bar_calibration.py.
"""

from __future__ import annotations

import sys
from datetime import timedelta
from pathlib import Path

# Reuse data loaders.
sys.path.insert(0, str(Path(__file__).resolve().parent))
from bootstrap_ci import fmt_dollar  # noqa: E402
from kill_bar_calibration import (  # noqa: E402
    HODL_BENCHMARK_NOTIONAL,
    HORIZONS_DAYS,
    MIN_TRADES_FOR_WR,
    btc_close_lookup,
    fetch_btc_daily_closes,
    load_trades,
    transform_scenario,
)

# ── Locked recalibration parameters ──────────────────────────────────────────
TARGET_FP_RATE = 0.10               # threshold quantile target
B_PATHS = 1_000
CALIBRATION_HORIZON_DAYS = 90

# Locked acceptance bands (same as original calibration pre-reg).
BAND_FP_NULL_PCT = 20.0
BAND_TP_DEG50_PCT = 50.0
BAND_TP_DEAD_PCT = 80.0
BORDERLINE_TOLERANCE_PP = 5.0

# ── Sampling ─────────────────────────────────────────────────────────────────
import random  # noqa: E402

RNG = random.Random(20260507)


def quantile_of(sorted_values: list[float], q: float) -> float:
    if not sorted_values:
        return 0.0
    n = len(sorted_values)
    pos = q * (n - 1)
    lo = int(pos)
    hi = min(lo + 1, n - 1)
    frac = pos - lo
    return sorted_values[lo] * (1 - frac) + sorted_values[hi] * frac


def collect_path_stats(trades: list[dict], scenario: str, horizon_days: int,
                       btc_prices: dict, B: int, global_mean: float = 0.0,
                       collect_monthly: bool = False) -> dict:
    """Run B paths and collect raw per-path statistics."""
    earliest = trades[0]["ts"]
    latest = trades[-1]["ts"]
    span_days = (latest - earliest).total_seconds() / 86400.0
    max_offset = span_days - horizon_days
    if max_offset <= 0:
        return {}

    out: dict = {
        "total_pnl": [],
        "max_sym_pct": [],
        "hodl_delta_cum": [],
        "wr_when_eligible": [],
        "n_trades": [],
        "monthly_deltas_flat": [],   # flat list across all paths
        "monthly_deltas_per_path": [],  # list-of-lists for HODL_W eval
    }
    for _ in range(B):
        offset_days = RNG.uniform(0, max_offset)
        win_start = earliest + timedelta(days=offset_days)
        win_end = win_start + timedelta(days=horizon_days)
        wts = [t for t in trades if win_start <= t["ts"] < win_end]
        if not wts:
            continue
        pnls = transform_scenario([t["pnl_usd"] for t in wts], scenario, global_mean)
        n_trades = len(pnls)
        total_pnl = sum(pnls)
        wins = sum(1 for t, p in zip(wts, pnls) if t["outcome"] in ("TARGET", "PARTIAL"))
        wr_pct = (wins / n_trades * 100) if n_trades > 0 else 0
        sym_pnls: dict = {}
        for t, p in zip(wts, pnls):
            sym_pnls[t["symbol"]] = sym_pnls.get(t["symbol"], 0) + p
        abs_total = abs(total_pnl) if total_pnl != 0 else 1.0
        max_sym_pct = max((abs(s) / abs_total * 100 for s in sym_pnls.values()), default=0.0)

        ps = btc_close_lookup(btc_prices, win_start)
        pe = btc_close_lookup(btc_prices, win_end)
        if ps and pe and ps > 0:
            hodl_total = HODL_BENCHMARK_NOTIONAL * (pe / ps - 1.0)
            hodl_delta_cum = total_pnl - hodl_total
        else:
            hodl_delta_cum = 0

        monthly: list[float] = []
        cursor = win_start
        win_size = timedelta(days=30)
        while cursor + win_size <= win_end:
            we = cursor + win_size
            wp = sum(p for t, p in zip(wts, pnls) if cursor <= t["ts"] < we)
            cs = btc_close_lookup(btc_prices, cursor)
            ce = btc_close_lookup(btc_prices, we)
            if cs and ce and cs > 0:
                wh = HODL_BENCHMARK_NOTIONAL * (ce / cs - 1.0)
                monthly.append(wp - wh)
            cursor = we

        out["total_pnl"].append(total_pnl)
        out["max_sym_pct"].append(max_sym_pct)
        out["hodl_delta_cum"].append(hodl_delta_cum)
        out["n_trades"].append(n_trades)
        if n_trades >= MIN_TRADES_FOR_WR:
            out["wr_when_eligible"].append(wr_pct)
        if collect_monthly:
            out["monthly_deltas_flat"].extend(monthly)
            out["monthly_deltas_per_path"].append(monthly)

    return out


def evaluate_with_thresholds(trades: list[dict], scenario: str, horizon_days: int,
                              btc_prices: dict, B: int, thresholds: dict,
                              global_mean: float = 0.0) -> dict:
    """Re-run B paths and compute fire rates given recalibrated thresholds.

    thresholds dict keys: PNL, WR, SYM, HODL_C, HODL_W (None means DISCARDED)
    """
    earliest = trades[0]["ts"]
    latest = trades[-1]["ts"]
    span_days = (latest - earliest).total_seconds() / 86400.0
    max_offset = span_days - horizon_days
    if max_offset <= 0:
        return {}

    fires = {k: 0 for k in ("PNL", "WR", "SYM", "HODL_C", "HODL_W")}
    eligible_wr = 0
    rng = random.Random(20260508 + horizon_days * 31 + hash(scenario) % 997)

    for _ in range(B):
        offset_days = rng.uniform(0, max_offset)
        win_start = earliest + timedelta(days=offset_days)
        win_end = win_start + timedelta(days=horizon_days)
        wts = [t for t in trades if win_start <= t["ts"] < win_end]
        if not wts:
            continue
        pnls = transform_scenario([t["pnl_usd"] for t in wts], scenario, global_mean)
        n_trades = len(pnls)
        total_pnl = sum(pnls)
        wins = sum(1 for t, p in zip(wts, pnls) if t["outcome"] in ("TARGET", "PARTIAL"))
        wr_pct = (wins / n_trades * 100) if n_trades > 0 else 0
        sym_pnls: dict = {}
        for t, p in zip(wts, pnls):
            sym_pnls[t["symbol"]] = sym_pnls.get(t["symbol"], 0) + p
        abs_total = abs(total_pnl) if total_pnl != 0 else 1.0
        max_sym_pct = max((abs(s) / abs_total * 100 for s in sym_pnls.values()), default=0.0)

        ps = btc_close_lookup(btc_prices, win_start)
        pe = btc_close_lookup(btc_prices, win_end)
        if ps and pe and ps > 0:
            hodl_total = HODL_BENCHMARK_NOTIONAL * (pe / ps - 1.0)
            hodl_delta_cum = total_pnl - hodl_total
        else:
            hodl_delta_cum = 0

        monthly: list[float] = []
        cursor = win_start
        win_size = timedelta(days=30)
        while cursor + win_size <= win_end:
            we = cursor + win_size
            wp = sum(p for t, p in zip(wts, pnls) if cursor <= t["ts"] < we)
            cs = btc_close_lookup(btc_prices, cursor)
            ce = btc_close_lookup(btc_prices, we)
            if cs and ce and cs > 0:
                wh = HODL_BENCHMARK_NOTIONAL * (ce / cs - 1.0)
                monthly.append(wp - wh)
            cursor = we

        # Apply NEW thresholds. None means criterion was discarded.
        if thresholds.get("PNL") is not None and total_pnl < thresholds["PNL"]:
            fires["PNL"] += 1
        if thresholds.get("WR") is not None and n_trades >= MIN_TRADES_FOR_WR:
            eligible_wr += 1
            if wr_pct < thresholds["WR"]:
                fires["WR"] += 1
        if thresholds.get("SYM") is not None and max_sym_pct > thresholds["SYM"]:
            fires["SYM"] += 1
        if thresholds.get("HODL_C") is not None and hodl_delta_cum < thresholds["HODL_C"]:
            fires["HODL_C"] += 1
        if thresholds.get("HODL_W") is not None:
            for i in range(len(monthly) - 1):
                if monthly[i] < thresholds["HODL_W"] and monthly[i + 1] < thresholds["HODL_W"]:
                    fires["HODL_W"] += 1
                    break

    return {k: v / B * 100 for k, v in fires.items()}


def main() -> int:
    args = sys.argv[1:]
    if args and Path(args[0]).is_dir():
        journal_dir = Path(args[0])
    else:
        journal_dir = Path(__file__).resolve().parent.parent / "results" / "hod_journals" / "2026-05-07"

    print("KILL-BAR RECALIBRATION — kill_bar_recal_decision_rule_2026-05-07.md")
    print("=" * 80)
    print(f"journal dir: {journal_dir}")
    print(f"target FP rate: {TARGET_FP_RATE*100:.0f}%  paths: {B_PATHS}  horizon: {CALIBRATION_HORIZON_DAYS}d")
    print()

    print("Loading trades & BTC prices...")
    trades = load_trades(journal_dir)
    earliest = trades[0]["ts"]
    latest = trades[-1]["ts"]
    btc_prices = fetch_btc_daily_closes(earliest, latest)
    global_mean = sum(t["pnl_usd"] for t in trades) / len(trades)
    print(f"  {len(trades):,} trades, {earliest.date()} → {latest.date()}")
    print(f"  {len(btc_prices):,} daily BTC closes")
    print(f"  global per-trade mean (used for dead scenario): {fmt_dollar(global_mean)}")
    print()

    # Step 1: collect null distribution at 90d.
    print(f"Step 1: collecting null distribution at {CALIBRATION_HORIZON_DAYS}d, B={B_PATHS}...")
    null_stats = collect_path_stats(
        trades, "null", CALIBRATION_HORIZON_DAYS, btc_prices, B_PATHS,
        global_mean=global_mean, collect_monthly=True
    )
    print(f"  collected {len(null_stats['total_pnl'])} paths' raw statistics")
    print(f"  WR-eligible paths: {len(null_stats['wr_when_eligible'])}")
    print(f"  total monthly deltas: {len(null_stats['monthly_deltas_flat'])}")
    print()

    # Step 2: derive new thresholds from quantiles.
    print(f"Step 2: deriving new thresholds from quantiles (target FP = {TARGET_FP_RATE*100:.0f}%)")
    thresholds: dict = {}

    # PNL: 10th percentile of total_pnl
    pnl_sorted = sorted(null_stats["total_pnl"])
    thresholds["PNL"] = quantile_of(pnl_sorted, TARGET_FP_RATE)
    print(f"  PNL    threshold: {fmt_dollar(thresholds['PNL'])}  "
          f"(was: < $0)")

    # SYM: 90th percentile of max_sym_pct
    sym_sorted = sorted(null_stats["max_sym_pct"])
    thresholds["SYM"] = quantile_of(sym_sorted, 1.0 - TARGET_FP_RATE)
    print(f"  SYM    threshold: {thresholds['SYM']:.1f}%  "
          f"(was: > 40%)")

    # HODL_C: 10th percentile of cumulative
    hc_sorted = sorted(null_stats["hodl_delta_cum"])
    thresholds["HODL_C"] = quantile_of(hc_sorted, TARGET_FP_RATE)
    print(f"  HODL_C threshold: {fmt_dollar(thresholds['HODL_C'])}  "
          f"(was: < $0)")

    # HODL_W: 10th percentile of single-window null delta distribution
    md_sorted = sorted(null_stats["monthly_deltas_flat"])
    thresholds["HODL_W"] = quantile_of(md_sorted, TARGET_FP_RATE)
    print(f"  HODL_W threshold: {fmt_dollar(thresholds['HODL_W'])}/window  "
          f"(was: -$5,000/window)")

    # WR: 10th percentile of WR when eligible
    if len(null_stats["wr_when_eligible"]) >= 30:
        wr_sorted = sorted(null_stats["wr_when_eligible"])
        thresholds["WR"] = quantile_of(wr_sorted, TARGET_FP_RATE)
        print(f"  WR     threshold: {thresholds['WR']:.2f}%  "
              f"(was: < 14%)")
    else:
        thresholds["WR"] = None
        print(f"  WR     threshold: DISCARDED  (only {len(null_stats['wr_when_eligible'])} eligible paths at 90d — too few to set quantile)")
    print()

    # Step 3: verify with new thresholds across all scenarios × horizons.
    print("Step 3: verifying recalibrated thresholds across scenarios × horizons...")
    print()
    scenarios = ["null", "deg30", "deg50", "dead"]
    results: dict = {}
    for sc in scenarios:
        results[sc] = {}
        for h in HORIZONS_DAYS:
            r = evaluate_with_thresholds(trades, sc, h, btc_prices, B_PATHS, thresholds, global_mean)
            results[sc][h] = r
            print(f"  {sc:>5}  {h:>3}d  PNL={r['PNL']:>5.1f}%  WR={r['WR']:>5.1f}%  "
                  f"SYM={r['SYM']:>5.1f}%  HODL_C={r['HODL_C']:>5.1f}%  HODL_W={r['HODL_W']:>5.1f}%")
    print()

    # Step 4: apply locked acceptance bands at 90d verification.
    print("=" * 80)
    print("DECISION RULE EVALUATION (90d verification, locked bands):")
    print(f"  FP(null) ≤ {BAND_FP_NULL_PCT}%   TP(deg50) ≥ {BAND_TP_DEG50_PCT}%   TP(dead) ≥ {BAND_TP_DEAD_PCT}%")
    print()
    criteria = ["PNL", "WR", "SYM", "HODL_C", "HODL_W"]
    accepted: list[str] = []
    discarded: list[str] = []
    for c in criteria:
        if thresholds.get(c) is None:
            discarded.append(c)
            print(f"  {c:<8} DISCARDED (no quantile set — too few eligible paths)")
            continue
        fp = results["null"][90][c]
        tp_deg50 = results["deg50"][90][c]
        tp_dead = results["dead"][90][c]
        fp_ok = fp <= BAND_FP_NULL_PCT
        tp_deg50_ok = tp_deg50 >= BAND_TP_DEG50_PCT
        tp_dead_ok = tp_dead >= BAND_TP_DEAD_PCT
        ok = fp_ok and tp_deg50_ok and tp_dead_ok
        status = "✓ ACCEPTED" if ok else "✗ DISCARDED"
        print(f"  {c:<8} FP={fp:>5.1f}% {'✓' if fp_ok else '✗'}   "
              f"TP(d50)={tp_deg50:>5.1f}% {'✓' if tp_deg50_ok else '✗'}   "
              f"TP(dd)={tp_dead:>5.1f}% {'✓' if tp_dead_ok else '✗'}   {status}")
        if ok:
            accepted.append(c)
        else:
            discarded.append(c)

    print()
    n_accepted = len(accepted)
    if n_accepted == 5:
        verdict = "CLEAN_RECAL"
    elif n_accepted >= 3:
        verdict = "PARTIAL_RECAL"
    elif n_accepted >= 1:
        verdict = "MOSTLY_DISCARDED"
    else:
        verdict = "NO_KILL_BAR"

    print("=" * 80)
    print(f"VERDICT: {verdict}")
    print(f"  Accepted ({len(accepted)}): {', '.join(accepted) if accepted else '—'}")
    print(f"  Discarded ({len(discarded)}): {', '.join(discarded) if discarded else '—'}")
    print("=" * 80)
    print()
    print("Final recalibrated thresholds (for accepted criteria only):")
    for c in accepted:
        if c == "PNL":
            print(f"  KILL_PNL    : trigger if total NET < {fmt_dollar(thresholds['PNL'])}")
        elif c == "WR":
            print(f"  KILL_WR     : trigger if WR < {thresholds['WR']:.2f}% when n_trades ≥ {MIN_TRADES_FOR_WR}")
        elif c == "SYM":
            print(f"  KILL_SYM    : trigger if max-sym-pct > {thresholds['SYM']:.1f}%")
        elif c == "HODL_C":
            print(f"  KILL_HODL_C : trigger if HODL Δ cumulative < {fmt_dollar(thresholds['HODL_C'])}")
        elif c == "HODL_W":
            print(f"  KILL_HODL_W : trigger if 2 consecutive 30d windows each < {fmt_dollar(thresholds['HODL_W'])}")
    if discarded:
        print()
        print("Discarded criteria do NOT contribute to the kill bar.")

    return 0


if __name__ == "__main__":
    sys.exit(main())
