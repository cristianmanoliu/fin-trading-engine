#!/usr/bin/env python3
"""
b3_features.py — symbol-mechanism cross-validation.

Test whether structural features of each symbol (realized vol, funding regime,
liquidity tier, listing age) explain the deployed-16 vs rejected-41 split, OR
whether the split is a chance artifact of historical-performance ranking.

Inputs:
  results/b3_symbol_net_<date>.csv   (from scripts/b3_symbol_run.sh)
  data/<SYMBOL>-1m-YYYY-MM.csv       (per-symbol 1m kline CSVs, full universe)
  data/funding/<SYMBOL>.csv          (per-symbol Binance funding history)

For each symbol, computes:
  - vol_ann_pct        annualized daily-return σ × √365 × 100
  - mean_daily_vol_m   mean daily USD volume in millions
  - mean_price         mean close over the sample (liquidity-tier proxy)
  - listing_age_days   span from first to last 1m kline
  - daily_skew         skewness of daily log returns
  - daily_kurt         excess kurtosis of daily log returns
  - funding_mean_8h    mean 8h funding rate (decimal)
  - funding_pct_pos    fraction of 8h funding observations > 0
  - funding_std_8h     std of 8h funding rate

Then runs:
  1. Per-feature distributional comparison (Mann-Whitney U, Welch's t-test)
     between deployed-16 and rejected-41 — is any feature significantly
     different between the two groups?
  2. Pearson correlation of each feature with annual NET across all 57
     symbols — does any feature predict performance independent of the
     deployed/rejected label?
  3. Bonferroni-corrected significance across all features tested.

Output:
  results/b3_features_<date>.csv     per-symbol feature table joined to NET
  stdout                             ranked feature analysis + verdict

Usage:
  python3 scripts/b3_features.py [results/b3_symbol_net_2026-05-07.csv]
"""

from __future__ import annotations

import csv
import math
import statistics
import sys
from datetime import datetime, timezone
from pathlib import Path


def daily_aggregates(symbol: str, root: Path) -> list[tuple[datetime, float, float]]:
    """Walk per-month 1m CSVs for a symbol; return [(day_utc, last_close, volume_sum), ...].

    Trades off precision for speed: we only need daily-rollup stats here,
    so we don't keep OHLC — just the daily close and daily volume sum.
    """
    days: dict[str, tuple[datetime, float, float]] = {}
    for csv_path in sorted(root.glob(f"data/{symbol}-1m-*.csv")):
        with csv_path.open() as f:
            for line in f:
                parts = line.split(",")
                if len(parts) < 6:
                    continue
                try:
                    open_ms = int(parts[0])
                except ValueError:
                    continue
                ts = datetime.fromtimestamp(open_ms / 1000, tz=timezone.utc)
                day_key = ts.strftime("%Y-%m-%d")
                try:
                    close = float(parts[4])
                    vol = float(parts[5])
                except ValueError:
                    continue
                if day_key in days:
                    prev_ts, _, prev_vol = days[day_key]
                    days[day_key] = (ts if ts > prev_ts else prev_ts, close, prev_vol + vol)
                else:
                    days[day_key] = (ts, close, vol)
    return sorted(days.values(), key=lambda r: r[0])


def funding_stats(symbol: str, root: Path) -> tuple[float, float, float]:
    """Mean 8h funding rate (decimal), fraction >0, std. Returns (0,0,0) if file missing."""
    path = root / "data" / "funding" / f"{symbol}.csv"
    if not path.exists():
        return (0.0, 0.0, 0.0)
    rates: list[float] = []
    with path.open() as f:
        reader = csv.reader(f)
        for row in reader:
            if not row:
                continue
            try:
                # Common formats: 'fundingRate' column or just (timestamp, rate)
                rate_str = row[-1].strip().strip('"')
                rates.append(float(rate_str))
            except (ValueError, IndexError):
                continue
    if not rates:
        return (0.0, 0.0, 0.0)
    mean = sum(rates) / len(rates)
    pos = sum(1 for r in rates if r > 0) / len(rates)
    if len(rates) > 1:
        std = statistics.stdev(rates)
    else:
        std = 0.0
    return (mean, pos, std)


