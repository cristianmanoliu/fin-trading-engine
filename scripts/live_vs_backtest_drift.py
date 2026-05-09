#!/usr/bin/env python3
"""
live_vs_backtest_drift.py — alert when realized live-paper trade metrics
diverge from the backtest empirical distribution.

This is the **decision-grade kill mechanism** for forward-paper. The
threshold-based criteria in scripts/forward_paper_status.sh are
mis-calibrated (NEEDS_RECALIBRATION + NO_KILL_BAR at 90d due to
distribution overlap — see results/kill_bar_recal_verdict_2026-05-07.md)
and serve as advisory-only. This detector achieved DETECTOR_VIABLE in
its pre-registered calibration (results/drift_detector_calibration_verdict_2026-05-07.md):
TP(deg30)=TP(deg50)=TP(dead)=100% across all operating points.

Operating point set per the calibration:
  ALPHA = 0.001 (recommended; FP=12.1% at N=50, TP=100% all scenarios)
  N_MIN = 30   (the original safety floor; we ramp to N=50 in practice
                where the calibration was anchored)

The script runs Welch's t-test (mean) and proportion z-test (WR) per
metric, applies Bonferroni correction across the metric set, and emits a
severity-graded summary. Designed to be run periodically as forward-paper
data accumulates.

Inputs:
  Backtest:  results/hod_journals/2026-05-07-mfe/   (cached locally; built
             by scripts/hod_journals.sh in B1/HG1 work)
  Live:      /var/log/paper-live/journal/<symbol>-<month>.jsonl
             (fetched via ssh from the VPS by default, or read locally
             when invoked with --live-source local)

Usage:
  python3 scripts/live_vs_backtest_drift.py
  python3 scripts/live_vs_backtest_drift.py --vps root@host
  python3 scripts/live_vs_backtest_drift.py --live-source local --live-dir ./logs/journal

Exit code: 0 = within tolerance, 1 = drift detected (Bonferroni-significant
on ≥1 metric), 2 = insufficient data.
"""
from __future__ import annotations

import argparse
import json
import math
import subprocess
import sys
import tempfile
from dataclasses import dataclass
from pathlib import Path

# Locked thresholds (per drift_detector_calibration_verdict_2026-05-07.md).
N_MIN = 30                  # minimum live trades for a meaningful comparison
ALPHA = 0.001               # family-wise α — calibration-recommended operating point
SHADOW_SUBDIRS = ("",)      # only the live strategy's journal dir; shadows handled separately

# Sanity floor for the backtest reference distribution. The locked
# reference (results/hod_journals/2026-05-07-mfe/) has ~2,210 trades.
# A reference smaller than this is corrupted/wiped — refuse to compare
# rather than silently emit a CLEAN verdict against an empty distribution.
# Closes the catastrophic fail-open where a missing/wiped backtest dir
# would let the decision-grade kill mechanism return exit 0 forever.
BACKTEST_MIN_TRADES = 500


@dataclass
class Trade:
    symbol: str
    side: str
    pnl_usd: float
    outcome: str
    mfe_r: float
    mae_r: float


# ── Loaders ──────────────────────────────────────────────────────────────────


def load_local_trades(journal_dir: Path) -> list[Trade]:
    """Pair open/close events in stream order. Same logic as mfe_mae_analysis."""
    trades: list[Trade] = []
    for jf in sorted(journal_dir.glob("*-*.jsonl")):
        symbol = jf.stem.split("-")[0]
        open_ev = None
        for line in jf.read_text().splitlines():
            if not line:
                continue
            ev = json.loads(line)
            if ev["event"] == "open":
                open_ev = ev
            elif ev["event"] == "close" and open_ev is not None:
                if ev.get("outcome") == "PARTIAL":
                    open_ev = None
                    continue
                trades.append(Trade(
                    symbol=symbol,
                    side=open_ev["side"],
                    pnl_usd=ev.get("pnl_usd", 0.0),
                    outcome=ev.get("outcome", "?"),
                    mfe_r=ev.get("mfe_r", 0.0),
                    mae_r=ev.get("mae_r", 0.0),
                ))
                open_ev = None
    return trades


def load_remote_trades(vps: str, remote_dir: str) -> list[Trade]:
    """Tar+stream remote journal files via ssh, parse locally. No persistence."""
    cmd = f'ssh {vps} "tar -cf - -C {remote_dir} . 2>/dev/null"'
    with tempfile.TemporaryDirectory() as tmp:
        result = subprocess.run(cmd, shell=True, check=True, capture_output=True)
        # Untar payload to tmp.
        subprocess.run(["tar", "-xf", "-", "-C", tmp], input=result.stdout, check=True)
        return load_local_trades(Path(tmp))


