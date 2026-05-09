#!/usr/bin/env python3
"""Analyze WHY some symbols are profitable and others aren't.

Joins:
  - results/symbol_characteristics.csv  (structural price metrics)
  - results/option_c_57sym_continuous_2026-05-05.txt  (continuous PnL per symbol)
  - results/oos_persistence_2026-05-05.tsv  (train/test PnL per symbol)

Computes:
  1. Pearson + Spearman correlations between each price metric and PnL
  2. Statistical significance test for per-symbol WR variation (binomial vs observed)
  3. Identifies which metrics best discriminate winners from losers
  4. Anti-pattern analysis: what makes CRV/ETC/APT bad vs RUNE/SOL/BNB good
"""
from __future__ import annotations

import csv
import math
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
CHARS_FP = ROOT / "results" / "symbol_characteristics.csv"
CONT_FP  = ROOT / "results" / "option_c_57sym_continuous_2026-05-05.txt"
OOS_FP   = ROOT / "results" / "oos_persistence_2026-05-05.tsv"


def load_continuous_pnl() -> dict[str, dict]:
    """Parse the continuous-sweep report. One row per symbol with trades/wins/pnl_usd."""
    out: dict[str, dict] = {}
    text = CONT_FP.read_text()
    for line in text.splitlines():
        m = re.match(r"^([A-Z0-9]+USDT)\s+(\d+)\s+(\d+)\s+([\d.]+%|–)\s+([+\-]?\s*[\d,]+)k", line)
        if m:
            sym = m.group(1)
            trades = int(m.group(2))
            wins = int(m.group(3))
            pnl_k = int(m.group(5).replace(",", "").replace(" ", ""))
            out[sym] = {"trades": trades, "wins": wins, "pnl_k": pnl_k}
    return out


def load_oos() -> tuple[dict[str, int], dict[str, int]]:
    train: dict[str, int] = {}
    test:  dict[str, int] = {}
    with OOS_FP.open() as f:
        next(f)  # header
        for line in f:
            parts = line.strip().split("\t")
            if len(parts) < 5:
                continue
            sym, period, _, _, pnl = parts[:5]
            try:
                p = int(float(pnl))
            except ValueError:
                continue
            (train if period == "train" else test)[sym] = p
    return train, test


def load_chars() -> list[dict]:
    rows = []
    with CHARS_FP.open() as f:
        for r in csv.DictReader(f):
            row = {"symbol": r["symbol"]}
            for k in ("days", "years", "total_return", "vol_annualized", "max_drawdown",
                     "autocorr_1d", "autocorr_5d", "autocorr_20d", "hurst",
                     "avg_daily_range_pct", "direction_persistence_1d"):
                v = r.get(k, "")
                if v == "":
                    row[k] = None
                else:
                    try:
                        row[k] = float(v)
                    except ValueError:
                        row[k] = None
            rows.append(row)
    return rows


def pearson(xs: list[float], ys: list[float]) -> float:
    n = len(xs)
    if n < 3:
        return float("nan")
    mx = sum(xs) / n; my = sum(ys) / n
    sx = sum((x - mx) ** 2 for x in xs)
    sy = sum((y - my) ** 2 for y in ys)
    sxy = sum((xs[i] - mx) * (ys[i] - my) for i in range(n))
    if sx <= 0 or sy <= 0:
        return float("nan")
    return sxy / math.sqrt(sx * sy)


def spearman(xs: list[float], ys: list[float]) -> float:
    n = len(xs)
    if n != len(ys) or n < 3:
        return float("nan")
    rx = {v: i for i, v in enumerate(sorted(xs))}
    ry = {v: i for i, v in enumerate(sorted(ys))}
    d2 = sum((rx[xs[i]] - ry[ys[i]]) ** 2 for i in range(n))
    return 1.0 - (6.0 * d2) / (n * (n * n - 1))


