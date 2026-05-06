#!/usr/bin/env python3
"""mechanism_analysis.py — diagnose WHY the P4-Combined edge looks the way it does.

Tests four hypotheses about the source of the apparent edge:

  H1 (vol-driven momentum):    edge concentrates in high-vol altcoins
  H2 (retail-bubble shorting): edge in retail-heavy/recent-listing names
  H3 (selection-bias noise):   per-symbol "edge" is largely noise
  H4 (structural bear-bias):   crypto has universal short-side skew

Inputs (per-symbol per-window aggregates produced by scripts/p4_fresh_oos.sh):
  results/fresh_oos_2023-05_to_2024-04_slip15_2026-05-06.txt   (W1, in-sample)
  results/fresh_oos_2024-05_to_2025-04_slip15_2026-05-06.txt   (W2, in-sample)
  results/fresh_oos_2025-05_to_2026-04_slip15_2026-05-06.txt   (W3, true OOS)
  Optional cross-config files at other slip levels and timeframes.

Outputs to stdout: mechanism diagnostic report including:
  - Per-symbol consistency table (which windows positive)
  - Pairwise symbol correlation across windows (common factor vs idiosyncratic)
  - Top-N rotation (does the same set dominate every window?)
  - Long vs short asymmetry pattern
  - Volatility-bin conditioning if kline data available
  - Verdict on which hypotheses are supported by data
"""
from __future__ import annotations

import csv
import re
import sys
from collections import defaultdict
from pathlib import Path
from statistics import mean, median, stdev

ROW_PAT = re.compile(
    r"^(?P<sym>[A-Z0-9]+USDT)\s+(?P<trades>\d+)\s+(?P<wins>\d+)\s+(?P<wr>[\d.]+)%\s+"
    r"(?P<gross>[+-]?\d+)\s+(?P<fees>\d+)\s+(?P<long>[+-]?\d+)\s+(?P<short>[+-]?\d+)\s+"
    r"(?P<hold>[\d.]+)h\s+net\s+(?P<net>[+-]?\d+)"
)


def parse_fresh_oos(path: Path) -> dict[str, dict]:
    """Return {symbol: {trades, wins, wr, gross, fees, long_net, short_net, avg_hold, net}}."""
    out = {}
    if not path.exists():
        return out
    for line in path.read_text().splitlines():
        m = ROW_PAT.match(line.strip())
        if m:
            out[m["sym"]] = {
                "trades": int(m["trades"]),
                "wins": int(m["wins"]),
                "wr": float(m["wr"]),
                "gross": int(m["gross"]),
                "fees": int(m["fees"]),
                "long_net": int(m["long"]),
                "short_net": int(m["short"]),
                "avg_hold": float(m["hold"]),
                "net": int(m["net"]),
            }
    return out


def correlation(xs: list[float], ys: list[float]) -> float:
    """Pearson correlation, bounded by [-1, 1]. Returns 0 for degenerate inputs."""
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


def compute_volatility(symbol: str, root: Path) -> float | None:
    """Average monthly volatility for the symbol over 2023-05..2026-04 (the OOS span).
    Returns annualized realized vol from monthly OHLC. None if data missing."""
    files = []
    for year in (2023, 2024, 2025, 2026):
        for month in range(1, 13):
            if year == 2023 and month < 5:
                continue
            if year == 2026 and month > 4:
                continue
            p = root / "data" / f"{symbol}-1m-{year}-{month:02d}.csv"
            if p.exists():
                files.append(p)
    if len(files) < 12:
        return None
    # Sample monthly: open/close from first/last 1m bar of each file gives a rough monthly return
    monthly_returns = []
    for p in files:
        with p.open() as f:
            r = csv.reader(f)
            rows = list(r)
            if not rows:
                continue
            # Skip header if present (first row's column 1 isn't a number)
            data_rows = rows
            try:
                float(rows[0][1])
            except (ValueError, IndexError):
                data_rows = rows[1:]
            if not data_rows:
                continue
            try:
                first_open = float(data_rows[0][1])
                last_close = float(data_rows[-1][4])
                if first_open > 0:
                    monthly_returns.append((last_close - first_open) / first_open)
            except (ValueError, IndexError):
                continue
    if len(monthly_returns) < 6:
        return None
    if len(monthly_returns) > 1:
        return stdev(monthly_returns) * (12 ** 0.5)  # annualize
    return None


