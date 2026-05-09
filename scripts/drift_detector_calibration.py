#!/usr/bin/env python3
"""drift_detector_calibration.py — Monte Carlo calibration of the drift
detector (scripts/live_vs_backtest_drift.py) at a fixed grid of
(α_family, N_LIVE) operating points, applying the locked decision rule
from `results/drift_detector_calibration_decision_rule_2026-05-07.md`.

Procedure:
  For each (α_family, N_LIVE) × scenario × B=1000 paths:
    1. Sample N_LIVE trades from the historical pool (with replacement)
    2. Apply scenario transform to PnL only (MFE/MAE preserved)
    3. Run drift detector's 5 Welch t-tests + 1 WR z-test
    4. Record fire (any test p < α_family/6)

  Then apply the locked selection rule:
    - For each (α, N_LIVE), report FP(null), TP(deg50), TP(dead)
    - Select (α*, N*) maximizing TP(dead) subject to FP(null) ≤ 20%
    - Verdict: VIABLE / PARTIAL / TOO_NOISY per acceptance bands
"""

from __future__ import annotations

import json
import math
import random
import sys
from dataclasses import dataclass
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from bootstrap_ci import fmt_dollar  # noqa: E402

# ── Locked grid ──────────────────────────────────────────────────────────────
ALPHA_FAMILY_GRID = [0.05, 0.01, 0.005, 0.001]
N_LIVE_GRID = [50, 100, 150, 200, 300]
B_PATHS = 1_000

# Acceptance bands (locked, same as kill bar)
BAND_FP_NULL_PCT = 20.0
BAND_TP_DEG50_PCT = 50.0
BAND_TP_DEAD_PCT = 80.0

JOURNAL_DIR = Path(__file__).resolve().parent.parent / "results" / "hod_journals" / "2026-05-07-mfe"

RNG = random.Random(20260507)


@dataclass
class Trade:
    pnl_usd: float
    mfe_r: float
    mae_r: float


def load_trades_with_mfe(journal_dir: Path) -> list[Trade]:
    out: list[Trade] = []
    for jf in sorted(journal_dir.glob("*-*.jsonl")):
        open_event = None
        for line in jf.read_text().splitlines():
            if not line:
                continue
            ev = json.loads(line)
            if ev["event"] == "open":
                open_event = ev
            elif ev["event"] == "close" and open_event is not None:
                if ev.get("outcome") == "PARTIAL":
                    open_event = None
                    continue
                out.append(Trade(
                    pnl_usd=ev.get("pnl_usd", 0.0),
                    mfe_r=ev.get("mfe_r", 0.0),
                    mae_r=ev.get("mae_r", 0.0),
                ))
                open_event = None
    return out


def mean_var(xs: list[float]) -> tuple[float, float]:
    n = len(xs)
    if n < 2:
        return (sum(xs) / n if n else 0.0, 0.0)
    m = sum(xs) / n
    v = sum((x - m) ** 2 for x in xs) / (n - 1)
    return (m, v)


def welch_p(a_mean: float, a_var: float, n_a: int,
            b_mean: float, b_var: float, n_b: int) -> float:
    """Two-sided p-value for Welch's t-test, normal-approx (matches drift detector)."""
    if n_a < 2 or n_b < 2:
        return 1.0
    se = math.sqrt(a_var / n_a + b_var / n_b)
    if se == 0:
        return 1.0
    t = (a_mean - b_mean) / se
    return 2 * (1 - 0.5 * (1 + math.erf(abs(t) / math.sqrt(2))))


def proportion_z_p(p1: float, n1: int, p2: float, n2: int) -> float:
    if n1 == 0 or n2 == 0:
        return 1.0
    p_pool = (p1 * n1 + p2 * n2) / (n1 + n2)
    if p_pool in (0, 1):
        return 1.0
    se = math.sqrt(p_pool * (1 - p_pool) * (1 / n1 + 1 / n2))
    if se == 0:
        return 1.0
    z = (p1 - p2) / se
    return 2 * (1 - 0.5 * (1 + math.erf(abs(z) / math.sqrt(2))))


def transform_pnl(pnls: list[float], scenario: str, global_mean: float) -> list[float]:
    if scenario == "null":
        return list(pnls)
    if scenario == "deg30":
        return [p * 0.7 for p in pnls]
    if scenario == "deg50":
        return [p * 0.5 for p in pnls]
    if scenario == "dead":
        return [p - global_mean for p in pnls]
    raise ValueError(scenario)


