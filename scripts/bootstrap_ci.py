#!/usr/bin/env python3
"""
bootstrap_ci.py — block-bootstrap confidence intervals on the deployed-16
candidate strategy's annual NET, with autocorrelation-aware uncertainty.

Three estimators in one run:
  1. Parametric IID — sum ± 1.96·sqrt(N)·std (assumes trades are IID)
  2. Stationary bootstrap (Politis-Romano) at multiple mean block lengths.
     Resamples consecutive blocks to preserve within-block dependence —
     captures regime clustering that IID assumes away.
  3. Effective sample size (1 + 2·Σρ_k) — sanity-checks how much trade-level
     autocorrelation actually matters at this sample size.

Reads the JSONL journals produced by scripts/hod_journals.sh. Treats trades
as a single chronologically-sorted series across all 16 symbols (the
conservative choice — captures cross-symbol contagion that hurts the fleet
simultaneously).

Comparison context (from CLAUDE.md):
  Walk-forward 6-window CI: mean +$130k/yr with 95%CI [-$111k, +$372k].
  That CI uses N=6 quarter-blocks; bootstrap on trade-level (N=2210)
  should be tighter for a comparable estimate but wider than naive IID.

Usage:
  python3 scripts/bootstrap_ci.py [results/hod_journals/2026-05-07] [B]
  (B = bootstrap resamples, default 5000)
"""

from __future__ import annotations

import json
import math
import random
import sys
from datetime import datetime, timezone
from pathlib import Path


def load_chronological_pnl(journal_dir: Path) -> tuple[list[float], float, datetime, datetime]:
    """Load all trades from JSONL files, sort by entry time across symbols.

    Returns (pnl_list, years_spanned, first_ts, last_ts). The cross-symbol
    interleave means consecutive trades in the returned list often span
    different symbols — that's intentional, captures contagion-style
    regime shifts (e.g., a BTC dump simultaneously hurting altcoin shorts).
    """
    rows: list[tuple[datetime, float]] = []
    for jf in sorted(journal_dir.glob("*-*.jsonl")):
        open_event = None
        with jf.open() as f:
            for line in f:
                line = line.strip()
                if not line:
                    continue
                ev = json.loads(line)
                if ev["event"] == "open":
                    open_event = ev
                elif ev["event"] == "close" and open_event is not None:
                    ts = datetime.fromisoformat(open_event["ts"].replace("Z", "+00:00"))
                    rows.append((ts.astimezone(timezone.utc), ev.get("pnl_usd", 0.0)))
                    open_event = None
    rows.sort(key=lambda x: x[0])
    if not rows:
        raise RuntimeError(f"no trades found in {journal_dir}")
    span = (rows[-1][0] - rows[0][0]).total_seconds() / (86400.0 * 365.25)
    return [r[1] for r in rows], span, rows[0][0], rows[-1][0]


def autocorr(data: list[float], lag: int) -> float:
    """Lag-k Pearson autocorrelation of a 1D series."""
    n = len(data)
    if n <= lag:
        return 0.0
    x = data[: n - lag]
    y = data[lag:]
    mx = sum(x) / len(x)
    my = sum(y) / len(y)
    numer = sum((xi - mx) * (yi - my) for xi, yi in zip(x, y))
    sxx = sum((xi - mx) ** 2 for xi in x)
    syy = sum((yi - my) ** 2 for yi in y)
    if sxx <= 0 or syy <= 0:
        return 0.0
    return numer / math.sqrt(sxx * syy)


def stationary_bootstrap_sums(data: list[float], mean_block: int, B: int, seed: int = 42) -> list[float]:
    """Politis-Romano stationary bootstrap. Returns B resampled sums.

    At each step, the next index either:
      - continues the current block with prob 1 - 1/L
      - or starts a new random block with prob 1/L
    Wraps around at the end (circular). Each resample is the same length
    as the original series, so sum-statistics are directly comparable.
    """
    rng = random.Random(seed)
    n = len(data)
    p_new = 1.0 / mean_block
    out: list[float] = []
    for _ in range(B):
        s = 0.0
        idx = rng.randrange(n)
        for _ in range(n):
            s += data[idx]
            if rng.random() < p_new:
                idx = rng.randrange(n)
            else:
                idx = (idx + 1) % n
        out.append(s)
    out.sort()
    return out


