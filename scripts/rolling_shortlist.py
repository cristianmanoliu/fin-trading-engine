#!/usr/bin/env python3
"""Rolling-shortlist analysis — would a quarterly-rebalanced shortlist with a
trailing 4-quarter (12-month) lookback have outperformed the fixed deployed-32?

The deployed-32 list is frozen as of 2026-05-05 — it cannot capture symbols that
had a losing 2020-2022 train half but a winning 2023-2025 test half (the
"recoveries cohort"). A rolling shortlist with a trailing 12-month lookback
would in principle pick those up as their trailing PnL goes positive.

This script tests that hypothesis using per-quarter NET data from
`scripts/p4_quarterly.sh`.

Usage:
  python3 scripts/rolling_shortlist.py results/p4_quarterly_slip15_2026-05-06.tsv
"""
from __future__ import annotations

import sys
from collections import defaultdict
from pathlib import Path


# DEPLOYED_32 is the persistent-winners-32 set from the 2026-05-05 cost-survivor
# battery (symbols positive on both train AND test halves at slip=5bp). This is
# an analysis snapshot used as the comparison baseline for the rolling-shortlist
# experiment — NOT the current live deployed list (which is in configs/symbols.yaml
# under `deployed`). The 32-name snapshot includes MKR and FTM which were later
# delisted on Binance Futures (Sept 2025 and Jan 2025); their inclusion here is
# historical, the live deploy uses the exchangeInfo-gated selection.
DEPLOYED_32 = {
    "ROSEUSDT", "MKRUSDT", "GRTUSDT", "1INCHUSDT", "ADAUSDT", "KAVAUSDT",
    "1000SHIBUSDT", "ENSUSDT", "XLMUSDT", "ETCUSDT", "RUNEUSDT", "AVAXUSDT",
    "IMXUSDT", "DOTUSDT", "BCHUSDT", "FTMUSDT", "FILUSDT", "SOLUSDT",
    "CRVUSDT", "AAVEUSDT", "APTUSDT", "SNXUSDT", "NEARUSDT", "APEUSDT",
    "MANAUSDT", "AXSUSDT", "GALAUSDT", "ETHUSDT", "ENJUSDT", "LINKUSDT",
    "VETUSDT", "LDOUSDT",
}


def quarter_key(year: int, q: int) -> str:
    return f"{year}-Q{q}"


def all_quarters(start_year: int = 2020, end_year: int = 2025, end_q: int = 1) -> list[str]:
    out = []
    for y in range(start_year, end_year + 1):
        last = end_q if y == end_year else 4
        for q in range(1, last + 1):
            out.append(quarter_key(y, q))
    return out


def load(path: Path) -> dict[tuple[str, str], tuple[int, int]]:
    """Returns {(symbol, quarter): (trades, net)}."""
    out: dict[tuple[str, str], tuple[int, int]] = {}
    for line in path.read_text().splitlines()[1:]:
        parts = line.split("\t")
        if len(parts) < 5:
            continue
        sym, period, trades, wins, net = parts[:5]
        try:
            out[(sym, period)] = (int(trades), int(net))
        except ValueError:
            continue
    return out


def trailing_net(data, sym: str, quarters: list[str], end_idx: int, lookback: int = 4) -> tuple[int, int]:
    """Return (sum_net, quarters_with_data) over the lookback window ending at end_idx (inclusive)."""
    start = max(0, end_idx - lookback + 1)
    total = 0
    n_active = 0
    for i in range(start, end_idx + 1):
        q = quarters[i]
        td = data.get((sym, q))
        if td and td[0] > 0:  # had trades this quarter
            total += td[1]
            n_active += 1
    return total, n_active