def precompute_backtest_stats(pool: list[Trade]) -> dict:
    """Precompute means and variances of all backtest metrics — these are
    fixed for the duration of the run, so we compute them once."""
    pnls = [t.pnl_usd for t in pool]
    mfes = [t.mfe_r for t in pool]
    maes = [t.mae_r for t in pool]
    wins = [t.pnl_usd for t in pool if t.pnl_usd > 0]
    losses = [-t.pnl_usd for t in pool if t.pnl_usd < 0]
    pnl_m, pnl_v = mean_var(pnls)
    mfe_m, mfe_v = mean_var(mfes)
    mae_m, mae_v = mean_var(maes)
    win_m, win_v = mean_var(wins)
    loss_m, loss_v = mean_var(losses)
    return {
        "pnl":      (pnl_m, pnl_v, len(pnls)),
        "mfe":      (mfe_m, mfe_v, len(mfes)),
        "mae":      (mae_m, mae_v, len(maes)),
        "win_pnl":  (win_m, win_v, len(wins)),
        "loss_abs": (loss_m, loss_v, len(losses)),
        "wr":       len(wins) / len(pnls),
        "n":        len(pnls),
    }


def evaluate_path(pool: list[Trade], n_live: int, scenario: str,
                  global_mean: float, bt_stats: dict, alpha_family: float) -> bool:
    """Sample N_LIVE trades, apply scenario, run all 6 tests, return fire bool."""
    sample = [pool[RNG.randrange(len(pool))] for _ in range(n_live)]
    pnls = transform_pnl([t.pnl_usd for t in sample], scenario, global_mean)
    mfes = [t.mfe_r for t in sample]
    maes = [t.mae_r for t in sample]
    wins = [p for p in pnls if p > 0]
    losses = [-p for p in pnls if p < 0]

    pnl_m, pnl_v = mean_var(pnls)
    mfe_m, mfe_v = mean_var(mfes)
    mae_m, mae_v = mean_var(maes)
    win_m, win_v = mean_var(wins)
    loss_m, loss_v = mean_var(losses)

    bt_pnl = bt_stats["pnl"]
    bt_mfe = bt_stats["mfe"]
    bt_mae = bt_stats["mae"]
    bt_win = bt_stats["win_pnl"]
    bt_loss = bt_stats["loss_abs"]
    bt_wr = bt_stats["wr"]
    bt_n = bt_stats["n"]

    alpha_per_test = alpha_family / 6.0

    p1 = welch_p(pnl_m, pnl_v, n_live,        bt_pnl[0], bt_pnl[1], bt_pnl[2])
    if p1 < alpha_per_test:
        return True
    p2 = welch_p(mfe_m, mfe_v, n_live,        bt_mfe[0], bt_mfe[1], bt_mfe[2])
    if p2 < alpha_per_test:
        return True
    p3 = welch_p(mae_m, mae_v, n_live,        bt_mae[0], bt_mae[1], bt_mae[2])
    if p3 < alpha_per_test:
        return True
    p4 = welch_p(win_m, win_v, max(2, len(wins)),     bt_win[0], bt_win[1], bt_win[2])
    if p4 < alpha_per_test:
        return True
    p5 = welch_p(loss_m, loss_v, max(2, len(losses)), bt_loss[0], bt_loss[1], bt_loss[2])
    if p5 < alpha_per_test:
        return True
    wr_live = len(wins) / n_live if n_live > 0 else 0.0
    p6 = proportion_z_p(wr_live, n_live, bt_wr, bt_n)
    if p6 < alpha_per_test:
        return True
    return False


def run_calibration(pool: list[Trade], global_mean: float, bt_stats: dict) -> dict:
    """Returns nested dict: {alpha: {N_LIVE: {scenario: fire_rate_pct}}}"""
    results: dict = {}
    scenarios = ["null", "deg30", "deg50", "dead"]
    total_runs = len(ALPHA_FAMILY_GRID) * len(N_LIVE_GRID) * len(scenarios)
    done = 0
    for alpha in ALPHA_FAMILY_GRID:
        results[alpha] = {}
        for n_live in N_LIVE_GRID:
            results[alpha][n_live] = {}
            for sc in scenarios:
                fires = 0
                for _ in range(B_PATHS):
                    if evaluate_path(pool, n_live, sc, global_mean, bt_stats, alpha):
                        fires += 1
                results[alpha][n_live][sc] = fires / B_PATHS * 100
                done += 1
            print(f"  α={alpha:.3f}  N={n_live:>3}  "
                  f"FP={results[alpha][n_live]['null']:>5.1f}%  "
                  f"TP(deg50)={results[alpha][n_live]['deg50']:>5.1f}%  "
                  f"TP(dead)={results[alpha][n_live]['dead']:>5.1f}%  "
                  f"({done}/{total_runs})")
    return results


