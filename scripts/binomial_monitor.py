#!/usr/bin/env python3
"""binomial_monitor.py — Interpretive binomial context for forward-paper monitoring.

Given observed wins + n + null-hypothesis WR, computes:
  1. One-tailed p-value: P(wins ≤ observed | n, null_p)
  2. Critical-n curve: smallest n where current observed-WR becomes significant
  3. Bayes factor: P(data | H0=null_p) / P(data | H1=backtest_p)  [when --backtest-p given]

PRE-REG: results/binomial_monitor_decision_rule_2026-05-24.md

INTERPRETIVE ONLY — output is context, not a trigger. Does NOT affect any
decision-grade pipeline. The drift detector governs kill/promote decisions.

Exit codes:
  0  output produced
  1  validation error (bad inputs)
"""
from __future__ import annotations

import argparse
import math
import sys


# ── Core math (stdlib only) ─────────────────────────────────────────────────

def binom_pmf(k: int, n: int, p: float) -> float:
    """P(X = k) for X ~ Binomial(n, p). Log-space to avoid overflow at large n."""
    if k < 0 or k > n:
        return 0.0
    if p == 0.0:
        return 1.0 if k == 0 else 0.0
    if p == 1.0:
        return 1.0 if k == n else 0.0
    log_pmf = (math.lgamma(n + 1) - math.lgamma(k + 1) - math.lgamma(n - k + 1)
               + k * math.log(p) + (n - k) * math.log(1 - p))
    return math.exp(log_pmf)


def binom_cdf(k: int, n: int, p: float) -> float:
    """P(X ≤ k) for X ~ Binomial(n, p). One-tailed lower tail."""
    return sum(binom_pmf(i, n, p) for i in range(k + 1))


def critical_n(observed_wr: float, null_p: float, alpha: float) -> int | None:
    """Smallest n where P(wins ≤ floor(observed_wr * n) | n, null_p) ≤ alpha.

    Returns None if not reachable within 10_000 trades.
    """
    for n in range(1, 10_001):
        wins = math.floor(observed_wr * n)
        if binom_cdf(wins, n, null_p) <= alpha:
            return n
    return None


def bayes_factor(wins: int, n: int, h0_p: float, h1_p: float) -> float:
    """BF = P(data|H0) / P(data|H1). BF < 1 favors H1 (backtest prior)."""
    p_h0 = binom_pmf(wins, n, h0_p)
    p_h1 = binom_pmf(wins, n, h1_p)
    if p_h1 == 0:
        return float("inf")
    return p_h0 / p_h1


# ── CLI ─────────────────────────────────────────────────────────────────────

def validate_args(wins: int, trades: int, null_p: float,
                  backtest_p: float | None, alpha: float) -> list[str]:
    errors = []
    if trades <= 0:
        errors.append(f"--trades must be > 0 (got {trades})")
    if wins < 0:
        errors.append(f"--wins must be ≥ 0 (got {wins})")
    if trades > 0 and wins > trades:
        errors.append(f"--wins ({wins}) cannot exceed --trades ({trades})")
    if not 0.0 < null_p < 1.0:
        errors.append(f"--null-p must be in (0, 1) (got {null_p})")
    if backtest_p is not None and not 0.0 < backtest_p < 1.0:
        errors.append(f"--backtest-p must be in (0, 1) (got {backtest_p})")
    if not 0.0 < alpha < 1.0:
        errors.append(f"--alpha must be in (0, 1) (got {alpha})")
    return errors


def main() -> int:
    parser = argparse.ArgumentParser(
        description=__doc__,
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    parser.add_argument("--wins", type=int, required=True, metavar="W",
                        help="observed number of winning trades")
    parser.add_argument("--trades", type=int, required=True, metavar="N",
                        help="total closed trades")
    parser.add_argument("--null-p", type=float, required=True, metavar="P",
                        help="null hypothesis WR, e.g. 0.143 for breakeven at 6:1 RR")
    parser.add_argument("--backtest-p", type=float, default=None, metavar="P2",
                        help="backtest expected WR for Bayes factor (optional)")
    parser.add_argument("--alpha", type=float, default=0.05, metavar="A",
                        help="significance level for critical-n curve (default 0.05)")
    args = parser.parse_args()

    errors = validate_args(args.wins, args.trades, args.null_p,
                           args.backtest_p, args.alpha)
    if errors:
        for e in errors:
            print(f"ERROR: {e}", file=sys.stderr)
        return 1

    wins = args.wins
    trades = args.trades
    null_p = args.null_p
    alpha = args.alpha
    observed_wr = wins / trades

    p_val = binom_cdf(wins, trades, null_p)

    crit_n_alpha = critical_n(observed_wr, null_p, alpha)
    crit_n_01 = critical_n(observed_wr, null_p, 0.01)

    print()
    print("══════════════════════════════════════════════════════════════════")
    print("  Binomial Monitor — interpretive context only")
    print("  Pre-reg: results/binomial_monitor_decision_rule_2026-05-24.md")
    print("══════════════════════════════════════════════════════════════════")
    print()
    print(f"  Observed:    {wins} wins / {trades} trades  (WR {observed_wr*100:.1f}%)")
    print(f"  Null WR:     {null_p*100:.1f}%  (H0)")
    if args.backtest_p:
        print(f"  Backtest WR: {args.backtest_p*100:.1f}%  (H1)")
    print()
    print(f"  P(wins ≤ {wins} | n={trades}, p={null_p:.3f}) = {p_val:.4f}  ", end="")
    if p_val > 0.05:
        print("→ NOT suspicious vs H0")
    elif p_val > 0.01:
        print("→ MILDLY suspicious (p<0.05 but above floor)")
    else:
        print("→ SUSPICIOUS (p<0.01 — but n<50 governs; see pre-reg)")
    print()

    # Critical-n curve
    print(f"  Critical-n curve (at observed WR {observed_wr*100:.1f}%):")
    if crit_n_alpha is None:
        print(f"    α={alpha:.2f}: never reaches significance within 10k trades")
    else:
        print(f"    α={alpha:.2f}: suspicious at n ≥ {crit_n_alpha}")
    if crit_n_01 is None:
        print(f"    α=0.01:  never reaches significance within 10k trades")
    else:
        print(f"    α=0.01:  suspicious at n ≥ {crit_n_01}")
    print()

    # Bayes factor (optional)
    if args.backtest_p:
        bf = bayes_factor(wins, trades, null_p, args.backtest_p)
        if bf < 1.0:
            bf_interp = f"data {1/bf:.1f}× more likely under H1 (backtest) than H0"
        elif bf > 1.0:
            bf_interp = f"data {bf:.1f}× more likely under H0 (null) than H1"
        else:
            bf_interp = "equal likelihood under H0 and H1"
        print(f"  Bayes factor BF₀₁ = {bf:.3f}  ({bf_interp})")
        print()

    print("  ── Interpretation contract ───────────────────────────────────")
    print("  p < 0.05 but n < 50: CONTINUE (Rule 3 in forward_paper_resolution)")
    print("  p < 0.01 but n < 50: CONTINUE — drift detector governs kill/promote")
    print("  This output is context. Do not act on it during monitoring.")
    print("══════════════════════════════════════════════════════════════════")
    print()

    return 0


if __name__ == "__main__":
    sys.exit(main())