def main() -> None:
    cont = load_continuous_pnl()
    train, test = load_oos()
    chars = load_chars()

    # Build joined dataset
    joined = []
    for c in chars:
        sym = c["symbol"]
        if sym not in cont:
            continue
        if any(c[k] is None for k in ("vol_annualized", "hurst", "autocorr_1d", "max_drawdown", "total_return")):
            continue
        wr = cont[sym]["wins"] / cont[sym]["trades"] if cont[sym]["trades"] else 0
        joined.append({
            "symbol": sym,
            "trades": cont[sym]["trades"],
            "wins": cont[sym]["wins"],
            "wr": wr,
            "pnl_k": cont[sym]["pnl_k"],
            "train_pnl": train.get(sym, 0),
            "test_pnl": test.get(sym, 0),
            **{k: c[k] for k in ("total_return", "vol_annualized", "max_drawdown",
                                 "autocorr_1d", "autocorr_5d", "autocorr_20d",
                                 "hurst", "avg_daily_range_pct", "direction_persistence_1d")},
        })

    print()
    print("=" * 80)
    print("  WHY ARE SOME SYMBOLS PROFITABLE?  Trend-follower diagnostic, 57 symbols")
    print("=" * 80)
    print()
    print(f"  Joined dataset: {len(joined)} symbols")
    print()

    # --- 1. Statistical significance: is per-symbol WR variation real or noise? ---
    print("─" * 80)
    print("  1. IS THE PER-SYMBOL WIN-RATE VARIATION REAL OR JUST NOISE?")
    print("─" * 80)
    total_trades = sum(j["trades"] for j in joined)
    total_wins = sum(j["wins"] for j in joined)
    pooled_wr = total_wins / total_trades
    print(f"  Pooled WR across all symbols/trades: {pooled_wr*100:.2f}%  (breakeven at 5R: 16.67%)")

    chi2 = 0.0
    for j in joined:
        expected = j["trades"] * pooled_wr
        observed = j["wins"]
        chi2 += (observed - expected) ** 2 / expected
    df = len(joined) - 1
    print(f"  χ² goodness-of-fit (H0: all symbols same WR): {chi2:.1f} on {df} df")
    print(f"  Critical χ² at p=0.001 ≈ {df + 3*math.sqrt(2*df):.0f}")
    if chi2 > df + 3 * math.sqrt(2 * df):
        print("  → REJECT H0: WR variation is statistically significant.")
        print("    There is real per-symbol structural alpha. Not pure noise.")
    else:
        print("  → CANNOT REJECT H0: WR variation is consistent with binomial noise.")
        print("    Per-symbol differences may be luck.")
    print()
    wr_std_observed = math.sqrt(sum((j["wr"] - pooled_wr) ** 2 for j in joined) / len(joined))
    avg_n = total_trades / len(joined)
    wr_std_expected = math.sqrt(pooled_wr * (1 - pooled_wr) / avg_n)
    print(f"  Observed WR std-dev across symbols:  {wr_std_observed*100:.3f}pp")
    print(f"  Expected WR std-dev under pure noise: {wr_std_expected*100:.3f}pp")
    print(f"  Ratio (observed / chance):            {wr_std_observed / wr_std_expected:.2f}×")
    print(f"  Interpretation: {wr_std_observed / wr_std_expected:.1f}× more spread than chance")
    print("  alone would produce → real structural alpha component, but most")
    print("  of the per-symbol spread is still consistent with chance.")
    print()

    # --- 2. Correlations: which structural metrics predict PnL/WR? ---
    print("─" * 80)
    print("  2. WHICH ASSET CHARACTERISTICS PREDICT STRATEGY PROFITABILITY?")
    print("─" * 80)
    print()
    metrics = [
        ("total_return",          "5y total return %"),
        ("vol_annualized",        "annualized vol"),
        ("max_drawdown",          "max drawdown (more negative = worse)"),
        ("autocorr_1d",           "1-day return autocorrelation"),
        ("autocorr_5d",           "5-day return autocorrelation"),
        ("autocorr_20d",          "20-day return autocorrelation"),
        ("hurst",                 "Hurst exponent (>0.5 trending)"),
        ("avg_daily_range_pct",   "avg daily range / close"),
        ("direction_persistence_1d", "fraction of consecutive-same-sign days"),
    ]
    print(f"  {'Metric':<32}  {'ρ(metric, PnL)':>12}  {'ρ(metric, WR)':>12}  {'ρ_test':>10}")
    print(f"  {'-'*32}  {'-'*12}  {'-'*12}  {'-'*10}")
    for key, desc in metrics:
        xs = [j[key] for j in joined]
        pnls = [j["pnl_k"] for j in joined]
        wrs  = [j["wr"]    for j in joined]
        tests = [j["test_pnl"] for j in joined]
        rho_pnl  = spearman(xs, pnls)
        rho_wr   = spearman(xs, wrs)
        rho_test = spearman(xs, tests)
        flag = ""
        if abs(rho_pnl) > 0.4 and not math.isnan(rho_pnl):
            flag = " ⭐"
        print(f"  {desc:<32}  {rho_pnl:>+12.3f}  {rho_wr:>+12.3f}  {rho_test:>+10.3f}{flag}")
    print()
    print("  ⭐ = strong correlation (|ρ| > 0.4). The strategy's profitability is")
    print("  most strongly driven by these metrics.")
    print()

    # --- 3. Top vs bottom 10 split ---
    print("─" * 80)
    print("  3. WHAT'S DIFFERENT ABOUT WINNERS vs LOSERS?")
    print("─" * 80)
    sorted_pnl = sorted(joined, key=lambda j: -j["pnl_k"])
    top10 = sorted_pnl[:10]
    bot10 = sorted_pnl[-10:]
    print()
    print(f"  {'Metric':<32}  {'Top-10 mean':>12}  {'Bottom-10 mean':>16}  {'Δ':>10}")
    print(f"  {'-'*32}  {'-'*12}  {'-'*16}  {'-'*10}")
    for key, desc in metrics:
        t = sum(j[key] for j in top10) / len(top10)
        b = sum(j[key] for j in bot10) / len(bot10)
        delta = t - b
        print(f"  {desc:<32}  {t:>+12.4f}  {b:>+16.4f}  {delta:>+10.4f}")
    print()

    # --- 4. The roster: top 10 and bottom 10 with their characteristics ---
    print("─" * 80)
    print("  4. TOP 10 BY CONTINUOUS PnL — what they have in common")
    print("─" * 80)
    print()
    print(f"  {'Sym':<10} {'PnL$1k':>8} {'WR':>6} {'5y ret':>8} {'vol':>6} {'maxDD':>7} {'hurst':>6}")
    for j in top10:
        print(f"  {j['symbol']:<10} {j['pnl_k']:>+7,}k {j['wr']*100:>5.1f}% {j['total_return']:>+7.1%} {j['vol_annualized']:>5.2f} {j['max_drawdown']:>+6.1%} {j['hurst']:>6.3f}")
    print()
    print("─" * 80)
    print("  5. BOTTOM 10 BY CONTINUOUS PnL — anti-patterns")
    print("─" * 80)
    print()
    print(f"  {'Sym':<10} {'PnL$1k':>8} {'WR':>6} {'5y ret':>8} {'vol':>6} {'maxDD':>7} {'hurst':>6}")
    for j in bot10:
        print(f"  {j['symbol']:<10} {j['pnl_k']:>+7,}k {j['wr']*100:>5.1f}% {j['total_return']:>+7.1%} {j['vol_annualized']:>5.2f} {j['max_drawdown']:>+6.1%} {j['hurst']:>6.3f}")
    print()

    # --- 6. Directional bias: does the strategy depend on uptrending markets? ---
    print("─" * 80)
    print("  6. DOES THE STRATEGY ONLY WORK ON UP-TRENDING ASSETS?")
    print("─" * 80)
    print()
    up_trended  = [j for j in joined if j["total_return"] >  0]
    down_trended = [j for j in joined if j["total_return"] <= 0]
    avg_up_pnl  = sum(j["pnl_k"] for j in up_trended)   / len(up_trended)   if up_trended   else 0
    avg_dn_pnl  = sum(j["pnl_k"] for j in down_trended) / len(down_trended) if down_trended else 0
    print(f"  Symbols with positive 5y return ({len(up_trended)}):  avg PnL = {avg_up_pnl:+.0f}k")
    print(f"  Symbols with negative 5y return ({len(down_trended)}): avg PnL = {avg_dn_pnl:+.0f}k")
    print(f"  Difference: {avg_up_pnl - avg_dn_pnl:+.0f}k")
    print()
    print("  If this gap is large, the strategy is fundamentally a long-bias bet")
    print("  on bull markets, not a true two-sided trend follower.")
    print()


if __name__ == "__main__":
    main()