def main() -> int:
    print("DRIFT DETECTOR CALIBRATION — drift_detector_calibration_decision_rule_2026-05-07.md")
    print("=" * 80)
    print(f"journal:  {JOURNAL_DIR}")
    print(f"alpha grid:  {ALPHA_FAMILY_GRID}")
    print(f"N_LIVE grid: {N_LIVE_GRID}")
    print(f"B paths:     {B_PATHS} per (α, N, scenario)")
    print()

    print("Loading trades with MFE/MAE...")
    pool = load_trades_with_mfe(JOURNAL_DIR)
    if len(pool) < 100:
        print(f"  insufficient trades: {len(pool)}")
        return 1
    global_mean = sum(t.pnl_usd for t in pool) / len(pool)
    print(f"  {len(pool):,} trades; per-trade global mean = {fmt_dollar(global_mean)}")
    print()

    print("Precomputing backtest summary statistics...")
    bt_stats = precompute_backtest_stats(pool)
    print(f"  WR_backtest = {bt_stats['wr']*100:.2f}%")
    print(f"  pnl_per_trade mean = {fmt_dollar(bt_stats['pnl'][0])}, var SD = {math.sqrt(bt_stats['pnl'][1]):,.0f}")
    print()

    print("Running calibration grid...")
    results = run_calibration(pool, global_mean, bt_stats)
    print()

    # Apply locked selection rule
    candidates = []
    for alpha in ALPHA_FAMILY_GRID:
        for n_live in N_LIVE_GRID:
            r = results[alpha][n_live]
            if r["null"] <= BAND_FP_NULL_PCT:
                candidates.append((alpha, n_live, r["null"], r["deg50"], r["dead"]))

    print("=" * 80)
    print("SELECTION RULE (locked):")
    print(f"  Maximize TP(dead) subject to FP(null) ≤ {BAND_FP_NULL_PCT}%")
    print("  Tie-break: smaller N_LIVE.")
    print()

    if not candidates:
        verdict = "DETECTOR_TOO_NOISY"
        selected = None
        print("  No (α, N_LIVE) in the grid satisfies FP(null) ≤ 20%.")
    else:
        candidates.sort(key=lambda x: (-x[4], x[1]))  # max TP(dead) then min N
        selected = candidates[0]
        alpha_star, n_star, fp_star, tp_d50_star, tp_dead_star = selected
        print(f"  Selected:  α* = {alpha_star},  N_LIVE* = {n_star}")
        print(f"  At selected: FP={fp_star:.1f}%   TP(deg50)={tp_d50_star:.1f}%   TP(dead)={tp_dead_star:.1f}%")
        if (fp_star <= BAND_FP_NULL_PCT and
                tp_d50_star >= BAND_TP_DEG50_PCT and
                tp_dead_star >= BAND_TP_DEAD_PCT):
            verdict = "DETECTOR_VIABLE"
        else:
            verdict = "DETECTOR_PARTIAL"

    print()
    print("=" * 80)
    print(f"VERDICT: {verdict}")
    print("=" * 80)

    print()
    print("Full grid (all 4 scenarios per cell):")
    print()
    for alpha in ALPHA_FAMILY_GRID:
        print(f"  α_family = {alpha}  (per-test α = {alpha/6:.5f})")
        print(f"    {'N':>4}  {'null':>6}  {'deg30':>6}  {'deg50':>6}  {'dead':>6}")
        print(f"    {'-'*4}  {'-'*6}  {'-'*6}  {'-'*6}  {'-'*6}")
        for n_live in N_LIVE_GRID:
            r = results[alpha][n_live]
            print(f"    {n_live:>4}  {r['null']:>5.1f}%  {r['deg30']:>5.1f}%  "
                  f"{r['deg50']:>5.1f}%  {r['dead']:>5.1f}%")
        print()

    return 0


if __name__ == "__main__":
    sys.exit(main())
