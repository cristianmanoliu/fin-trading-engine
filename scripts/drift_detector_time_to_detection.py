#!/usr/bin/env python3
"""drift_detector_time_to_detection.py — apply the locked decision rule
from `results/drift_detector_time_to_detection_decision_rule_2026-05-08.md`.

Day-by-day Monte Carlo simulation of forward-paper trade arrivals
(Poisson at fleet rate 1.18/day), running the drift detector daily once
N≥30 trades accumulated. Records first-fire-day per path.

Reuses the trade-pool loader and helpers from drift_detector_calibration.py.
"""

from __future__ import annotations

import math
import random
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from bootstrap_ci import fmt_dollar  # noqa: E402
from drift_detector_calibration import (  # noqa: E402
    Trade,
    load_trades_with_mfe,
    mean_var,
    precompute_backtest_stats,
    proportion_z_p,
    transform_pnl,
    welch_p,
)

# ── Locked parameters ────────────────────────────────────────────────────────
ALPHA_FAMILY = 0.001       # locked from drift_detector_calibration_verdict
N_MIN = 30                 # locked from detector code
TRADES_PER_DAY = 1.18      # locked: historical fleet rate (paired with
                           # forward_paper_timeline.py::HISTORICAL_FLEET_RATE
                           # via scripts/test_criterion_coverage.py pin —
                           # both sites MUST agree, pre-reg change required.
                           # Changing here without there would invalidate
                           # the locked α=0.001 calibration AND produce
                           # inconsistent operator-facing projections.)
HORIZON_DAYS = 365
B_PATHS = 1_000

# Acceptance bands (locked from pre-reg)
BAND_FP_365 = 20.0         # cumulative FP under null at 365d ≤ 20%
BAND_MEDIAN_DEAD_DAYS = 60 # median detection day under "dead" ≤ 60

JOURNAL_DIR = Path(__file__).resolve().parent.parent / "results" / "hod_journals" / "2026-05-07-mfe"

RNG = random.Random(20260508)


def simulate_path(pool: list[Trade], scenario: str, global_mean: float,
                  bt_stats: dict, rng: random.Random) -> int:
    """Simulate one path. Return first fire day, or HORIZON_DAYS+1 if censored."""
    alpha_per_test = ALPHA_FAMILY / 6.0
    bt_pnl = bt_stats["pnl"]
    bt_mfe = bt_stats["mfe"]
    bt_mae = bt_stats["mae"]
    bt_win = bt_stats["win_pnl"]
    bt_loss = bt_stats["loss_abs"]
    bt_wr = bt_stats["wr"]
    bt_n = bt_stats["n"]

    # Accumulating sample.
    sample_pnls: list[float] = []
    sample_mfes: list[float] = []
    sample_maes: list[float] = []

    for day in range(1, HORIZON_DAYS + 1):
        # Poisson trade arrivals.
        n_today = poisson_sample(TRADES_PER_DAY, rng)
        for _ in range(n_today):
            t = pool[rng.randrange(len(pool))]
            sample_mfes.append(t.mfe_r)
            sample_maes.append(t.mae_r)
            sample_pnls.append(t.pnl_usd)
        if len(sample_pnls) < N_MIN:
            continue

        # Apply scenario transform to PnL only (MFE/MAE pass through).
        pnls = transform_pnl(sample_pnls, scenario, global_mean)
        wins = [p for p in pnls if p > 0]
        losses = [-p for p in pnls if p < 0]
        n_live = len(pnls)

        pnl_m, pnl_v = mean_var(pnls)
        mfe_m, mfe_v = mean_var(sample_mfes)
        mae_m, mae_v = mean_var(sample_maes)
        win_m, win_v = mean_var(wins)
        loss_m, loss_v = mean_var(losses)

        # 6 tests; fire if any single one rejects at α/6.
        p1 = welch_p(pnl_m, pnl_v, n_live, *bt_pnl)
        if p1 < alpha_per_test:
            return day
        p2 = welch_p(mfe_m, mfe_v, n_live, *bt_mfe)
        if p2 < alpha_per_test:
            return day
        p3 = welch_p(mae_m, mae_v, n_live, *bt_mae)
        if p3 < alpha_per_test:
            return day
        if len(wins) >= 2:
            p4 = welch_p(win_m, win_v, len(wins), *bt_win)
            if p4 < alpha_per_test:
                return day
        if len(losses) >= 2:
            p5 = welch_p(loss_m, loss_v, len(losses), *bt_loss)
            if p5 < alpha_per_test:
                return day
        wr_live = len(wins) / n_live
        p6 = proportion_z_p(wr_live, n_live, bt_wr, bt_n)
        if p6 < alpha_per_test:
            return day

    return HORIZON_DAYS + 1  # censored


def poisson_sample(lam: float, rng: random.Random) -> int:
    """Knuth's Poisson sampler. Adequate for lam ≈ 1."""
    L = math.exp(-lam)
    k, p = 0, 1.0
    while p > L:
        k += 1
        p *= rng.random()
    return k - 1


def percentile(sorted_vals: list[int], q: float) -> float:
    if not sorted_vals:
        return float("nan")
    n = len(sorted_vals)
    pos = q * (n - 1)
    lo = int(pos)
    hi = min(lo + 1, n - 1)
    frac = pos - lo
    return sorted_vals[lo] * (1 - frac) + sorted_vals[hi] * frac


