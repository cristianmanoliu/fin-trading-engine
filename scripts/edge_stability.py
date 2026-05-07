#!/usr/bin/env python3
"""edge_stability.py — Quarter-level OLS regression of strategy NET against
time, applying the locked decision rule from
`results/edge_stability_decision_rule_2026-05-07.md`.

Tests STABLE_EDGE / IMPROVING_EDGE / DECAYING_EDGE.
"""

from __future__ import annotations

import math
import sys
from pathlib import Path
from datetime import datetime, timezone

sys.path.insert(0, str(Path(__file__).resolve().parent))
from bootstrap_ci import fmt_dollar, load_chronological_pnl  # noqa: E402
from kill_bar_calibration import load_trades  # noqa: E402

ALPHA = 0.05  # locked


def calendar_quarter_index(ts: datetime, base_year: int) -> int:
    """Return 1-indexed quarter offset from base_year-Q1."""
    year_off = ts.year - base_year
    quarter_in_year = (ts.month - 1) // 3  # 0..3
    return year_off * 4 + quarter_in_year + 1


def quarter_label(idx: int, base_year: int) -> str:
    year_off = (idx - 1) // 4
    q_in_year = (idx - 1) % 4
    return f"{base_year + year_off}-Q{q_in_year + 1}"


def ols_with_p(xs: list[float], ys: list[float]) -> dict:
    """Manual OLS with t-test on slope. Returns slope, intercept, p-value, etc."""
    n = len(xs)
    if n < 3:
        return {"valid": False}
    mx = sum(xs) / n
    my = sum(ys) / n
    sxx = sum((x - mx) ** 2 for x in xs)
    syy = sum((y - my) ** 2 for y in ys)
    sxy = sum((x - mx) * (y - my) for x, y in zip(xs, ys))
    if sxx <= 0:
        return {"valid": False}
    slope = sxy / sxx
    intercept = my - slope * mx
    # Residuals & residual SE
    residuals = [y - (intercept + slope * x) for x, y in zip(xs, ys)]
    rss = sum(r ** 2 for r in residuals)
    df = n - 2
    if df <= 0:
        return {"valid": False}
    sigma2 = rss / df
    se_slope = math.sqrt(sigma2 / sxx)
    t_stat = slope / se_slope if se_slope > 0 else 0.0
    # Two-sided p-value via Student's-t survival approximation.
    # Using regularized incomplete beta function I_x(a,b).
    # p = 2 * (1 - cdf(t, df)) for t > 0; p = 2 * cdf(t, df) for t < 0.
    # Approximate via a simple series since we don't have scipy.
    p_value = student_t_two_sided_p(abs(t_stat), df)
    r_squared = 1 - rss / syy if syy > 0 else 0.0
    return {
        "valid": True,
        "n": n,
        "slope": slope,
        "intercept": intercept,
        "se_slope": se_slope,
        "t_stat": t_stat,
        "df": df,
        "p_value": p_value,
        "r_squared": r_squared,
    }


def student_t_two_sided_p(t: float, df: int) -> float:
    """Two-sided p-value for Student's-t. Uses the relationship
    p_one_sided = 0.5 * I_{df/(df+t²)}(df/2, 1/2).
    Since we're computing two-sided, p = I_{df/(df+t²)}(df/2, 1/2)."""
    if t <= 0:
        return 1.0
    x = df / (df + t * t)
    return _regularized_incomplete_beta(x, df / 2.0, 0.5)


def _regularized_incomplete_beta(x: float, a: float, b: float) -> float:
    """Numerical approximation of I_x(a, b). Uses continued-fraction
    representation good to ~1e-7 in [0,1]. Adapted from Numerical
    Recipes Section 6.4."""
    if x <= 0:
        return 0.0
    if x >= 1:
        return 1.0
    # log front factor
    bt = math.exp(
        _lgamma(a + b) - _lgamma(a) - _lgamma(b)
        + a * math.log(x) + b * math.log(1 - x)
    )
    if x < (a + 1) / (a + b + 2):
        return bt * _betacf(x, a, b) / a
    return 1.0 - bt * _betacf(1 - x, b, a) / b


def _betacf(x: float, a: float, b: float, max_iter: int = 200, eps: float = 3e-7) -> float:
    qab, qap, qam = a + b, a + 1.0, a - 1.0
    c, d = 1.0, 1.0 - qab * x / qap
    if abs(d) < 1e-30:
        d = 1e-30
    d = 1.0 / d
    h = d
    for m in range(1, max_iter + 1):
        m2 = 2 * m
        aa = m * (b - m) * x / ((qam + m2) * (a + m2))
        d = 1.0 + aa * d
        if abs(d) < 1e-30:
            d = 1e-30
        c = 1.0 + aa / c
        if abs(c) < 1e-30:
            c = 1e-30
        d = 1.0 / d
        h *= d * c
        aa = -(a + m) * (qab + m) * x / ((a + m2) * (qap + m2))
        d = 1.0 + aa * d
        if abs(d) < 1e-30:
            d = 1e-30
        c = 1.0 + aa / c
        if abs(c) < 1e-30:
            c = 1e-30
        d = 1.0 / d
        delta = d * c
        h *= delta
        if abs(delta - 1.0) < eps:
            break
    return h


def _lgamma(x: float) -> float:
    return math.lgamma(x)