def compute_features(symbol: str, root: Path) -> dict[str, float]:
    daily = daily_aggregates(symbol, root)
    if len(daily) < 30:
        return {}
    closes = [d[1] for d in daily]
    vols = [d[2] for d in daily]
    log_rets = []
    for i in range(1, len(closes)):
        if closes[i - 1] > 0 and closes[i] > 0:
            log_rets.append(math.log(closes[i] / closes[i - 1]))
    if len(log_rets) < 30:
        return {}

    mean_ret = sum(log_rets) / len(log_rets)
    var_ret = sum((r - mean_ret) ** 2 for r in log_rets) / (len(log_rets) - 1)
    std_ret = math.sqrt(var_ret)
    vol_ann_pct = std_ret * math.sqrt(365) * 100

    mean_dollar_vol = sum(c * v for c, v in zip(closes, vols)) / len(closes) / 1_000_000

    skew_num = sum((r - mean_ret) ** 3 for r in log_rets) / len(log_rets)
    daily_skew = skew_num / (std_ret ** 3) if std_ret > 0 else 0.0
    kurt_num = sum((r - mean_ret) ** 4 for r in log_rets) / len(log_rets)
    daily_kurt = (kurt_num / (std_ret ** 4) - 3) if std_ret > 0 else 0.0  # excess

    fund_mean, fund_pct_pos, fund_std = funding_stats(symbol, root)

    return {
        "vol_ann_pct": vol_ann_pct,
        "mean_daily_vol_m": mean_dollar_vol,
        "mean_price": sum(closes) / len(closes),
        "listing_age_days": (daily[-1][0] - daily[0][0]).days,
        "daily_skew": daily_skew,
        "daily_kurt": daily_kurt,
        "funding_mean_8h": fund_mean,
        "funding_pct_pos": fund_pct_pos,
        "funding_std_8h": fund_std,
    }


def mann_whitney_u(a: list[float], b: list[float]) -> tuple[float, float]:
    """Two-sided Mann-Whitney U with normal approximation for the z-score.
    Returns (U_stat, two-sided p-value)."""
    if not a or not b:
        return (0.0, 1.0)
    n1, n2 = len(a), len(b)
    combined = sorted([(v, 0) for v in a] + [(v, 1) for v in b])
    # Tied-rank handling.
    ranks = [0.0] * len(combined)
    i = 0
    while i < len(combined):
        j = i
        while j + 1 < len(combined) and combined[j + 1][0] == combined[i][0]:
            j += 1
        avg_rank = (i + j) / 2 + 1
        for k in range(i, j + 1):
            ranks[k] = avg_rank
        i = j + 1
    r1 = sum(ranks[k] for k in range(len(combined)) if combined[k][1] == 0)
    u1 = r1 - n1 * (n1 + 1) / 2
    u2 = n1 * n2 - u1
    u = min(u1, u2)
    mean_u = n1 * n2 / 2
    sd_u = math.sqrt(n1 * n2 * (n1 + n2 + 1) / 12)
    z = (u - mean_u) / sd_u if sd_u > 0 else 0.0
    # Two-tailed p from normal CDF.
    p = 2 * (1 - 0.5 * (1 + math.erf(abs(z) / math.sqrt(2))))
    return (u, p)


def welch_t(a: list[float], b: list[float]) -> tuple[float, float]:
    """Welch's t-statistic and approximate p-value (normal-approx, ample n)."""
    if len(a) < 2 or len(b) < 2:
        return (0.0, 1.0)
    ma, mb = sum(a) / len(a), sum(b) / len(b)
    va = sum((x - ma) ** 2 for x in a) / (len(a) - 1)
    vb = sum((x - mb) ** 2 for x in b) / (len(b) - 1)
    se = math.sqrt(va / len(a) + vb / len(b))
    if se == 0:
        return (0.0, 1.0)
    t = (ma - mb) / se
    # Use normal approx for p (n=16, n=41 — Welch df is in the 30s, close to normal).
    p = 2 * (1 - 0.5 * (1 + math.erf(abs(t) / math.sqrt(2))))
    return (t, p)


def pearson(xs: list[float], ys: list[float]) -> float:
    n = len(xs)
    if n < 2:
        return 0.0
    mx = sum(xs) / n
    my = sum(ys) / n
    num = sum((x - mx) * (y - my) for x, y in zip(xs, ys))
    sx = math.sqrt(sum((x - mx) ** 2 for x in xs))
    sy = math.sqrt(sum((y - my) ** 2 for y in ys))
    if sx == 0 or sy == 0:
        return 0.0
    return num / (sx * sy)