def fire_rate_at_day(fire_days: list[int], day: int) -> float:
    """Proportion of paths that fired by `day` (inclusive)."""
    return sum(1 for d in fire_days if d <= day) / len(fire_days) * 100


def main() -> int:
    print("DRIFT DETECTOR TIME-TO-DETECTION (locked)")
    print("=" * 80)
    print("Pre-reg:    drift_detector_time_to_detection_decision_rule_2026-05-08.md")
    print(f"α_family:   {ALPHA_FAMILY} (per-test α = {ALPHA_FAMILY/6:.5f})")
    print(f"N_MIN:      {N_MIN}")
    print(f"Trade rate: {TRADES_PER_DAY}/day (historical fleet)")
    print(f"Horizon:    {HORIZON_DAYS} days   B paths: {B_PATHS}")
    print()

    print("Loading data...")
    pool = load_trades_with_mfe(JOURNAL_DIR)
    if len(pool) < 100:
        print(f"  insufficient pool: {len(pool)}")
        return 1
    global_mean = sum(t.pnl_usd for t in pool) / len(pool)
    bt_stats = precompute_backtest_stats(pool)
    print(f"  pool: {len(pool):,} trades; per-trade global mean = {fmt_dollar(global_mean)}")
    print()

    scenarios = ["null", "deg30", "deg50", "dead"]
    results: dict = {}
    for sc in scenarios:
        sub_rng = random.Random(RNG.randrange(1 << 30))
        fires = []
        for _ in range(B_PATHS):
            fires.append(simulate_path(pool, sc, global_mean, bt_stats, sub_rng))
        results[sc] = fires
        # Quick progress line
        n_fired = sum(1 for d in fires if d <= HORIZON_DAYS)
        median_d = sorted(d for d in fires if d <= HORIZON_DAYS)
        med_str = f"{percentile(median_d, 0.5):.0f}d" if median_d else "—"
        print(f"  {sc:>5}: {n_fired}/{B_PATHS} fired by day {HORIZON_DAYS}, median={med_str}")
    print()

    # Cumulative FP/TP at milestone days.
    print("Cumulative fire rate (% of paths that have fired by day X):")
    print()
    print(f"  {'Scenario':<8} {'30d':>6} {'60d':>6} {'90d':>6} {'180d':>6} {'365d':>6}")
    print(f"  {'-'*8} {'-'*6} {'-'*6} {'-'*6} {'-'*6} {'-'*6}")
    for sc in scenarios:
        fires = results[sc]
        print(f"  {sc:<8} {fire_rate_at_day(fires, 30):>5.1f}% "
              f"{fire_rate_at_day(fires, 60):>5.1f}% "
              f"{fire_rate_at_day(fires, 90):>5.1f}% "
              f"{fire_rate_at_day(fires, 180):>5.1f}% "
              f"{fire_rate_at_day(fires, 365):>5.1f}%")
    print()

    # Detection-day distribution per scenario.
    print("Detection-day distribution (paths that fired within 365d):")
    print()
    print(f"  {'Scenario':<8} {'P25':>6} {'P50':>6} {'P75':>6} {'P90':>6} {'censored':>9}")
    print(f"  {'-'*8} {'-'*6} {'-'*6} {'-'*6} {'-'*6} {'-'*9}")
    for sc in scenarios:
        fired = sorted(d for d in results[sc] if d <= HORIZON_DAYS)
        n_cens = sum(1 for d in results[sc] if d > HORIZON_DAYS)
        if fired:
            print(f"  {sc:<8} {percentile(fired, 0.25):>5.0f}d "
                  f"{percentile(fired, 0.50):>5.0f}d "
                  f"{percentile(fired, 0.75):>5.0f}d "
                  f"{percentile(fired, 0.90):>5.0f}d {n_cens:>4}/{B_PATHS}")
        else:
            print(f"  {sc:<8} {'—':>6} {'—':>6} {'—':>6} {'—':>6} {n_cens:>4}/{B_PATHS}")
    print()

    # Apply locked rule.
    print("=" * 80)
    print("DECISION RULE EVALUATION (locked):")
    print()
    fp_365 = fire_rate_at_day(results["null"], 365)
    fired_dead = sorted(d for d in results["dead"] if d <= HORIZON_DAYS)
    median_dead = percentile(fired_dead, 0.5) if fired_dead else float("inf")

    fp_ok = fp_365 <= BAND_FP_365
    tp_ok = median_dead <= BAND_MEDIAN_DEAD_DAYS

    print(f"  FP band: cumulative FP(null, 365d) = {fp_365:.1f}% "
          f"(need ≤ {BAND_FP_365}%)  {'✓' if fp_ok else '✗'}")
    print(f"  TP band: median detection(dead) = {median_dead:.1f}d "
          f"(need ≤ {BAND_MEDIAN_DEAD_DAYS}d)  {'✓' if tp_ok else '✗'}")

    if fp_ok and tp_ok:
        verdict = "DEPLOYABLE_AS_IS"
        diag = ""
    else:
        verdict = "NEEDS_TUNING"
        flags = []
        if not fp_ok:
            flags.append("too-noisy-null")
        if not tp_ok:
            flags.append("too-slow-dead")
        diag = f" — {', '.join(flags)}"

    print()
    print("=" * 80)
    print(f"VERDICT: {verdict}{diag}")
    print("=" * 80)

    return 0


if __name__ == "__main__":
    sys.exit(main())
