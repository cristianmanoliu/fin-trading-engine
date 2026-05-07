#!/usr/bin/env python3
"""
hod_decompose.py — entry-hour-of-day decomposition of the deployed-16
candidate strategy (4H short EMA9/21 mh504 target_rr=6) over 5y of merged
1m data.

For a 4H signal_tf strategy entries can only happen at 6 UTC hours: 00, 04,
08, 12, 16, 20 — each maps to a different global trading session. This
script asks: does the strategy's edge concentrate in a specific session, or
is it uniform across all six?

Reads JSONL journals produced by scripts/hod_journals.sh under
results/hod_journals/<DATE>/ and produces a session-bucketed breakdown:
trade count, win-rate, sum NET, mean NET, % of total NET.

Usage:
  python3 scripts/hod_decompose.py [results/hod_journals/2026-05-07]
"""

from __future__ import annotations

import json
import math
import sys
from collections import defaultdict
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path

# Map UTC hour to a human-readable session label. Crypto is 24/7 but the
# big liquidity / participant transitions still cluster around the legacy
# Asian / EU / US sessions.
SESSION_LABEL = {
    0:  "Asian open",       # 00:00-04:00 UTC
    4:  "Asian midday",     # 04:00-08:00
    8:  "London open",      # 08:00-12:00
    12: "NY pre-market",    # 12:00-16:00
    16: "NY midday",        # 16:00-20:00
    20: "NY close / Asia evening",  # 20:00-00:00
}


@dataclass
class Trade:
    symbol: str
    entry_hour: int
    entry_year: int
    side: str
    pnl_usd: float
    outcome: str  # TARGET | STOP | PARTIAL


def load_journals(journal_dir: Path) -> list[Trade]:
    """Pair open/close events in stream order per file. Backtest journals
    have close events written immediately after the open they correspond to,
    so a per-file FIFO pairing is exact (no signal multiplexing in backtest)."""
    trades: list[Trade] = []
    for jf in sorted(journal_dir.glob("*-*.jsonl")):
        symbol = jf.stem.split("-")[0]
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
                    ts_utc = ts.astimezone(timezone.utc)
                    trades.append(Trade(
                        symbol=symbol,
                        entry_hour=ts_utc.hour,
                        entry_year=ts_utc.year,
                        side=open_event["side"],
                        pnl_usd=ev.get("pnl_usd", 0.0),
                        outcome=ev.get("outcome", "?"),
                    ))
                    open_event = None
    return trades


def wilson_ci(wins: int, n: int, z: float = 1.96) -> tuple[float, float]:
    """Wilson 95% confidence interval for a Bernoulli proportion. Reasonable
    even at small n where the normal approximation breaks down."""
    if n == 0:
        return (0.0, 0.0)
    p = wins / n
    denom = 1 + z**2 / n
    centre = (p + z**2 / (2 * n)) / denom
    margin = z * math.sqrt((p * (1 - p) + z**2 / (4 * n)) / n) / denom
    return (max(0.0, centre - margin), min(1.0, centre + margin))