def main() -> int:
    root = Path(__file__).parent.parent
    if len(sys.argv) > 1:
        net_csv = Path(sys.argv[1])
    else:
        candidates = sorted(root.glob("results/b3_symbol_net_*.csv"))
        if not candidates:
            print("no b3_symbol_net_*.csv under results/ — run scripts/b3_symbol_run.sh first")
            return 1
        net_csv = candidates[-1]

    nets: dict[str, dict] = {}
    with net_csv.open() as f:
        for row in csv.DictReader(f):
            sym = row["symbol"]
            try:
                nets[sym] = {
                    "net_usd": float(row["net_usd"]) if row["net_usd"] else 0.0,
                    "trades": int(row["trades"]) if row["trades"] else 0,
                    "wins": int(row["wins"]) if row["wins"] else 0,
                    "wr_pct": float(row["wr_pct"]) if row["wr_pct"] else 0.0,
                    "deployed": row["deployed"] == "1",
                }
            except ValueError:
                continue

    print(f"loaded {len(nets)} symbols from {net_csv}")
    print("computing per-symbol features (this scans ~5y of 1m CSVs)...")
    feats: dict[str, dict[str, float]] = {}
    for i, sym in enumerate(sorted(nets), 1):
        f = compute_features(sym, root)
        if f:
            feats[sym] = f
        if i % 10 == 0:
            print(f"  {i}/{len(nets)}")
    print(f"  features computed for {len(feats)} symbols")
    print()

    # Write joined CSV.
    feature_keys = sorted(set().union(*(set(f.keys()) for f in feats.values())))
    out_csv = root / "results" / f"b3_features_{datetime.now().strftime('%Y-%m-%d')}.csv"
    with out_csv.open("w", newline="") as f:
        w = csv.writer(f)
        w.writerow(["symbol", "net_usd", "trades", "wins", "wr_pct", "deployed"] + feature_keys)
        for sym in sorted(feats):
            row = nets.get(sym, {})
            line = [sym, row.get("net_usd", 0), row.get("trades", 0), row.get("wins", 0),
                    row.get("wr_pct", 0), 1 if row.get("deployed") else 0]
            line.extend(feats[sym].get(k, 0.0) for k in feature_keys)
            w.writerow(line)
    print(f"saved {out_csv}")
    print()

    # ── Distributional comparison: deployed-16 vs rejected-41 per feature ────
    print("=" * 90)
    print("FEATURE COMPARISON — deployed-16 vs rejected-41")
    print("=" * 90)
    print(f"{'feature':<22}  {'deployed mean':>14}  {'rejected mean':>14}  {'Δ%':>7}  {'MWU p':>8}  {'Welch p':>9}  {'flag'}")
    print(f"{'-'*22}  {'-'*14}  {'-'*14}  {'-'*7}  {'-'*8}  {'-'*9}  {'-'*4}")
    n_features = len(feature_keys)
    bonferroni_alpha = 0.05 / n_features
    sig_features = []
    rows = []
    for key in feature_keys:
        depl = [feats[s][key] for s in feats if nets[s]["deployed"]]
        rejd = [feats[s][key] for s in feats if not nets[s]["deployed"]]
        if not depl or not rejd:
            continue
        md = sum(depl) / len(depl)
        mr = sum(rejd) / len(rejd)
        delta_pct = (md - mr) / mr * 100 if mr != 0 else 0.0
        _, p_mwu = mann_whitney_u(depl, rejd)
        _, p_t = welch_t(depl, rejd)
        flag = ""
        if min(p_mwu, p_t) < bonferroni_alpha:
            flag = "  ★ Bonferroni"
            sig_features.append(key)
        elif min(p_mwu, p_t) < 0.05:
            flag = "  · single-test"
        rows.append((key, md, mr, delta_pct, p_mwu, p_t, flag))
        print(f"{key:<22}  {md:>14,.4f}  {mr:>14,.4f}  {delta_pct:>+6.1f}%  {p_mwu:>7.4f}  {p_t:>8.4f}{flag}")

    print()
    print(f"Bonferroni α = 0.05 / {n_features} = {bonferroni_alpha:.4f}")
    print()

    # ── Cross-symbol correlation: feature vs annual NET ──────────────────────
    print("=" * 90)
    print("FEATURE vs annual NET — Pearson correlation across all 57 symbols")
    print("=" * 90)
    SPAN_YEARS = 5.28
    annual_nets: dict[str, float] = {s: nets[s]["net_usd"] / SPAN_YEARS for s in feats}
    rho_rows = []
    for key in feature_keys:
        xs = [feats[s][key] for s in sorted(feats)]
        ys = [annual_nets[s] for s in sorted(feats)]
        rho = pearson(xs, ys)
        rho_rows.append((key, rho))
    rho_rows.sort(key=lambda x: -abs(x[1]))
    print(f"{'feature':<22}  {'ρ vs annual NET':>16}")
    print(f"{'-'*22}  {'-'*16}")
    for key, rho in rho_rows:
        flag = ""
        if abs(rho) > 2 / math.sqrt(len(feats)):
            flag = "  · single-test sig"
        print(f"{key:<22}  {rho:>+15.3f}{flag}")

    print()
    # ── Verdict ──────────────────────────────────────────────────────────────
    print("=" * 90)
    print("VERDICT")
    print("=" * 90)
    if not sig_features:
        print("✗ NO Bonferroni-significant feature distinguishes deployed-16 from rejected-41.")
        print("  → The deployed selection is statistically indistinguishable from the rejected")
        print("    pool by any of the structural features tested. The split is not driven by")
        print("    a measurable mechanism — consistent with 'selection adds variance not edge'.")
    else:
        print(f"✓ {len(sig_features)} feature(s) survive Bonferroni at α=0.05:")
        for k in sig_features:
            print(f"    {k}")
        print("  → These are candidate mechanism explanations for the deployed/rejected split.")
        print("    Consider whether they could form a *symbol-selection rule based on mechanism*")
        print("    rather than performance — that would survive multiple-comparisons in a way")
        print("    pure performance-based selection cannot.")

    return 0


if __name__ == "__main__":
    sys.exit(main())
