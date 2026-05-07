#!/usr/bin/env python3
"""sample_split_bootstrap.py — independent block bootstrap on each half of
the 5y × 16-sym deployed-candidate trade dataset, applying the locked
decision rule from
`results/sample_split_bootstrap_decision_rule_2026-05-07.md`.

The split point is fixed at 2022-10-31 (calendar midpoint of the original
2020-04 → 2025-04 sample). Per half we re-run Politis-Romano stationary
bootstrap at L = round(√N) × 5000 resamples (mirrors A1 setup), then apply
the per-half pass condition `lo95 > $0 AND p_pos ≥ 95%` mechanically.

Imports the helper functions from `bootstrap_ci.py` rather than duplicating
to keep both pieces of analysis using identical math.

Usage:
  python3 scripts/sample_split_bootstrap.py [results/hod_journals/2026-05-07] [B]
"""

from __future__ import annotations

import math
import sys
from datetime import datetime, timezone
from pathlib import Path

# Same-dir import of the existing bootstrap implementation.
sys.path.insert(0, str(Path(__file__).resolve().parent))
from bootstrap_ci import (  # noqa: E402
    fmt_dollar,
    load_chronological_pnl,
    quantile,
    stationary_bootstrap_sums,
)


SPLIT_POINT = datetime(2022, 10, 31, 23, 59, 59, tzinfo=timezone.utc)
PASS_LOWER_BOUND_USD = 0.0       # lo95 > $0 per half
PASS_P_POS_MIN_PCT = 95.0        # P(>$0) ≥ 95% per half


def filter_pnls_by_date(
    journal_dir: Path, start: datetime | None, end: datetime | None
) -> tuple[list[float], float, datetime, datetime, int]:
    """Re-implements load_chronological_pnl with a date-window filter.

    The base function returns aggregated PnLs without timestamps, which is
    what the bootstrap consumes — but we need to filter BEFORE aggregation,
    so we duplicate just enough logic here to apply the filter cleanly.
    """
    import json

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
                    ts = ts.astimezone(timezone.utc)
                    if start is not None and ts < start:
                        open_event = None
                        continue
                    if end is not None and ts > end:
                        open_event = None
                        continue
                    rows.append((ts, ev.get("pnl_usd", 0.0)))
                    open_event = None
    rows.sort(key=lambda x: x[0])
    if not rows:
        return [], 0.0, datetime(1970, 1, 1, tzinfo=timezone.utc), datetime(1970, 1, 1, tzinfo=timezone.utc), 0
    span = (rows[-1][0] - rows[0][0]).total_seconds() / (86400.0 * 365.25)
    return [r[1] for r in rows], span, rows[0][0], rows[-1][0], len(rows)


def bootstrap_half(label: str, pnls: list[float], years: float, B: int) -> dict:
    """Run the stationary bootstrap on one half and return summary stats."""
    N = len(pnls)
    if N == 0 or years <= 0:
        return {"label": label, "N": 0, "valid": False}
    L = max(2, int(round(math.sqrt(N))))
    sums = stationary_bootstrap_sums(pnls, L, B, seed=42 + L)
    annual = sorted(s / years for s in sums)
    median = quantile(annual, 0.5)
    lo = quantile(annual, 0.025)
    hi = quantile(annual, 0.975)
    p_pos = sum(1 for x in annual if x > 0) / B
    p_50k = sum(1 for x in annual if x > 50_000) / B
    p_100k = sum(1 for x in annual if x > 100_000) / B
    half_width = (hi - lo) / 2

    pass_lo = lo > PASS_LOWER_BOUND_USD
    pass_p_pos = p_pos * 100 >= PASS_P_POS_MIN_PCT
    passed = bool(pass_lo and pass_p_pos)

    return {
        "label": label,
        "N": N,
        "L": L,
        "years": years,
        "total": sum(pnls),
        "annual_point": sum(pnls) / years,
        "median": median,
        "lo": lo,
        "hi": hi,
        "half_width": half_width,
        "p_pos_pct": p_pos * 100,
        "p_50k_pct": p_50k * 100,
        "p_100k_pct": p_100k * 100,
        "pass_lo": pass_lo,
        "pass_p_pos": pass_p_pos,
        "passed": passed,
        "valid": True,
    }