# ── Statistical machinery ────────────────────────────────────────────────────


def mean_var(xs: list[float]) -> tuple[float, float]:
    n = len(xs)
    if n < 2:
        return (sum(xs) / n if n else 0.0, 0.0)
    m = sum(xs) / n
    v = sum((x - m) ** 2 for x in xs) / (n - 1)
    return (m, v)


def welch_t(a: list[float], b: list[float]) -> tuple[float, float]:
    """Returns (t-stat, two-sided p-value). Normal-approx p (df typically large)."""
    if len(a) < 2 or len(b) < 2:
        return (0.0, 1.0)
    ma, va = mean_var(a)
    mb, vb = mean_var(b)
    se = math.sqrt(va / len(a) + vb / len(b))
    if se == 0:
        return (0.0, 1.0)
    t = (ma - mb) / se
    p = 2 * (1 - 0.5 * (1 + math.erf(abs(t) / math.sqrt(2))))
    return (t, p)


def proportion_z(p1: float, n1: int, p2: float, n2: int) -> tuple[float, float]:
    """Two-proportion z-test, returns (z, two-sided p)."""
    if n1 == 0 or n2 == 0:
        return (0.0, 1.0)
    p_pool = (p1 * n1 + p2 * n2) / (n1 + n2)
    if p_pool in (0, 1):
        return (0.0, 1.0)
    se = math.sqrt(p_pool * (1 - p_pool) * (1 / n1 + 1 / n2))
    if se == 0:
        return (0.0, 1.0)
    z = (p1 - p2) / se
    p = 2 * (1 - 0.5 * (1 + math.erf(abs(z) / math.sqrt(2))))
    return (z, p)


# ── Metric definitions ───────────────────────────────────────────────────────


def metric_pnl_per_trade(trades: list[Trade]) -> list[float]:
    return [t.pnl_usd for t in trades]


def metric_mfe_r(trades: list[Trade]) -> list[float]:
    return [t.mfe_r for t in trades]


def metric_mae_r(trades: list[Trade]) -> list[float]:
    return [t.mae_r for t in trades]


def metric_loss_only_pnl(trades: list[Trade]) -> list[float]:
    return [-t.pnl_usd for t in trades if t.pnl_usd < 0]


def metric_win_only_pnl(trades: list[Trade]) -> list[float]:
    return [t.pnl_usd for t in trades if t.pnl_usd > 0]


CONTINUOUS_METRICS = [
    ("pnl_per_trade",   metric_pnl_per_trade,   "$"),
    ("mfe_r",           metric_mfe_r,           "R"),
    ("mae_r",           metric_mae_r,           "R"),
    ("loss_pnl_abs",    metric_loss_only_pnl,   "$"),
    ("win_pnl",         metric_win_only_pnl,    "$"),
]