def main() -> int:
    if len(sys.argv) > 1:
        journal_dir = Path(sys.argv[1])
    else:
        # default: latest dated dir under results/hod_journals/
        root = Path(__file__).parent.parent / "results" / "hod_journals"
        candidates = sorted([p for p in root.iterdir() if p.is_dir()])
        if not candidates:
            print(f"no journal dirs under {root} — run scripts/hod_journals.sh first")
            return 1
        journal_dir = candidates[-1]

    print(f"reading journals from {journal_dir}")
    trades = load_journals(journal_dir)
    if not trades:
        print("no trades found")
        return 1

    total_n = len(trades)
    total_wins = sum(1 for t in trades if t.pnl_usd > 0)
    total_net = sum(t.pnl_usd for t in trades)
    overall_wr = total_wins / total_n
    print(f"loaded {total_n} trades from {len(set(t.symbol for t in trades))} symbols")
    print(f"overall: {total_wins}/{total_n} wins ({overall_wr*100:.1f}% WR) | NET ${total_net:,.0f}")
    print()

    # Bucket by entry hour-of-day.
    by_hour: dict[int, list[Trade]] = defaultdict(list)
    for t in trades:
        by_hour[t.entry_hour].append(t)

    # Header.
    print(f"{'hour':>5}  {'session':<26}  {'n':>5}  {'wins':>5}  "
          f"{'WR%':>6}  {'WR 95%CI':>14}  {'NET $':>12}  {'mean $':>9}  "
          f"{'%total':>7}  {'sigma_z':>8}")
    print(f"{'-'*5}  {'-'*26}  {'-'*5}  {'-'*5}  {'-'*6}  {'-'*14}  {'-'*12}  "
          f"{'-'*9}  {'-'*7}  {'-'*8}")

    rows = []
    for hour in sorted(by_hour.keys()):
        bucket = by_hour[hour]
        n = len(bucket)
        wins = sum(1 for t in bucket if t.pnl_usd > 0)
        net = sum(t.pnl_usd for t in bucket)
        wr = wins / n
        ci_lo, ci_hi = wilson_ci(wins, n)
        # z-score of bucket WR vs overall WR (Bernoulli SE).
        se = math.sqrt(overall_wr * (1 - overall_wr) / n) if n > 0 else 0.0
        z = (wr - overall_wr) / se if se > 0 else 0.0
        share = (net / total_net * 100) if total_net != 0 else 0.0
        rows.append((hour, n, wins, wr, ci_lo, ci_hi, net, net / n, share, z))

    for r in rows:
        hour, n, wins, wr, ci_lo, ci_hi, net, mean, share, z = r
        print(f"{hour:>5}  {SESSION_LABEL[hour]:<26}  {n:>5}  {wins:>5}  "
              f"{wr*100:>5.1f}%  [{ci_lo*100:>4.1f}, {ci_hi*100:>4.1f}]  "
              f"${net:>11,.0f}  ${mean:>8,.0f}  {share:>6.1f}%  {z:>+8.2f}")

    print()
    print("interpretation:")
    print(f"  - WR 95%CI overlapping the overall WR ({overall_wr*100:.1f}%) means the bucket is statistically indistinguishable from the average.")
    print(f"  - sigma_z is z-score of bucket WR vs overall WR; |z|>2 ≈ 5% one-bucket significance, |z|>2.64 ≈ Bonferroni-corrected 5% across 6 buckets.")
    print(f"  - %total NET = bucket's contribution to total NET PnL. If one bucket carries >40% of NET on <20% of trades, that's regime concentration.")

    # Concentration check.
    concentrated = [(h, share) for (h, n, w, wr, lo, hi, net, mean, share, z) in rows
                    if abs(share) > 40]
    if concentrated:
        print()
        print("⚠ CONCENTRATION FLAG: bucket(s) carrying >40% of total NET:")
        for h, share in concentrated:
            print(f"    hour {h:02d}:00 ({SESSION_LABEL[h]}) — {share:+.1f}% of total NET")

    # Bonferroni-significant buckets.
    sig = [(h, wr, z) for (h, n, w, wr, lo, hi, net, mean, share, z) in rows
           if abs(z) > 2.64]
    if sig:
        print()
        print("⚠ BONFERRONI-SIGNIFICANT bucket(s) (|z| > 2.64, 5% corrected for 6 tests):")
        for h, wr, z in sig:
            direction = "above" if z > 0 else "below"
            print(f"    hour {h:02d}:00 ({SESSION_LABEL[h]}) — WR {wr*100:.1f}% ({direction} overall by z={z:+.2f}σ)")
    else:
        print()
        print("✓ no Bonferroni-significant differences in WR by entry hour (|z| ≤ 2.64 for all buckets).")

    # ── Per-symbol concentration check ───────────────────────────────────────
    # Goal: test whether the strongest+weakest hours' effects are uniform
    # across the 16 symbols, or carried by 1-2 outlier symbols (artifact).
    print()
    print("=" * 80)
    print("PER-SYMBOL × HOUR — NET $ contribution per (symbol, hour) bucket")
    print("=" * 80)
    print(f"{'symbol':<13} | " + "  ".join(f"{h:>02d}h" + " " * 6 for h in sorted(by_hour.keys())) + " | total")
    print("-" * 100)
    by_sym_hour: dict[tuple[str, int], float] = defaultdict(float)
    for t in trades:
        by_sym_hour[(t.symbol, t.entry_hour)] += t.pnl_usd
    by_sym_total: dict[str, float] = defaultdict(float)
    for t in trades:
        by_sym_total[t.symbol] += t.pnl_usd
    symbols_sorted = sorted(by_sym_total.keys(), key=lambda s: -by_sym_total[s])
    for sym in symbols_sorted:
        cells = []
        for h in sorted(by_hour.keys()):
            v = by_sym_hour[(sym, h)]
            cells.append(f"${v:>+8,.0f}")
        total = by_sym_total[sym]
        print(f"{sym:<13} | " + "  ".join(cells) + f" | ${total:>+10,.0f}")

    # Count how many symbols had positive contribution from 16:00 vs 12:00.
    # A real session effect should show up in MOST symbols, not just a few outliers.
    print()
    print("symbols-with-positive-NET-in-bucket counts (n=16):")
    for h in sorted(by_hour.keys()):
        positive = sum(1 for sym in symbols_sorted if by_sym_hour[(sym, h)] > 0)
        negative = sum(1 for sym in symbols_sorted if by_sym_hour[(sym, h)] < 0)
        zero = 16 - positive - negative
        print(f"  hour {h:02d}:00 ({SESSION_LABEL[h]:<26}) | +{positive}  -{negative}  0:{zero}")

    # ── Per-year persistence check ───────────────────────────────────────────
    # Goal: test whether the 16:00 effect (if any) persists across years, or
    # whether it's concentrated in a single high-vol period (e.g. 2022 bear).
    print()
    print("=" * 80)
    print("PER-YEAR × HOUR — NET $ per (year, hour) bucket")
    print("=" * 80)
    years = sorted(set(t.entry_year for t in trades))
    print(f"{'year':<6} | " + "  ".join(f"{h:>02d}h" + " " * 6 for h in sorted(by_hour.keys())) + " | total")
    print("-" * 100)
    by_year_hour: dict[tuple[int, int], float] = defaultdict(float)
    by_year_total: dict[int, float] = defaultdict(float)
    for t in trades:
        by_year_hour[(t.entry_year, t.entry_hour)] += t.pnl_usd
        by_year_total[t.entry_year] += t.pnl_usd
    for yr in years:
        cells = []
        for h in sorted(by_hour.keys()):
            v = by_year_hour[(yr, h)]
            cells.append(f"${v:>+8,.0f}")
        total = by_year_total[yr]
        print(f"{yr:<6} | " + "  ".join(cells) + f" | ${total:>+10,.0f}")

    # Count years where 16:00 was the best hour, and where 12:00 was the worst.
    print()
    print("hour-rank consistency across years:")
    best_count = defaultdict(int)
    worst_count = defaultdict(int)
    for yr in years:
        hour_pnls = {h: by_year_hour[(yr, h)] for h in sorted(by_hour.keys())}
        best_h = max(hour_pnls, key=hour_pnls.get)
        worst_h = min(hour_pnls, key=hour_pnls.get)
        best_count[best_h] += 1
        worst_count[worst_h] += 1
    print(f"  best-hour-of-year tally: " + ", ".join(
        f"{h:02d}h:{best_count[h]}" for h in sorted(by_hour.keys()) if best_count[h] > 0))
    print(f"  worst-hour-of-year tally: " + ", ".join(
        f"{h:02d}h:{worst_count[h]}" for h in sorted(by_hour.keys()) if worst_count[h] > 0))

    return 0


if __name__ == "__main__":
    sys.exit(main())
