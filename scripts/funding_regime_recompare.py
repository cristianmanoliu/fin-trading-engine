#!/usr/bin/env python3
"""funding_regime_recompare.py — recompute funding-regime correlation pre/post
the funding-loader bug fix and report whether the regime story holds.

Inputs:
  results/p4_quarterly_slip15_full_2026-05-06.tsv     (PRE-fix, funding=$0)
  results/p4_quarterly_slip15_postfix_2026-05-06.tsv  (POST-fix, real funding)
  data/funding/*.csv                                   (per-symbol funding history)

Output: side-by-side comparison of Pearson r(funding_rate, NET) and quartile bins.
"""
from __future__ import annotations

import csv
import sys
from collections import defaultdict
from datetime import datetime, timezone
from pathlib import Path


def quarter_of(ts_ms: int) -> str:
    dt = datetime.fromtimestamp(ts_ms / 1000, tz=timezone.utc)
    q = (dt.month - 1) // 3 + 1
    return f"{dt.year}-Q{q}"


def load_quarterly(path: Path) -> dict[str, dict[str, int]]:
    """Returns {symbol: {period: net_usd}} excluding zero-trade cells."""
    out: dict[str, dict[str, int]] = defaultdict(dict)
    with path.open() as f:
        rdr = csv.reader(f, delimiter="\t")
        next(rdr)
        for row in rdr:
            if len(row) < 5:
                continue
            sym, period, trades, _, net = row
            if int(trades) > 0:
                out[sym][period] = int(net)
    return out


def load_funding_avg(funding_dir: Path, symbols: set[str]) -> dict[str, dict[str, float]]:
    """Returns {symbol: {period: avg_funding_rate_per_8h}}."""
    out: dict[str, dict[str, float]] = defaultdict(dict)
    for csv_path in funding_dir.glob("*.csv"):
        sym = csv_path.stem
        if sym not in symbols:
            continue
        quarter_rates: dict[str, list[float]] = defaultdict(list)
        with csv_path.open() as f:
            rdr = csv.reader(f)
            next(rdr)
            for row in rdr:
                if len(row) < 2:
                    continue
                try:
                    ts = int(row[0])
                    rate = float(row[1].strip('"'))
                except (ValueError, IndexError):
                    continue
                quarter_rates[quarter_of(ts)].append(rate)
        for q, rates in quarter_rates.items():
            if rates:
                out[sym][q] = sum(rates) / len(rates)
    return out


def pearson(xs: list[float], ys: list[float]) -> tuple[float, int, float]:
    n = len(xs)
    if n < 3:
        return 0.0, n, 0.0
    mx, my = sum(xs) / n, sum(ys) / n
    num = sum((x - mx) * (y - my) for x, y in zip(xs, ys))
    dx = (sum((x - mx) ** 2 for x in xs)) ** 0.5
    dy = (sum((y - my) ** 2 for y in ys)) ** 0.5
    if dx == 0 or dy == 0:
        return 0.0, n, 0.0
    r = num / (dx * dy)
    t = r * (n - 2) ** 0.5 / (1 - r * r) ** 0.5 if abs(r) < 1 else float("inf")
    return r, n, t


def correlation_block(label: str, data: dict[str, dict[str, int]], funding: dict[str, dict[str, float]]) -> None:
    pairs: list[tuple[float, int]] = []
    for sym in data:
        for q, net in data[sym].items():
            if q in funding.get(sym, {}):
                pairs.append((funding[sym][q], net))
    if not pairs:
        print(f"  {label}: no pairs"); return
    xs = [p[0] for p in pairs]
    ys = [float(p[1]) for p in pairs]
    r, n, t = pearson(xs, ys)
    print(f"  {label}")
    print(f"  {'─' * 70}")
    print(f"    n = {n}, Pearson r = {r:+.4f}, t = {t:+.2f}")
    if abs(t) > 3:
        sig = "HIGHLY SIGNIFICANT (p<0.001)"
    elif abs(t) > 2.58:
        sig = "significant (p<0.01)"
    elif abs(t) > 1.96:
        sig = "marginally significant (p<0.05)"
    else:
        sig = "NOT significant"
    print(f"    significance: {sig}")
    # Quartile bins
    pairs_sorted = sorted(pairs, key=lambda p: p[0])
    qsize = n // 4
    bins = [
        ("Q1 (most-negative funding)", pairs_sorted[:qsize]),
        ("Q2", pairs_sorted[qsize:2 * qsize]),
        ("Q3", pairs_sorted[2 * qsize:3 * qsize]),
        ("Q4 (most-positive funding)", pairs_sorted[3 * qsize:]),
    ]
    print(f"    {'bin':<28} {'n':>4} {'avg_funding':>15} {'avg_NET':>10} {'sum_NET':>13} {'pos%':>6}")
    for bname, bps in bins:
        if not bps:
            continue
        af = sum(p[0] for p in bps) / len(bps)
        an = sum(p[1] for p in bps) / len(bps)
        sn = sum(p[1] for p in bps)
        pos = sum(1 for p in bps if p[1] > 0)
        pct = 100 * pos / len(bps)
        print(f"    {bname:<28} {len(bps):>4} {af:>+15.6f} ${an:>+9,.0f} ${sn:>+12,} {pct:>5.1f}%")