# ── Main ─────────────────────────────────────────────────────────────────────


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--backtest-dir", default="results/hod_journals/2026-05-07-mfe",
                    help="Local backtest journal directory (cached).")
    ap.add_argument("--live-source", choices=("vps", "local"), default="vps",
                    help="Where to read live journals from. Default: vps.")
    ap.add_argument("--vps", default="root@178.105.24.230",
                    help="VPS target when --live-source=vps")
    ap.add_argument("--live-dir", default="/var/log/paper-live/journal",
                    help="Live journal dir (remote path if vps, local path if local)")
    ap.add_argument("--n-min", type=int, default=N_MIN,
                    help="Minimum live trades required for the comparison to fire")
    args = ap.parse_args()

    backtest_dir = Path(args.backtest_dir)
    # Exit 3 (ERROR) for environment/config failures — distinct from exit 2
    # (INSUFFICIENT_DATA, normal early-deployment state). The wrapper
    # (run_drift_check.sh) maps exit 3 to a Telegram WARN; exit 2 stays
    # silent. Conflating them would silence config errors that need attention.
    if not backtest_dir.is_dir():
        print(f"ERROR: backtest dir not found: {backtest_dir}", file=sys.stderr)
        return 3

    print("Live-vs-backtest distribution drift detector")
    print("=" * 80)
    print(f"backtest:   {backtest_dir}")
    print(f"live:       {args.live_source} → {args.vps if args.live_source == 'vps' else ''}:{args.live_dir}")
    print()

    # Load.
    backtest = load_local_trades(backtest_dir)
    # Backtest-reference sanity floor. A wiped or partially-corrupt
    # backtest dir would silently produce "all metrics insufficient →
    # no drift detected → exit 0 CLEAN" — the decision-grade kill
    # mechanism returning green forever despite having no reference.
    if len(backtest) < BACKTEST_MIN_TRADES:
        print(f"ERROR: backtest reference has only {len(backtest)} trades "
              f"(< floor {BACKTEST_MIN_TRADES}). Reference may be corrupt or "
              f"partially populated. Refusing to compare.", file=sys.stderr)
        return 3

    if args.live_source == "vps":
        try:
            live = load_remote_trades(args.vps, args.live_dir)
        except subprocess.CalledProcessError as e:
            print(f"ERROR: ssh fetch failed: {e}", file=sys.stderr)
            return 3
    else:
        live_dir = Path(args.live_dir)
        if not live_dir.is_dir():
            print(f"ERROR: live dir not found: {live_dir}", file=sys.stderr)
            return 3
        live = load_local_trades(live_dir)

    print(f"backtest trades: {len(backtest):,}")
    print(f"live trades:     {len(live):,}")
    print()

    # Insufficient-data gate.
    if len(live) < args.n_min:
        print(f"⏸  INSUFFICIENT DATA — live n={len(live)} < n_min={args.n_min}.")
        print("   Per the locked threshold, drift comparison is suppressed until")
        print("   live trades accumulate. Re-run after each batch of new trades.")
        print()
        if len(live) > 0:
            print("   For monitoring purposes only (not decision-grade):")
            for name, fn, unit in CONTINUOUS_METRICS:
                xs = fn(live)
                if xs:
                    m, _ = mean_var(xs)
                    print(f"     {name:<16}  live mean = {m:>+12,.2f} {unit}  (n={len(xs)})")
            wins = sum(1 for t in live if t.pnl_usd > 0)
            wr = wins / len(live) * 100 if live else 0
            print(f"     {'WR':<16}  live = {wr:>5.1f}%  ({wins}/{len(live)})")
        return 2

    # Bonferroni: 5 continuous + 1 WR = 6 tests
    n_metrics = len(CONTINUOUS_METRICS) + 1
    alpha_bonf = ALPHA / n_metrics

    print(f"Bonferroni α = {ALPHA} / {n_metrics} = {alpha_bonf:.4f}")
    print()
    print(f"  {'metric':<18}  {'backtest mean':>14}  {'live mean':>14}  "
          f"{'Δ':>10}  {'t/z':>7}  {'p':>8}  status")
    print(f"  {'-'*18}  {'-'*14}  {'-'*14}  {'-'*10}  {'-'*7}  {'-'*8}  {'-'*6}")

    drift_detected = False

    for name, fn, unit in CONTINUOUS_METRICS:
        a = fn(backtest)
        b = fn(live)
        if len(a) < 2 or len(b) < 2:
            print(f"  {name:<18}  insufficient data")
            continue
        ma, _ = mean_var(a)
        mb, _ = mean_var(b)
        t, p = welch_t(a, b)
        delta = mb - ma
        if p < alpha_bonf:
            status = "★ DRIFT"
            drift_detected = True
        elif p < ALPHA:
            status = "· single-test"
        else:
            status = "✓ ok"
        print(f"  {name:<18}  {ma:>+13,.2f} {unit}  {mb:>+13,.2f} {unit}  "
              f"{delta:>+9,.2f}  {t:>+6.2f}  {p:>7.4f}  {status}")

    # WR comparison via two-proportion z-test.
    wins_a = sum(1 for t in backtest if t.pnl_usd > 0)
    wins_b = sum(1 for t in live if t.pnl_usd > 0)
    wr_a = wins_a / len(backtest)
    wr_b = wins_b / len(live)
    z, p = proportion_z(wr_b, len(live), wr_a, len(backtest))
    delta_pp = (wr_b - wr_a) * 100
    if p < alpha_bonf:
        status = "★ DRIFT"
        drift_detected = True
    elif p < ALPHA:
        status = "· single-test"
    else:
        status = "✓ ok"
    print(f"  {'WR':<18}  {wr_a*100:>13.1f} %  {wr_b*100:>13.1f} %  "
          f"{delta_pp:>+8.2f}pp  {z:>+6.2f}  {p:>7.4f}  {status}")

    print()
    if drift_detected:
        print("⚠ DRIFT DETECTED on ≥1 metric (Bonferroni-corrected). Investigate before")
        print("  next deploy decision. Probable causes: changing market regime, broker")
        print("  cost-stack drift (fee/slip not matching modeled), exchange-side change.")
        return 1
    print("✓ No Bonferroni-significant drift across any metric. Strategy behavior")
    print("  matches the backtest empirical distribution within tolerance.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
