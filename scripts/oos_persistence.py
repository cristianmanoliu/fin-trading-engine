#!/usr/bin/env python3
"""Analyze out-of-sample persistence of symbol selection.

Reads the TSV produced by oos_persistence.sh (columns:
symbol, period, trades, wins, pnl_usd) and computes:

  - Train-period top-16 overlap with test-period top-16
  - Spearman rank correlation between train and test PnL
  - Per-symbol regime change (winner→loser, loser→winner)
  - Aggregate train and test PnL for the train-period top-16

Usage:  python3 scripts/oos_persistence.py results/oos_persistence_<date>.tsv
"""
from __future__ import annotations

import sys
from pathlib import Path


def spearman(xs: list[float], ys: list[float]) -> float:
    """Spearman rank correlation, no scipy dependency."""
    n = len(xs)
    if n != len(ys) or n < 2:
        return 0.0
    rx = {v: i for i, v in enumerate(sorted(xs))}
    ry = {v: i for i, v in enumerate(sorted(ys))}
    d2 = sum((rx[xs[i]] - ry[ys[i]]) ** 2 for i in range(n))
    return 1.0 - (6.0 * d2) / (n * (n * n - 1))


def main() -> None:
    if len(sys.argv) < 2:
        print("usage: oos_persistence.py <tsv>", file=sys.stderr)
        sys.exit(1)

    rows = Path(sys.argv[1]).read_text().strip().splitlines()
    train: dict[str, tuple[int, int, int]] = {}
    test:  dict[str, tuple[int, int, int]] = {}
    for line in rows[1:]:
        parts = line.split("\t")
        if len(parts) < 5:
            continue
        sym, period, trades, wins, pnl = parts[:5]
        try:
            t, w, p = int(trades), int(wins), int(float(pnl))
        except ValueError:
            continue
        (train if period == "train" else test)[sym] = (t, w, p)

    common = sorted(set(train) & set(test))
    if not common:
        print("no common symbols", file=sys.stderr)
        sys.exit(1)

    train_sorted = sorted(common, key=lambda s: -train[s][2])
    test_sorted  = sorted(common, key=lambda s: -test[s][2])

    train_top16 = set(train_sorted[:16])
    test_top16  = set(test_sorted[:16])
    overlap = train_top16 & test_top16

    train_pnls = [train[s][2] for s in common]
    test_pnls  = [test[s][2]  for s in common]
    rho = spearman(train_pnls, test_pnls)

    train_top16_total_train = sum(train[s][2] for s in train_top16)
    train_top16_total_test  = sum(test[s][2]  for s in train_top16)
    full_train = sum(train_pnls)
    full_test  = sum(test_pnls)

    # WR check: how many train-top-16 became unprofitable in test?
    flipped = [s for s in train_top16 if test[s][2] <= 0]
    persisted = sorted(train_top16 & test_top16, key=lambda s: -test[s][2])

    print()
    print("════════════════════════════════════════════════════════════════════")
    print("  OUT-OF-SAMPLE PERSISTENCE TEST  target_rr=5.0")
    print("  train: 2020-01..2022-12 (36mo)   test: 2023-01..2025-04 (28mo)")
    print("════════════════════════════════════════════════════════════════════")
    print()
    print(f"  Symbols with data in both periods:  {len(common)}")
    print(f"  Train top-16 ∩ Test top-16:          {len(overlap)}/16")
    print(f"  Random-baseline expected overlap:    {16*16/len(common):.1f}/16")
    print(f"  Spearman ρ (train vs test PnL):      {rho:+.3f}")
    print()
    print(f"  Aggregate train PnL (all):          ${full_train:>+12,.0f}")
    print(f"  Aggregate test  PnL (all):          ${full_test:>+12,.0f}")
    print()
    print(f"  Train top-16 train-period total:    ${train_top16_total_train:>+12,.0f}")
    print(f"  Train top-16 test-period total:     ${train_top16_total_test:>+12,.0f}")
    print(f"  Train-top-16 → test-period flips to losing: {len(flipped)}/16")
    print()

    if len(overlap) >= 12:
        verdict = "STRUCTURAL ALPHA — top-16 selection is persistent. Rotate confidently."
    elif len(overlap) >= 8:
        verdict = "PARTIAL PERSISTENCE — keep proven names, drop only confirmed losers."
    elif len(overlap) >= 4:
        verdict = "MOSTLY NOISE — top-16 selection may be overfit. Be cautious."
    else:
        verdict = "LUCK — symbol selection does not survive OOS. Reconsider strategy."
    print(f"  VERDICT: {verdict}")
    print()

    print("  Train top-16 — fate in test period:")
    print(f"  {'Symbol':<14} {'Train $1k':>12} {'Test $1k':>12} {'Δ':>10}  Status")
    print(f"  {'------':<14} {'-':>12} {'-':>12} {'-':>10}  ------")
    for s in train_sorted[:16]:
        train_p = train[s][2] // 1000
        test_p  = test[s][2]  // 1000
        delta   = test_p - train_p
        in_test_top16 = "★ in test top16" if s in test_top16 else ("✗ became loser" if test[s][2] <= 0 else "  middle")
        print(f"  {s:<14} {train_p:>+10,}k {test_p:>+10,}k {delta:>+8,}k  {in_test_top16}")
    print()

    print("  Test top-16 — origin in train period:")
    print(f"  {'Symbol':<14} {'Train $1k':>12} {'Test $1k':>12}   Status")
    print(f"  {'------':<14} {'-':>12} {'-':>12}   ------")
    for s in test_sorted[:16]:
        train_p = train[s][2] // 1000
        test_p  = test[s][2]  // 1000
        was_top16 = "★ also in train top16" if s in train_top16 else ("☉ rose from middle" if train[s][2] > 0 else "✗ rose from losing")
        print(f"  {s:<14} {train_p:>+10,}k {test_p:>+10,}k   {was_top16}")
    print()

    print("  Currently deployed (12) — train vs test:")
    deployed = ["RUNEUSDT","SOLUSDT","XLMUSDT","LINKUSDT","APEUSDT","BTCUSDT",
                "ENJUSDT","TIAUSDT","ETHUSDT","ZILUSDT","LDOUSDT","OPUSDT"]
    print(f"  {'Symbol':<14} {'Train $1k':>12} {'Test $1k':>12}   Persistence")
    for s in deployed:
        if s not in train or s not in test:
            print(f"  {s:<14} {'(no data)':>12} {'':>12}")
            continue
        train_p = train[s][2] // 1000
        test_p  = test[s][2]  // 1000
        sign_train = "+" if train_p > 0 else ("0" if train_p == 0 else "-")
        sign_test  = "+" if test_p > 0 else ("0" if test_p == 0 else "-")
        flag = "✓ both +" if train_p > 0 and test_p > 0 else ("✗ flipped" if (train_p > 0) != (test_p > 0) else "× both -")
        print(f"  {s:<14} {train_p:>+10,}k {test_p:>+10,}k   {flag}")


if __name__ == "__main__":
    main()