def quantile(sorted_data: list[float], q: float) -> float:
    n = len(sorted_data)
    if n == 0:
        return 0.0
    pos = q * (n - 1)
    lo = int(pos)
    hi = min(lo + 1, n - 1)
    frac = pos - lo
    return sorted_data[lo] * (1 - frac) + sorted_data[hi] * frac


def fmt_dollar(x: float) -> str:
    sign = "-" if x < 0 else "+"
    return f"{sign}${abs(x):,.0f}"


def main() -> int:
    args = sys.argv[1:]
    if args and Path(args[0]).is_dir():
        journal_dir = Path(args[0])
        args = args[1:]
    else:
        root = Path(__file__).parent.parent / "results" / "hod_journals"
        candidates = sorted([p for p in root.iterdir() if p.is_dir()])
        if not candidates:
            print(f"no journal dirs under {root} — run scripts/hod_journals.sh first")
            return 1
        journal_dir = candidates[-1]

    B = int(args[0]) if args else 5000

    pnls, years, first_ts, last_ts = load_chronological_pnl(journal_dir)
    N = len(pnls)
    total = sum(pnls)
    annual_point = total / years

    print(f"BLOCK BOOTSTRAP CI — deployed-16 candidate (4H short EMA9/21 mh504 RR6, fee10/slip5)")
    print(f"=" * 80)
    print(f"journal dir: {journal_dir}")
    print(f"trades:      {N:,}")
    print(f"span:        {years:.2f} years ({first_ts.date()} → {last_ts.date()})")
    print(f"total NET:   {fmt_dollar(total)}")
    print(f"annual:      {fmt_dollar(annual_point)}/yr (point estimate)")
    print(f"mean/trade:  {fmt_dollar(total/N)} (over {N:,} trades)")
    print(f"resamples:   {B:,} per block-length")
    print()

    # ── Autocorrelation diagnostics ──────────────────────────────────────────
    print("Per-trade PnL autocorrelation (proxy for regime clustering):")
    rhos = [(lag, autocorr(pnls, lag)) for lag in [1, 5, 10, 25, 50, 100, 250]]
    for lag, rho in rhos:
        flag = "  ←" if abs(rho) > 2 / math.sqrt(N) else ""
        print(f"  lag {lag:>3}: ρ = {rho:+.4f}{flag}")
    # Effective sample size approximation (Newey-West-ish at small lag).
    rho_sum = sum(rho for _, rho in rhos[:5])  # use lags 1..50
    ess_factor = 1 + 2 * rho_sum
    if ess_factor < 1:
        ess_factor = 1.0
    ess = N / ess_factor
    print(f"  ESS estimate (1 + 2·Σρ_lag1..50): {ess:,.0f} effective trades from {N:,} actual")
    print(f"  → CI width inflation under autocorrelation: ×{math.sqrt(ess_factor):.2f}")
    print()

    # ── Parametric IID baseline ──────────────────────────────────────────────
    mean = total / N
    var = sum((p - mean) ** 2 for p in pnls) / (N - 1)
    std = math.sqrt(var)
    se_total = std * math.sqrt(N)  # SD of sum under IID
    iid_lo = (total - 1.96 * se_total) / years
    iid_hi = (total + 1.96 * se_total) / years
    print(f"Parametric IID 95% CI (assumes trades are IID — autocorrelation IGNORED):")
    print(f"  annual NET: [{fmt_dollar(iid_lo)}, {fmt_dollar(iid_hi)}]/yr")
    print(f"  point:      {fmt_dollar(annual_point)}/yr")
    print(f"  half-width: ${(iid_hi - iid_lo) / 2:,.0f}/yr")
    print()

    # ── Stationary block bootstrap at multiple block lengths ─────────────────
    print(f"Stationary bootstrap 95% CI by mean block length L:")
    print(f"  L is the expected # of consecutive trades resampled together.")
    print(f"  L=1  → effectively IID bootstrap (compare to parametric above).")
    print(f"  L=√N≈47 → standard rule-of-thumb. L=250 ≈ ~2 quarters of regime.")
    print()
    print(f"  {'L':>4}  {'median':>14}  {'2.5%':>14}  {'97.5%':>14}  {'half-width':>11}  "
          f"{'P(>$0)':>7}  {'P(>$50k)':>9}  {'P(>$100k)':>10}  {'P(>$200k)':>10}")
    print(f"  {'-'*4}  {'-'*14}  {'-'*14}  {'-'*14}  {'-'*11}  "
          f"{'-'*7}  {'-'*9}  {'-'*10}  {'-'*10}")

    rule_of_thumb = max(2, int(round(math.sqrt(N))))
    block_lengths = sorted({1, 5, 25, rule_of_thumb, 100, 250, 500})

    rows = []
    for L in block_lengths:
        if L >= N:
            continue
        sums = stationary_bootstrap_sums(pnls, L, B, seed=42 + L)
        annual = sorted(s / years for s in sums)
        med = quantile(annual, 0.5)
        lo = quantile(annual, 0.025)
        hi = quantile(annual, 0.975)
        half = (hi - lo) / 2
        p_pos = sum(1 for x in annual if x > 0) / B
        p_50k = sum(1 for x in annual if x > 50_000) / B
        p_100k = sum(1 for x in annual if x > 100_000) / B
        p_200k = sum(1 for x in annual if x > 200_000) / B
        rows.append((L, med, lo, hi, half, p_pos, p_50k, p_100k, p_200k))
        marker = "  ← √N" if L == rule_of_thumb else ""
        print(f"  {L:>4}  {fmt_dollar(med):>14}  {fmt_dollar(lo):>14}  {fmt_dollar(hi):>14}  "
              f"${half:>10,.0f}  {p_pos*100:>6.1f}%  {p_50k*100:>8.1f}%  {p_100k*100:>9.1f}%  {p_200k*100:>9.1f}%{marker}")

    print()
    # Compare to walk-forward CI from CLAUDE.md.
    wf_mean = 130_000
    wf_lo = -111_000
    wf_hi = 372_000
    wf_half = (wf_hi - wf_lo) / 2
    print(f"Comparison — walk-forward 6-window CI from CLAUDE.md:")
    print(f"  annual NET: [{fmt_dollar(wf_lo)}, {fmt_dollar(wf_hi)}]/yr (mean {fmt_dollar(wf_mean)})")
    print(f"  half-width: ${wf_half:,.0f}/yr  (N=6 quarter-blocks)")
    print()
    if rows:
        # Use rule-of-thumb L for the headline comparison.
        L_target = rule_of_thumb
        for r in rows:
            if r[0] == L_target:
                _, med, lo, hi, half, *_ = r
                ratio = wf_half / half
                print(f"At L=√N={L_target}, bootstrap half-width is ${half:,.0f} vs WF ${wf_half:,.0f}")
                print(f"  walk-forward half-width is {ratio:.1f}× wider than trade-level bootstrap.")
                if ratio > 2.0:
                    print(f"  → Walk-forward CI captures regime variance the trade bootstrap misses.")
                    print(f"     Anchor expectations to walk-forward, not bootstrap.")
                elif ratio < 1.5:
                    print(f"  → Block bootstrap and walk-forward CIs agree within ~50%.")
                    print(f"     Trade-level autocorrelation explains most of the regime variance.")
                else:
                    print(f"  → Modest regime-variance gap. Walk-forward is the more conservative estimate.")
                break

    print()
    print("Interpretation guide:")
    print(f"  - 'P(>$X)' is the bootstrap probability that the strategy's annualized NET")
    print(f"    exceeds $X — a direct go/no-go probability framing.")
    print(f"  - As L increases, CI widens (more autocorrelation captured) until it stabilizes.")
    print(f"    The plateau is the honest CI; further L increase only adds estimation noise.")
    print(f"  - Bootstrap median ≈ point estimate is a sanity check (no resampling bias).")
    print(f"  - If walk-forward CI is significantly wider than bootstrap, the regime-variance")
    print(f"    gap is real — trade-level data alone underestimates per-quarter swings.")

    return 0


if __name__ == "__main__":
    sys.exit(main())