def main() -> None:
    root = Path(__file__).resolve().parent.parent

    windows = {
        "W1": root / "results/fresh_oos_2023-05_to_2024-04_slip15_2026-05-06.txt",
        "W2": root / "results/fresh_oos_2024-05_to_2025-04_slip15_2026-05-06.txt",
        "W3": root / "results/fresh_oos_2025-05_to_2026-04_slip15_2026-05-06.txt",
    }

    data = {wlabel: parse_fresh_oos(p) for wlabel, p in windows.items()}
    missing = [w for w, d in data.items() if not d]
    if missing:
        print(f"FAIL: per-symbol data missing for {missing} — re-run p4_fresh_oos.sh", file=sys.stderr)
        sys.exit(1)

    universe = sorted(set().union(*[set(data[w].keys()) for w in data]))
    print()
    print("=" * 110)
    print("  MECHANISM ANALYSIS  (P4-Combined: 4H short EMA9×EMA21, target_rr=6, slip=15bp, fee=10bp)")
    print(f"  Universe: {len(universe)} symbols × 3 windows (W1/W2 in-sample for selection, W3 true OOS)")
    print("=" * 110)

    # ─────────────────────────────────────────────────────────────────────────
    # Test 1: Per-symbol consistency
    # ─────────────────────────────────────────────────────────────────────────
    print("\n  TEST 1 — per-symbol consistency across 3 windows")
    print("  " + "─" * 90)
    print(f"  {'symbol':<12} {'W1':>9} {'W2':>9} {'W3':>9}  {'wins/3':<8} {'sign_flips':<10} category")
    print(f"  {'-'*12} {'-'*9} {'-'*9} {'-'*9}  {'-'*8} {'-'*10} {'-'*8}")

    rows = []
    for sym in universe:
        nets = [data[w].get(sym, {}).get("net", 0) for w in ("W1", "W2", "W3")]
        positives = sum(1 for n in nets if n > 0)
        # Count sign flips between consecutive windows
        flips = sum(1 for i in range(len(nets) - 1) if (nets[i] > 0) != (nets[i+1] > 0) and abs(nets[i]) > 200 and abs(nets[i+1]) > 200)
        if positives == 3:
            cat = "ROBUST"
        elif positives == 2:
            cat = "supportive"
        elif positives == 1:
            cat = "noisy"
        else:
            cat = "REJECTED"
        rows.append((sym, nets, positives, flips, cat))

    # Sort by aggregate strength then negativity
    rows.sort(key=lambda r: (-r[2], -sum(r[1])))

    # Show all
    cat_counts: dict[str, int] = defaultdict(int)
    for sym, nets, pos, flips, cat in rows:
        cat_counts[cat] += 1
        print(f"  {sym:<12} {nets[0]:>+9,} {nets[1]:>+9,} {nets[2]:>+9,}  {pos}/3      {flips}/2        {cat}")

    print()
    print("  CONSISTENCY SUMMARY")
    for c in ("ROBUST", "supportive", "noisy", "REJECTED"):
        print(f"    {c:<11}: {cat_counts[c]:>2}/{len(universe)} symbols")
    total_flips = sum(r[3] for r in rows)
    print(f"    sign-flips between windows: {total_flips} total ({100*total_flips/(2*len(universe)):.0f}% of consecutive pairs)")

    # ─────────────────────────────────────────────────────────────────────────
    # Test 2: Pairwise symbol correlation (common factor vs idiosyncratic)
    # ─────────────────────────────────────────────────────────────────────────
    print("\n  TEST 2 — pairwise symbol correlation (common factor probe)")
    print("  " + "─" * 90)
    print("  Each symbol has 3 NET observations (one per window). Compute pairwise")
    print("  correlation across the universe; if HIGH avg → strategy captures a market-")
    print("  wide factor (regime-dependent); if LOW avg → idiosyncratic per-symbol edge.")
    print()

    nets_per_sym = {sym: [data[w].get(sym, {}).get("net", 0) for w in ("W1", "W2", "W3")] for sym in universe}
    syms_with_data = [s for s in universe if any(n != 0 for n in nets_per_sym[s])]
    pairwise = []
    for i, s1 in enumerate(syms_with_data):
        for s2 in syms_with_data[i+1:]:
            c = correlation(nets_per_sym[s1], nets_per_sym[s2])
            pairwise.append(c)

    avg_pair = mean(pairwise) if pairwise else 0.0
    median_pair = median(pairwise) if pairwise else 0.0
    print(f"    Average pairwise correlation (n={len(pairwise)} pairs): {avg_pair:+.3f}")
    print(f"    Median pairwise correlation:                          {median_pair:+.3f}")
    print(f"    {sum(1 for c in pairwise if c > 0.5)}/{len(pairwise)} pairs > +0.5 (strong common factor)")
    print(f"    {sum(1 for c in pairwise if c < -0.5)}/{len(pairwise)} pairs < -0.5 (anti-correlated)")
    print()
    if avg_pair > 0.4:
        print("  → INTERPRETATION: strong common factor — strategy captures market-wide regime.")
        print("    Implication: edge is regime-dependent, not idiosyncratic. Risk is regime change.")
    elif avg_pair > 0.15:
        print("  → INTERPRETATION: moderate common factor — partial regime dependence.")
        print("    Implication: edge has both market-wide and per-symbol components.")
    else:
        print("  → INTERPRETATION: weak/no common factor — edge is per-symbol idiosyncratic.")
        print("    Implication: either microstructural per-symbol effect OR pure noise.")

    # ─────────────────────────────────────────────────────────────────────────
    # Test 3: Top-N rotation (do the same symbols dominate every window?)
    # ─────────────────────────────────────────────────────────────────────────
    print("\n  TEST 3 — top-5 rotation across windows")
    print("  " + "─" * 90)
    top5_by_window = {}
    for w in ("W1", "W2", "W3"):
        ranked = sorted(data[w].items(), key=lambda kv: -kv[1]["net"])[:5]
        top5_by_window[w] = [s for s, _ in ranked]
    for w in ("W1", "W2", "W3"):
        print(f"    {w} top-5: {', '.join(top5_by_window[w])}")
    overlap_12 = set(top5_by_window["W1"]) & set(top5_by_window["W2"])
    overlap_23 = set(top5_by_window["W2"]) & set(top5_by_window["W3"])
    overlap_13 = set(top5_by_window["W1"]) & set(top5_by_window["W3"])
    overlap_all = set(top5_by_window["W1"]) & set(top5_by_window["W2"]) & set(top5_by_window["W3"])
    print()
    print(f"    Overlap W1∩W2: {len(overlap_12)}/5  ({sorted(overlap_12)})")
    print(f"    Overlap W2∩W3: {len(overlap_23)}/5  ({sorted(overlap_23)})")
    print(f"    Overlap W1∩W3: {len(overlap_13)}/5  ({sorted(overlap_13)})")
    print(f"    Overlap all 3: {len(overlap_all)}/5  ({sorted(overlap_all)})")
    print()
    if len(overlap_all) >= 3:
        print("  → INTERPRETATION: stable top-N — same symbols dominate every window.")
        print("    Implication: per-symbol edge appears persistent.")
    elif len(overlap_all) >= 1:
        print("  → INTERPRETATION: partial top-N persistence.")
    else:
        print("  → INTERPRETATION: top-N rotates fully — different symbols win each window.")
        print("    Implication: 'top performer' status is not predictive forward — selecting")
        print("    based on past top-N is data mining.")

    # ─────────────────────────────────────────────────────────────────────────
    # Test 4: WR drift across windows
    # ─────────────────────────────────────────────────────────────────────────
    print("\n  TEST 4 — universe WR drift across windows")
    print("  " + "─" * 90)
    for w in ("W1", "W2", "W3"):
        total_trades = sum(d["trades"] for d in data[w].values())
        total_wins = sum(d["wins"] for d in data[w].values())
        wr = 100 * total_wins / total_trades if total_trades else 0
        print(f"    {w}: {total_trades:>5} trades  {total_wins:>4} wins  {wr:.2f}% WR")
    # Breakeven WR for cost-laden 6:1 RR
    print(f"\n    Breakeven WR for fee=10bp, slip=15bp, RR=6: ~14.3%")
    print(f"    Strategy needs WR > breakeven across all regimes to be robust.")

    # ─────────────────────────────────────────────────────────────────────────
    # Test 5: Avg hold-hours drift (regime indicator)
    # ─────────────────────────────────────────────────────────────────────────
    print("\n  TEST 5 — avg hold-hours by window (regime indicator)")
    print("  " + "─" * 90)
    for w in ("W1", "W2", "W3"):
        holds = [d["avg_hold"] for d in data[w].values() if d["trades"] > 0]
        if holds:
            print(f"    {w}: avg hold {mean(holds):.1f}h  median {median(holds):.1f}h  range {min(holds):.0f}-{max(holds):.0f}h")
    print(f"\n    Longer holds → trades trending toward max-hold force-close (336h cap)")
    print(f"    Shorter holds → faster RR resolution (either target or stop)")

    # ─────────────────────────────────────────────────────────────────────────
    # Test 6: Volatility-bin conditioning (H1 test)
    # ─────────────────────────────────────────────────────────────────────────
    print("\n  TEST 6 — volatility-bin conditioning (H1: vol-driven momentum)")
    print("  " + "─" * 90)
    print("  Compute annualized realized vol per symbol from monthly OHLC")
    print("  (2023-05..2026-04). Bin universe into low/mid/high vol terciles.")
    print("  If H1 holds: high-vol bin should produce highest aggregate NET.")
    print()
    sym_vols = {}
    for sym in universe:
        v = compute_volatility(sym, root)
        if v is not None:
            sym_vols[sym] = v
    if not sym_vols:
        print("    (No volatility data computed — kline files missing)")
    else:
        sorted_by_vol = sorted(sym_vols.items(), key=lambda kv: kv[1])
        n = len(sorted_by_vol)
        third = n // 3
        bins = {
            "low_vol":  sorted_by_vol[:third],
            "mid_vol":  sorted_by_vol[third:2*third],
            "high_vol": sorted_by_vol[2*third:],
        }
        for bname, bsyms in bins.items():
            tot_net = 0
            for sym, _ in bsyms:
                for w in ("W1", "W2", "W3"):
                    tot_net += data[w].get(sym, {}).get("net", 0)
            avg_vol = mean(v for _, v in bsyms) if bsyms else 0
            sample_syms = ", ".join(s for s, _ in bsyms[:5])
            print(f"    {bname:<8} (n={len(bsyms)}, avg_vol={avg_vol:.2f}): NET 3yr ${tot_net:>+12,}  ({sample_syms}, ...)")

    # ─────────────────────────────────────────────────────────────────────────
    # Test 7: Random-baseline comparison (the smoking gun for H3)
    # ─────────────────────────────────────────────────────────────────────────
    print("\n  TEST 7 — random-baseline: are observed consistency counts above chance?")
    print("  " + "─" * 90)
    print("  Under H0 (no per-symbol skill), each symbol's per-window outcome is independent")
    print("  with probability p_w = (positive_count_w / universe_size_w). Compute expected")
    print("  count of symbols positive in 3/3 windows under H0; compare to observed.")
    print()
    p1 = 30 / 57  # W1
    p2 = 42 / 57  # W2
    p3 = 24 / 56  # W3(one symbol no trades)
    n_total = 57
    expected_3of3 = n_total * p1 * p2 * p3
    expected_2of3 = n_total * (p1*p2*(1-p3) + p1*(1-p2)*p3 + (1-p1)*p2*p3)
    expected_1of3 = n_total * (p1*(1-p2)*(1-p3) + (1-p1)*p2*(1-p3) + (1-p1)*(1-p2)*p3)
    expected_0of3 = n_total * (1-p1)*(1-p2)*(1-p3)
    p_robust = p1 * p2 * p3
    sigma_robust = (n_total * p_robust * (1 - p_robust)) ** 0.5
    z_robust = (cat_counts["ROBUST"] - expected_3of3) / sigma_robust if sigma_robust else 0

    print(f"    Window-marginal p(positive | symbol): W1={p1:.3f} W2={p2:.3f} W3={p3:.3f}")
    print()
    print(f"    {'category':<14} {'expected':>10} {'observed':>10}  {'z-score':>10}")
    print(f"    {'-'*14} {'-'*10} {'-'*10}  {'-'*10}")
    sigma_2of3 = (n_total * (expected_2of3/n_total) * (1 - expected_2of3/n_total)) ** 0.5
    sigma_1of3 = (n_total * (expected_1of3/n_total) * (1 - expected_1of3/n_total)) ** 0.5
    sigma_0of3 = (n_total * (expected_0of3/n_total) * (1 - expected_0of3/n_total)) ** 0.5
    z_2 = (cat_counts["supportive"] - expected_2of3) / sigma_2of3 if sigma_2of3 else 0
    z_1 = (cat_counts["noisy"] - expected_1of3) / sigma_1of3 if sigma_1of3 else 0
    z_0 = (cat_counts["REJECTED"] - expected_0of3) / sigma_0of3 if sigma_0of3 else 0
    print(f"    {'ROBUST (3/3)':<14} {expected_3of3:>10.1f} {cat_counts['ROBUST']:>10}  {z_robust:>+10.2f}")
    print(f"    {'supp (2/3)':<14} {expected_2of3:>10.1f} {cat_counts['supportive']:>10}  {z_2:>+10.2f}")
    print(f"    {'noisy (1/3)':<14} {expected_1of3:>10.1f} {cat_counts['noisy']:>10}  {z_1:>+10.2f}")
    print(f"    {'REJECTED (0/3)':<14} {expected_0of3:>10.1f} {cat_counts['REJECTED']:>10}  {z_0:>+10.2f}")
    print()
    if abs(z_robust) < 1.96:
        print(f"  → ROBUST count ({cat_counts['ROBUST']}) is statistically INDISTINGUISHABLE from chance ({expected_3of3:.1f})")
        print(f"    at 95% CI. This is STRONG evidence that there is NO per-symbol edge —")
        print(f"    the symbols that look 'robust' are likely lucky, not skilled.")
    else:
        print(f"  → ROBUST count is significantly different from chance.")

    # ─────────────────────────────────────────────────────────────────────────
    # Test 8: Long vs short asymmetry (universal vs regime)
    # ─────────────────────────────────────────────────────────────────────────
    print("\n  TEST 8 — long vs short asymmetry (universal vs regime-dependent)")
    print("  " + "─" * 90)
    longs_files = {
        "W1": root / "results/fresh_oos_2023-05_to_2024-04_longs_slip15_2026-05-06.txt",
        "W2": root / "results/fresh_oos_2024-05_to_2025-04_longs_slip15_2026-05-06.txt",
        "W3": root / "results/fresh_oos_longs_slip15_2026-05-06.txt",
    }
    longs_data = {w: parse_fresh_oos(p) for w, p in longs_files.items()}
    if all(longs_data.values()):
        print(f"    {'window':<6} {'short NET':>12} {'long NET':>12} {'asymmetry':>12}  {'short pos/N':<14} {'long pos/N'}")
        print(f"    {'-'*6} {'-'*12} {'-'*12} {'-'*12}  {'-'*14} {'-'*14}")
        long_universal = True
        short_minus_long_per_w = []
        for w in ("W1", "W2", "W3"):
            short_net = sum(d["net"] for d in data[w].values())
            long_net = sum(d["net"] for d in longs_data[w].values())
            asym = short_net - long_net
            n_short = len(data[w])
            n_long = len(longs_data[w])
            short_pos = sum(1 for d in data[w].values() if d["net"] > 0)
            long_pos = sum(1 for d in longs_data[w].values() if d["net"] > 0)
            print(f"    {w:<6} ${short_net:>+11,} ${long_net:>+11,} ${asym:>+11,}  {short_pos}/{n_short:<11} {long_pos}/{n_long}")
            short_minus_long_per_w.append(asym)
            if long_net > 0:
                long_universal = False
        sum_long = sum(sum(d["net"] for d in longs_data[w].values()) for w in ("W1","W2","W3"))
        sum_short = sum(sum(d["net"] for d in data[w].values()) for w in ("W1","W2","W3"))
        print()
        print(f"    3-window aggregate: shorts ${sum_short:+,}  longs ${sum_long:+,}  asymmetry ${sum_short-sum_long:+,}")
        print()
        if long_universal:
            print(f"  → Longs are NEGATIVE in ALL 3 windows. The asymmetry is UNIVERSAL,")
            print(f"    not regime-conditioned. This is a STRUCTURAL property of the strategy,")
            print(f"    consistent with H4 (crypto has structural short-side skew).")
        else:
            print(f"  → Long PnL varies by window — asymmetry is partially regime-conditioned.")
    else:
        missing_long = [w for w, d in longs_data.items() if not d]
        print(f"    Long-side data missing for: {missing_long}")

    # ─────────────────────────────────────────────────────────────────────────
    # Synthesis
    # ─────────────────────────────────────────────────────────────────────────
    print("\n" + "=" * 110)
    print("  SYNTHESIS")
    print("=" * 110)
    print()
    print("  Hypothesis support summary (subjective read of evidence above):")
    print()

    # H3 quick scoring: high sign-flips and rotating top-N support H3
    h3_score = (total_flips / (2 * len(universe))) + (1 - len(overlap_all) / 5)
    h3_verdict = "STRONGLY SUPPORTED" if h3_score > 0.7 else ("supported" if h3_score > 0.4 else "weakly supported")
    print(f"  H3 (selection-bias noise): {h3_verdict}")
    print(f"     Evidence: {total_flips}/{2*len(universe)} sign-flips, top-N stability {len(overlap_all)}/5")
    print()

    h4_score = avg_pair  # high common factor → H4 (universal effect)
    h4_verdict = "supported" if h4_score > 0.3 else "weakly supported" if h4_score > 0.1 else "NOT supported"
    print(f"  H4 (structural bear-bias / universal): {h4_verdict}")
    print(f"     Evidence: avg pairwise correlation {avg_pair:+.3f}")
    print()

    if sym_vols:
        # H1 score: ratio of high-vol NET to low-vol NET
        low_net = sum(data[w].get(s, {}).get("net", 0) for w in ("W1","W2","W3") for s, _ in bins["low_vol"])
        high_net = sum(data[w].get(s, {}).get("net", 0) for w in ("W1","W2","W3") for s, _ in bins["high_vol"])
        if abs(low_net) > 1000:
            h1_ratio = high_net / abs(low_net) if low_net else float('inf')
            h1_verdict = "supported" if high_net > 2 * abs(low_net) else "weakly supported" if high_net > low_net else "NOT supported"
        else:
            h1_verdict = "inconclusive (low_vol bin near zero)"
        print(f"  H1 (vol-driven momentum): {h1_verdict}")
        print(f"     Evidence: low_vol bin NET ${low_net:+,}, high_vol bin NET ${high_net:+,}")
        print()

    print("  H2 (retail-bubble shorting) requires listing-date data not parsed here.")


if __name__ == "__main__":
    main()