def main() -> int:
    args = sys.argv[1:]
    if args and Path(args[0]).is_dir():
        journal_dir = Path(args[0])
    else:
        journal_dir = Path(__file__).resolve().parent.parent / "results" / "hod_journals" / "2026-05-07"

    print("EDGE STABILITY — edge_stability_decision_rule_2026-05-07.md")
    print("=" * 80)
    print(f"journal dir: {journal_dir}")
    print(f"alpha:       {ALPHA} (two-sided)")
    print()

    trades = load_trades(journal_dir)
    if not trades:
        print(f"  no trades in {journal_dir}")
        return 1
    earliest = trades[0]["ts"]
    latest = trades[-1]["ts"]
    base_year = earliest.year
    print(f"trades: {len(trades):,}, {earliest.date()} → {latest.date()}")
    print(f"base year for quarter indexing: {base_year}")
    print()

    # Bucket trades into calendar quarters.
    quarter_pnl: dict = {}
    quarter_count: dict = {}
    for t in trades:
        q_idx = calendar_quarter_index(t["ts"], base_year)
        quarter_pnl[q_idx] = quarter_pnl.get(q_idx, 0.0) + t["pnl_usd"]
        quarter_count[q_idx] = quarter_count.get(q_idx, 0) + 1

    # Iterate sequentially, possibly with empty quarters (none expected).
    quarters = sorted(quarter_pnl.keys())
    print("Per-quarter NET and trade count:")
    print(f"  {'idx':>3}  {'label':<8}  {'trades':>6}  {'NET':>14}")
    print(f"  {'-'*3}  {'-'*8}  {'-'*6}  {'-'*14}")
    rows = []
    for q in quarters:
        label = quarter_label(q, base_year)
        n = quarter_count.get(q, 0)
        pnl = quarter_pnl[q]
        rows.append((q, label, n, pnl))
        print(f"  {q:>3}  {label:<8}  {n:>6}  {fmt_dollar(pnl):>14}")
    print()

    # OLS regression.
    xs = [float(q) for q, _, _, _ in rows]
    ys = [pnl for _, _, _, pnl in rows]
    fit = ols_with_p(xs, ys)
    if not fit.get("valid"):
        print("OLS fit failed — too few quarters?")
        return 1

    print("Linear regression: NET_q = a + b · t")
    print(f"  n quarters:    {fit['n']}")
    print(f"  intercept (a): {fmt_dollar(fit['intercept'])}/q")
    print(f"  slope     (b): {fmt_dollar(fit['slope'])}/q-per-quarter")
    print(f"  SE slope:      {fmt_dollar(fit['se_slope'])}")
    print(f"  t-statistic:   {fit['t_stat']:.3f}  (df={fit['df']})")
    print(f"  p-value:       {fit['p_value']:.4f}  (two-sided)")
    print(f"  R-squared:     {fit['r_squared']:.4f}")
    print()

    # Apply locked decision rule.
    print("=" * 80)
    print(f"DECISION RULE EVALUATION (α = {ALPHA}):")
    if fit["p_value"] > ALPHA:
        verdict = "STABLE_EDGE"
        msg = f"  p = {fit['p_value']:.4f} > {ALPHA} → no detectable trend."
    elif fit["slope"] > 0:
        verdict = "IMPROVING_EDGE"
        msg = f"  p = {fit['p_value']:.4f} ≤ {ALPHA} AND slope > 0 → edge is growing."
    else:
        verdict = "DECAYING_EDGE"
        msg = f"  p = {fit['p_value']:.4f} ≤ {ALPHA} AND slope < 0 → edge is shrinking."
    print(msg)
    print()
    print("=" * 80)
    print(f"VERDICT: {verdict}")
    print("=" * 80)
    print()

    # Diagnostic: if STABLE, also report what slope WOULD be needed for significance.
    if verdict == "STABLE_EDGE":
        # Critical t at α=0.05, df=fit['df']: ~2.09 for df=19
        crit_t = 2.09 if fit["df"] == 19 else 2.0
        crit_slope = fit["se_slope"] * crit_t
        print(f"Diagnostic: critical slope for α={ALPHA} at df={fit['df']} is "
              f"|{fmt_dollar(crit_slope)}|/q. Observed slope is "
              f"{fmt_dollar(fit['slope'])}/q.")

    # Recent vs early comparison, regardless of verdict (informational).
    n_quarters = len(rows)
    half = n_quarters // 2
    early_pnl = sum(p for _, _, _, p in rows[:half])
    late_pnl = sum(p for _, _, _, p in rows[half:])
    print()
    print(f"Sanity context (no decision impact):")
    print(f"  early {half} quarters NET:  {fmt_dollar(early_pnl)}")
    print(f"  late {n_quarters - half} quarters NET:  {fmt_dollar(late_pnl)}")
    print(f"  recent-vs-early ratio: {late_pnl / early_pnl:.2f}x" if early_pnl > 0 else "")

    # Recent 4 quarters projection (if DECAYING)
    if verdict == "DECAYING_EDGE":
        recent_4 = rows[-4:]
        recent_avg = sum(p for _, _, _, p in recent_4) / 4
        print()
        print(f"Implied forward-paper anchor (if DECAYING):")
        print(f"  recent 4-quarter avg: {fmt_dollar(recent_avg)}/q "
              f"= {fmt_dollar(recent_avg * 4)}/yr")
        print(f"  cumulative-anchor:    {fmt_dollar(sum(ys) * 4 / len(ys))}/yr")

    return 0


if __name__ == "__main__":
    sys.exit(main())