def main() -> None:
    root = Path(__file__).resolve().parent.parent
    pre = root / "results/p4_quarterly_slip15_full_2026-05-06.tsv"
    post = root / "results/p4_quarterly_slip15_postfix_2026-05-06.tsv"
    funding_dir = root / "data/funding"
    if not pre.exists():
        print(f"FAIL: pre-fix TSV missing: {pre}", file=sys.stderr); sys.exit(1)
    if not post.exists():
        print(f"FAIL: post-fix TSV missing: {post}", file=sys.stderr); sys.exit(1)
    pre_data = load_quarterly(pre)
    post_data = load_quarterly(post)
    universe = set(pre_data) | set(post_data)
    funding = load_funding_avg(funding_dir, universe)

    print()
    print("=" * 110)
    print("  FUNDING-REGIME CORRELATION  (PRE-fix vs POST-fix funding-loader)")
    print(f"  Symbol-quarter cells: pre={sum(len(v) for v in pre_data.values())}, post={sum(len(v) for v in post_data.values())}")
    print("=" * 110)
    print()
    correlation_block("PRE-FIX  (funding=$0 due to quoted-rate ParseFloat bug)", pre_data, funding)
    print()
    correlation_block("POST-FIX (real Binance funding accrued)", post_data, funding)
    print()
    print("=" * 110)
    print("  INTERPRETATION")
    print("=" * 110)
    print()

    # Compute deltas
    pre_pairs: list[tuple[float, int]] = []
    post_pairs: list[tuple[float, int]] = []
    for sym in universe:
        pd = pre_data.get(sym, {})
        po = post_data.get(sym, {})
        for q in pd:
            if q in funding.get(sym, {}):
                pre_pairs.append((funding[sym][q], pd[q]))
        for q in po:
            if q in funding.get(sym, {}):
                post_pairs.append((funding[sym][q], po[q]))

    pre_r, _, pre_t = pearson([p[0] for p in pre_pairs], [float(p[1]) for p in pre_pairs])
    post_r, _, post_t = pearson([p[0] for p in post_pairs], [float(p[1]) for p in post_pairs])
    delta_r = post_r - pre_r

    print(f"  Pearson r shifted from {pre_r:+.4f} (PRE) to {post_r:+.4f} (POST), Δr = {delta_r:+.4f}")
    print()
    if post_r < -0.10 and abs(post_t) > 2.58:
        print("  → REGIME STORY HOLDS: even with real funding, NET is significantly negatively")
        print("    correlated with funding rate. The strategy IS regime-conditioned. The funding")
        print("    filter has a real motivation; worth wiring into cmd/engine and testing live.")
    elif -0.10 <= post_r < -0.05 and abs(post_t) > 1.96:
        print("  → REGIME STORY WEAKENED but persists. Strategy still has measurable regime")
        print("    sensitivity but funding income partially offsets the gross-PnL effect.")
        print("    Funding filter has weaker motivation; case-dependent.")
    elif -0.05 <= post_r <= 0.05:
        print("  → REGIME STORY DISSOLVED. Real funding income for shorts in bull regimes")
        print("    cancels the gross-PnL regime effect. The 'regime-conditioned strategy'")
        print("    interpretation was an artifact of the bug. Funding filter is dead — don't")
        print("    deploy it. Simpler model: strategy works ~uniformly across regimes once")
        print("    funding is correctly accrued.")
    elif post_r > 0.05:
        print("  → REGIME STORY FLIPPED. Post-fix correlation is POSITIVE — strategy now")
        print("    appears to BENEFIT from positive funding regimes (because funding income")
        print("    exceeds gross-PnL drag). This is unexpected; cross-check before publishing.")


if __name__ == "__main__":
    main()