def print_half(d: dict) -> None:
    if not d.get("valid"):
        print(f"  {d['label']}: NO DATA in window")
        return
    print(f"  {d['label']}:")
    print(f"    trades:        {d['N']:,}  (block length L = {d['L']})")
    print(f"    span:          {d['years']:.2f} years")
    print(f"    total NET:     {fmt_dollar(d['total'])}")
    print(f"    annual point:  {fmt_dollar(d['annual_point'])}/yr")
    print(f"    bootstrap median:  {fmt_dollar(d['median'])}/yr")
    print(f"    bootstrap 95% CI:  [{fmt_dollar(d['lo'])}, {fmt_dollar(d['hi'])}]/yr")
    print(f"    half-width:    ${d['half_width']:,.0f}/yr")
    print(f"    P(>$0):        {d['p_pos_pct']:.1f}%   {'✓' if d['pass_p_pos'] else '✗'} (need ≥{PASS_P_POS_MIN_PCT}%)")
    print(f"    P(>$50k):      {d['p_50k_pct']:.1f}%")
    print(f"    P(>$100k):     {d['p_100k_pct']:.1f}%")
    print(f"    lo95 > $0:     {'✓' if d['pass_lo'] else '✗'}")
    print(f"    >>> per-half:  {'PASS' if d['passed'] else 'FAIL'}")


def determine_verdict(a: dict, b: dict) -> str:
    a_pass = a.get("passed", False)
    b_pass = b.get("passed", False)
    if a_pass and b_pass:
        return "ROBUST_BOTH"
    if a_pass != b_pass:
        return "PARTIAL_REGIME_DEP"
    return "FRAGILE"


def main() -> int:
    args = sys.argv[1:]
    if args and Path(args[0]).is_dir():
        journal_dir = Path(args[0])
        args = args[1:]
    else:
        journal_dir = Path(__file__).resolve().parent.parent / "results" / "hod_journals" / "2026-05-07"
    B = int(args[0]) if args else 5000

    print("SAMPLE-SPLIT BOOTSTRAP — sample_split_bootstrap_decision_rule_2026-05-07.md")
    print("=" * 80)
    print(f"journal dir: {journal_dir}")
    print(f"split point: {SPLIT_POINT.date()} (calendar midpoint, locked)")
    print(f"resamples:   {B:,} per half")
    print(f"pass rule:   lo95 > ${PASS_LOWER_BOUND_USD:,.0f} AND P(>$0) ≥ {PASS_P_POS_MIN_PCT}%")
    print()

    # Sanity check — full dataset stats for context vs A1.
    full_pnls, full_years, full_first, full_last = load_chronological_pnl(journal_dir)
    print("Full dataset (sanity vs A1):")
    print(f"  trades = {len(full_pnls):,}  span = {full_years:.2f}y "
          f"({full_first.date()} → {full_last.date()})  "
          f"total = {fmt_dollar(sum(full_pnls))}  "
          f"annual = {fmt_dollar(sum(full_pnls)/full_years)}/yr")
    print()

    # Half A: start of data → SPLIT_POINT
    a_pnls, a_years, a_first, a_last, a_n = filter_pnls_by_date(
        journal_dir, start=None, end=SPLIT_POINT
    )
    a = bootstrap_half(f"Half A ({a_first.date()} → {a_last.date()})", a_pnls, a_years, B)

    # Half B: SPLIT_POINT+1s → end of data
    from datetime import timedelta
    b_pnls, b_years, b_first, b_last, b_n = filter_pnls_by_date(
        journal_dir, start=SPLIT_POINT + timedelta(seconds=1), end=None
    )
    b = bootstrap_half(f"Half B ({b_first.date()} → {b_last.date()})", b_pnls, b_years, B)

    print("Per-half bootstrap results:")
    print()
    print_half(a)
    print()
    print_half(b)
    print()

    verdict = determine_verdict(a, b)
    print("=" * 80)
    print(f"VERDICT: {verdict}")
    print("=" * 80)

    if verdict == "ROBUST_BOTH":
        print("  Both halves independently pass the per-half rule.")
        print("  → Cumulative CI is regime-independent at this split.")
        print("  → Anchor unchanged: deploy expectations stay on existing CI.")
    elif verdict == "PARTIAL_REGIME_DEP":
        weak = a if not a.get("passed") else b
        strong = b if not a.get("passed") else a
        print(f"  Only one half passes ({strong['label']}).")
        print(f"  Weaker half: {weak['label']}, lo95 = {fmt_dollar(weak.get('lo', 0))}/yr, "
              f"P(>$0) = {weak.get('p_pos_pct', 0):.1f}%.")
        print("  → Real regime-dependence. Honest read: anchor expectations to the WEAKER")
        print("    half's CI as the conservative estimate of forward-paper outcomes.")
    else:
        print("  Neither half passes the per-half rule.")
        print("  → Cumulative claim depends on inter-half aggregation rather than")
        print("    standalone strength of each regime. Major flag for deploy decision.")

    return 0


if __name__ == "__main__":
    sys.exit(main())