def main() -> None:
    if len(sys.argv) < 2:
        print("usage: rolling_shortlist.py <quarterly.tsv>", file=sys.stderr)
        sys.exit(1)

    data = load(Path(sys.argv[1]))
    if not data:
        print("no rows parsed", file=sys.stderr)
        sys.exit(1)

    symbols = sorted({s for s, _ in data.keys()})
    quarters = all_quarters(2020, 2025, 1)
    n_quarters = len(quarters)  # 21

    LOOKBACK = 4
    MIN_ACTIVE = 2
    SHORTLIST_K = 32

    # First rebalance occurs at quarters[LOOKBACK] = 2021-Q1.
    # For each rebalance, compute the shortlist using trailing data ending at
    # quarters[i-1], then evaluate forward PnL on quarters[i].
    eval_start = LOOKBACK   # index of first forward quarter

    rolling_total = 0
    deployed_total = 0
    all57_total = 0
    rolling_n_picks = []
    rolling_membership_per_q: list[set[str]] = []
    recoveries = {"ARBUSDT", "ATOMUSDT", "BLURUSDT", "BNBUSDT", "GMXUSDT",
                  "HBARUSDT", "IOTAUSDT", "LDOUSDT", "OPUSDT", "PYTHUSDT",
                  "SEIUSDT", "SUIUSDT", "TIAUSDT", "WLDUSDT", "ZILUSDT"}
    rolling_recovery_capture = 0  # forward PnL contribution from recoveries
    fixed_recovery_capture = 0
    n_recovery_picks_per_q = []

    for i in range(eval_start, n_quarters):
        # Build rolling shortlist from trailing 4 quarters ending at i-1.
        candidates = []
        for s in symbols:
            tn, n_active = trailing_net(data, s, quarters, i - 1, LOOKBACK)
            if n_active >= MIN_ACTIVE and tn > 0:
                candidates.append((tn, s))
        candidates.sort(reverse=True)
        rolling_pick = {s for _, s in candidates[:SHORTLIST_K]}
        rolling_membership_per_q.append(rolling_pick)
        rolling_n_picks.append(len(rolling_pick))

        fwd_q = quarters[i]
        # Rolling forward PnL
        for s in rolling_pick:
            td = data.get((s, fwd_q))
            if td:
                rolling_total += td[1]
                if s in recoveries:
                    rolling_recovery_capture += td[1]

        # Deployed forward PnL
        for s in DEPLOYED_32:
            td = data.get((s, fwd_q))
            if td:
                deployed_total += td[1]
                if s in recoveries:
                    fixed_recovery_capture += td[1]

        # All-57 forward PnL
        for s in symbols:
            td = data.get((s, fwd_q))
            if td:
                all57_total += td[1]

        n_recovery_picks_per_q.append(len(rolling_pick & recoveries))

    forward_quarters = quarters[eval_start:]
    period_years = len(forward_quarters) / 4.0

    print()
    print("=" * 86)
    print("  ROLLING-SHORTLIST ANALYSIS")
    print(f"  Per-quarter source: {sys.argv[1]}")
    print(f"  Lookback: {LOOKBACK} quarters (12 mo)   Rebalance: quarterly   Shortlist: top-{SHORTLIST_K}")
    print(f"  Forward window: {forward_quarters[0]} → {forward_quarters[-1]}  ({period_years:.2f} yr)")
    print("=" * 86)
    print()

    print(f"  {'Selection rule':<40} {'forward NET':>14} {'$/yr':>12}")
    print(f"  {'-'*40} {'-'*14} {'-'*12}")
    for label, total in [
        ("rolling shortlist (trailing 4Q, top-32)", rolling_total),
        ("deployed-32 (fixed)",                     deployed_total),
        ("all-57 (no selection)",                   all57_total),
    ]:
        print(f"  {label:<40} ${total:>+13,} ${total/period_years:>+11,.0f}")
    print()

    print(f"  Rolling shortlist size per rebalance:")
    print(f"    min/median/max: {min(rolling_n_picks)} / "
          f"{sorted(rolling_n_picks)[len(rolling_n_picks)//2]} / {max(rolling_n_picks)}")
    print()

    print(f"  Recoveries-cohort capture")
    print(f"  {'-'*82}")
    print(f"    Rolling shortlist captured ${rolling_recovery_capture:>+10,} "
          f"from recoveries (vs deployed-32: ${fixed_recovery_capture:>+10,})")
    print(f"    Recoveries appearing in rolling pick per quarter:")
    for fq, n in zip(forward_quarters, n_recovery_picks_per_q):
        bar = "█" * n
        print(f"      {fq}: {n:>2}  {bar}")
    print()

    # Membership stability — fraction of shortlist that turns over each quarter
    print(f"  Shortlist turnover (Jaccard distance) between consecutive rebalances")
    print(f"  {'-'*82}")
    if len(rolling_membership_per_q) >= 2:
        for i in range(1, min(len(rolling_membership_per_q), 8)):
            a = rolling_membership_per_q[i - 1]
            b = rolling_membership_per_q[i]
            jaccard = len(a & b) / max(len(a | b), 1)
            print(f"    {forward_quarters[i-1]} → {forward_quarters[i]}: "
                  f"{len(a & b):>2}/{len(a | b):>2} stable  ({jaccard*100:.0f}% similarity)")
    print()

    print(f"  VERDICT")
    print(f"  {'-'*82}")
    delta = rolling_total - deployed_total
    pct = 100.0 * delta / max(abs(deployed_total), 1)
    if delta > 0:
        print(f"    Rolling shortlist beats deployed-32 by ${delta:+,} "
              f"({pct:+.1f}%) over {period_years:.2f}yr.")
        print(f"    Annualised improvement: ${delta/period_years:+,.0f}/yr.")
        print(f"    The recoveries cohort contributed ${rolling_recovery_capture - fixed_recovery_capture:+,} "
              f"of that delta.")
    else:
        print(f"    Rolling shortlist UNDERPERFORMS deployed-32 by ${-delta:,} "
              f"({pct:+.1f}%) over {period_years:.2f}yr.")
        print(f"    Conclusion: recoveries-cohort capture does not offset turnover noise / "
              f"momentum-chasing losses.")
    print()


if __name__ == "__main__":
    main()
