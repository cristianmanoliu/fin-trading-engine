#!/usr/bin/env python3
"""quarterly_mechanism.py — high-statistical-power mechanism analysis using 25
quarters of per-symbol data instead of 3 windows.

With 25 data points per symbol (vs 3 in the window analysis), we can:
  1. Detect per-symbol skill at meaningful statistical power
  2. Compute robust pairwise correlation matrix
  3. Test for time-trend / decay in aggregate edge
  4. Compute realistic drawdown and peak-to-trough
  5. Bootstrap CIs on annualized expected value

Input: results/p4_quarterly_slip15_full_2026-05-06.tsv (or older 2026-05-06.tsv)
       Format: symbol \\t period \\t trades \\t wins \\t net_usd

Output: report to stdout.
"""
from __future__ import annotations

import csv
import random
import sys
from collections import defaultdict
from pathlib import Path
from statistics import mean, median, stdev


def load_quarterly(path: Path) -> dict[str, list[tuple[str, int, int, int]]]:
    """Returns {symbol: [(period, trades, wins, net_usd), ...]} sorted by period."""
    out: dict[str, list[tuple[str, int, int, int]]] = defaultdict(list)
    with path.open() as f:
        rdr = csv.reader(f, delimiter="\t")
        next(rdr)  # header
        for row in rdr:
            if len(row) < 5:
                continue
            sym, period, trades, wins, net = row
            out[sym].append((period, int(trades), int(wins), int(net)))
    for sym in out:
        out[sym].sort(key=lambda r: r[0])
    return out


def correlation(xs: list[float], ys: list[float]) -> float:
    n = len(xs)
    if n < 2:
        return 0.0
    mx, my = sum(xs) / n, sum(ys) / n
    num = sum((x - mx) * (y - my) for x, y in zip(xs, ys))
    dx = sum((x - mx) ** 2 for x in xs) ** 0.5
    dy = sum((y - my) ** 2 for y in ys) ** 0.5
    if dx == 0 or dy == 0:
        return 0.0
    return num / (dx * dy)


def t_stat(xs: list[float]) -> tuple[float, float]:
    """One-sample t-test of mean > 0. Returns (t, sample_size)."""
    n = len(xs)
    if n < 2:
        return 0.0, n
    m = sum(xs) / n
    s = stdev(xs)
    if s == 0:
        return 0.0, n
    return m / (s / n ** 0.5), n


def bootstrap_ci(values: list[float], n_iter: int = 5000, ci: float = 0.95) -> tuple[float, float]:
    """Bootstrap CI for mean."""
    if len(values) < 2:
        return 0.0, 0.0
    means = []
    n = len(values)
    for _ in range(n_iter):
        sample = [values[random.randint(0, n-1)] for _ in range(n)]
        means.append(sum(sample) / n)
    means.sort()
    lo = means[int(n_iter * (1 - ci) / 2)]
    hi = means[int(n_iter * (1 + ci) / 2)]
    return lo, hi


def main() -> None:
    root = Path(__file__).resolve().parent.parent
    # Prefer the extended (full) TSV if present
    if len(sys.argv) > 1:
        tsv = Path(sys.argv[1])
    else:
        # Default search order: postfix > full > orig (newest data first)
        tsv_postfix = root / "results/p4_quarterly_slip15_postfix_2026-05-06.tsv"
        tsv_full = root / "results/p4_quarterly_slip15_full_2026-05-06.tsv"
        tsv_orig = root / "results/p4_quarterly_slip15_2026-05-06.tsv"
        if tsv_postfix.exists():
            tsv = tsv_postfix
        elif tsv_full.exists():
            tsv = tsv_full
        else:
            tsv = tsv_orig
    if not tsv.exists():
        print(f"FAIL: TSV not found ({tsv})", file=sys.stderr)
        sys.exit(1)

    data = load_quarterly(tsv)
    universe = sorted(data.keys())
    all_periods = sorted({p for sym in universe for p, *_ in data[sym]})

    print()
    print("=" * 110)
    print("  QUARTERLY MECHANISM ANALYSIS  (P4-Combined: 4H short, target_rr=6, slip=15bp, fee=10bp)")
    print(f"  Universe: {len(universe)} symbols × {len(all_periods)} quarters")
    print(f"  Source: {tsv.name}")
    print("=" * 110)

    # ─────────────────────────────────────────────────────────────────────────
    # Test Q1: Per-symbol skill detection (t-test against zero, Bonferroni)
    # ─────────────────────────────────────────────────────────────────────────
    print("\n  TEST Q1 — per-symbol skill detection (one-sample t-test, Bonferroni)")
    print("  " + "─" * 90)
    print("  Each symbol has up to 25 quarterly NET observations. Test H0: mean=0 vs H1: mean!=0.")
    print("  Bonferroni-adjusted critical value at α=0.05/57 ≈ 0.0009 → |t| > 3.45 (df>20)")
    print()

    skill_results = []
    for sym in universe:
        nets = [r[3] for r in data[sym] if r[1] > 0]  # exclude no-trade quarters
        if len(nets) < 10:
            continue
        t, n = t_stat([float(x) for x in nets])
        m = sum(nets) / len(nets)
        skill_results.append((sym, m, t, n, sum(nets)))

    skill_results.sort(key=lambda r: -r[2])  # by t-stat desc

    # Bonferroni threshold: α=0.05 / number-of-tests (use Welch's approximation, df>>20)
    n_tests = len(skill_results)
    bonf_alpha = 0.05 / n_tests
    # For α=0.0009 two-tail with df>20, critical t ≈ 3.45
    crit_t = 3.45
    print(f"  {'symbol':<12} {'avg_q':>10} {'sum_q':>10} {'n_q':>5} {'t':>7}  significant?")
    print(f"  {'-'*12} {'-'*10} {'-'*10} {'-'*5} {'-'*7}  ---")
    sig_count = 0
    for sym, m, t, n, total in skill_results[:15]:
        sig = "YES" if abs(t) > crit_t else "no"
        if abs(t) > crit_t:
            sig_count += 1
        print(f"  {sym:<12} ${m:>+9,.0f} ${total:>+9,} {n:>5} {t:>+7.2f}  {sig}")

    print(f"  ... ({len(skill_results) - 15} more rows)")
    # Count all
    all_sig = sum(1 for _, _, t, _, _ in skill_results if abs(t) > crit_t)
    print(f"\n  → {all_sig}/{len(skill_results)} symbols pass Bonferroni-adjusted significance (p < {bonf_alpha:.4f})")
    if all_sig <= n_tests * 0.05 + 2:
        print(f"    Under H0, expect {n_tests * 0.05:.1f} false-positives. {all_sig} observed → ")
        print(f"    {'NO statistical evidence of per-symbol skill' if all_sig <= 3 else 'consistent with chance'}.")

    # ─────────────────────────────────────────────────────────────────────────
    # Test Q2: Time-trend / decay test
    # ─────────────────────────────────────────────────────────────────────────
    print("\n  TEST Q2 — time trend in aggregate quarterly NET (decay detection)")
    print("  " + "─" * 90)
    quarterly_agg = []
    for period in all_periods:
        agg = sum(net for sym in universe for p, t, w, net in data[sym] if p == period)
        n_active = sum(1 for sym in universe for p, t, w, net in data[sym] if p == period and t > 0)
        quarterly_agg.append((period, agg, n_active))

    print(f"  {'quarter':<10} {'NET':>12} {'active syms':>12}")
    for period, agg, n_active in quarterly_agg:
        bar = "█" * max(0, min(40, int(agg / 50000) if agg > 0 else 0))
        bar_neg = "▒" * max(0, min(40, int(-agg / 50000) if agg < 0 else 0))
        print(f"  {period:<10} ${agg:>+11,} {n_active:>12}  {bar}{bar_neg}")

    # Linear regression: NET ~ quarter_index
    indices = list(range(len(quarterly_agg)))
    nets_qq = [a for _, a, _ in quarterly_agg]
    if len(indices) >= 4:
        n = len(indices)
        mx = sum(indices) / n
        my = sum(nets_qq) / n
        slope = sum((i - mx) * (y - my) for i, y in zip(indices, nets_qq)) / sum((i - mx) ** 2 for i in indices)
        intercept = my - slope * mx
        # Slope significance: t-stat of slope coefficient
        residuals = [y - (slope * i + intercept) for i, y in zip(indices, nets_qq)]
        rss = sum(r ** 2 for r in residuals)
        sxx = sum((i - mx) ** 2 for i in indices)
        if sxx > 0 and n > 2:
            se_slope = (rss / (n - 2)) ** 0.5 / sxx ** 0.5
            t_slope = slope / se_slope if se_slope > 0 else 0
        else:
            t_slope = 0
        print(f"\n  Linear trend: NET = {slope:+,.0f} × q + {intercept:+,.0f}  (slope t={t_slope:+.2f})")
        if t_slope < -2:
            print("  → NEGATIVE trend significant (p<0.05). Strategy edge is decaying over time.")
        elif t_slope < -1:
            print("  → Negative trend (not significant). Possible decay.")
        elif t_slope > 2:
            print("  → POSITIVE trend significant. Strategy edge improving (unusual).")
        else:
            print("  → No significant trend. Strategy edge is stable in expectation.")

    # ─────────────────────────────────────────────────────────────────────────
    # Test Q3: Drawdown — worst peak-to-trough on cumulative quarterly NET
    # ─────────────────────────────────────────────────────────────────────────
    print("\n  TEST Q3 — drawdown analysis (cumulative quarterly NET)")
    print("  " + "─" * 90)
    cum = 0
    cum_history = [(quarterly_agg[0][0], 0)]
    for period, agg, _ in quarterly_agg:
        cum += agg
        cum_history.append((period, cum))
    peak = cum_history[0][1]
    peak_period = cum_history[0][0]
    max_dd = 0
    max_dd_period = ""
    max_dd_peak = ""
    max_dd_dollars = 0
    for period, c in cum_history:
        if c > peak:
            peak = c
            peak_period = period
        dd = peak - c
        if dd > max_dd:
            max_dd = dd
            max_dd_period = period
            max_dd_peak = peak_period
            max_dd_dollars = -dd
    print(f"    Cumulative range: min ${min(c for _, c in cum_history):+,}  max ${max(c for _, c in cum_history):+,}")
    print(f"    Final cumulative: ${cum_history[-1][1]:+,}")
    print(f"    Max drawdown:     ${max_dd_dollars:+,} (peak {max_dd_peak} → trough {max_dd_period})")
    print(f"    Drawdown duration: {[p for p, _ in cum_history].index(max_dd_period) - [p for p, _ in cum_history].index(max_dd_peak)} quarters")

    # ─────────────────────────────────────────────────────────────────────────
    # Test Q4: Bootstrap CI on annualized expected value
    # ─────────────────────────────────────────────────────────────────────────
    print("\n  TEST Q4 — bootstrap CI on annualized expected value")
    print("  " + "─" * 90)
    quarterly_floats = [float(a) for _, a, _ in quarterly_agg]
    ci_lo, ci_hi = bootstrap_ci(quarterly_floats, n_iter=5000)
    annual_lo = ci_lo * 4
    annual_hi = ci_hi * 4
    annual_mean = sum(quarterly_floats) / len(quarterly_floats) * 4
    print(f"    Mean quarterly NET:    ${sum(quarterly_floats)/len(quarterly_floats):+,.0f}")
    print(f"    Mean annualized:       ${annual_mean:+,.0f}/yr")
    print(f"    95% bootstrap CI (annual): [${annual_lo:+,.0f}, ${annual_hi:+,.0f}]")
    print(f"    Width: ${annual_hi - annual_lo:,.0f} — variance bound on per-year expectation")

    # ─────────────────────────────────────────────────────────────────────────
    # Test Q5: Pairwise symbol correlation with N=25
    # ─────────────────────────────────────────────────────────────────────────
    print("\n  TEST Q5 — pairwise symbol correlation (25 quarter observations)")
    print("  " + "─" * 90)
    # Build per-symbol-per-period vector
    nets_per_sym: dict[str, list[float]] = {}
    for sym in universe:
        per_period = {p: net for p, _, _, net in data[sym]}
        nets_per_sym[sym] = [float(per_period.get(p, 0)) for p in all_periods]

    pairwise = []
    syms_with_data = [s for s in universe if any(n != 0 for n in nets_per_sym[s])]
    for i, s1 in enumerate(syms_with_data):
        for s2 in syms_with_data[i+1:]:
            c = correlation(nets_per_sym[s1], nets_per_sym[s2])
            pairwise.append(c)
    avg_pair = mean(pairwise) if pairwise else 0.0
    median_pair = median(pairwise) if pairwise else 0.0
    print(f"    Average pairwise correlation: {avg_pair:+.3f}  (vs +0.16 from window analysis)")
    print(f"    Median: {median_pair:+.3f}")
    print(f"    {sum(1 for c in pairwise if c > 0.5)}/{len(pairwise)} pairs > +0.5")
    print(f"    {sum(1 for c in pairwise if c < -0.5)}/{len(pairwise)} pairs < -0.5")
    print("    With N=25 the correlation estimates are much more reliable (σ ≈ 1/√25 = 0.20)")

    # ─────────────────────────────────────────────────────────────────────────
    # Test Q6: Above-baseline ROBUST count
    # ─────────────────────────────────────────────────────────────────────────
    print("\n  TEST Q6 — high-power per-symbol consistency")
    print("  " + "─" * 90)
    # For each symbol, count quarters positive
    consistency = {}
    for sym in syms_with_data:
        nets = [r[3] for r in data[sym] if r[1] > 0]
        if not nets:
            continue
        pos = sum(1 for n in nets if n > 0)
        consistency[sym] = (pos, len(nets))
    print(f"    {'symbol':<12} {'pos/total':>10} {'pos%':>8}")
    print(f"    {'-'*12} {'-'*10} {'-'*8}")
    sorted_consist = sorted(consistency.items(), key=lambda kv: -kv[1][0]/kv[1][1])
    for sym, (pos, n) in sorted_consist[:10]:
        pct = 100 * pos / n
        print(f"    {sym:<12} {pos:>3}/{n:<6} {pct:>7.1f}%")
    # Avg/baseline
    avg_pos_pct = mean(100 * pos / n for pos, n in consistency.values())
    n_above_60pct = sum(1 for pos, n in consistency.values() if 100*pos/n > 60)
    n_above_70pct = sum(1 for pos, n in consistency.values() if 100*pos/n > 70)
    print(f"\n    Mean positive%: {avg_pos_pct:.1f}%")
    print(f"    Symbols with >60% positive quarters: {n_above_60pct}/{len(consistency)}")
    print(f"    Symbols with >70% positive quarters: {n_above_70pct}/{len(consistency)}")

    print()
    print("=" * 110)


if __name__ == "__main__":
    main()
