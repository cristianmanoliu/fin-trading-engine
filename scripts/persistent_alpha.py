#!/usr/bin/env python3
"""Persistent-alpha analysis: which symbols worked in BOTH the 2020-2025 backtest
AND the 2025-05..2026-04 fresh OOS?

Reads:
  results/proto_oos_combined_2026-05-05.txt — per-symbol backtest 5.33y NET (slip=5)
  results/fresh_oos_2025-05_to_2026-04_slip15_2026-05-06.txt — fresh 12mo NET (slip=15)
  results/fresh_oos_2025-05_to_2026-04_slip25_2026-05-06.txt — fresh 12mo NET (slip=25)

Computes:
  - PERSISTENT: positive in both periods → candidates for an alpha-resilient subset
  - DEAD: positive in backtest, negative in fresh → casualties of decay/regime
  - RISING: negative in backtest, positive in fresh → opportunities the train-only filter missed
  - CONFIRMED LOSER: negative in both → correctly-excluded

For the PERSISTENT subset, sub-aggregates:
  - Backtest annualised NET
  - Fresh 12mo NET (at slip=15 and slip=25)
  - Projected forward expectation if we deployed only this subset
"""
from __future__ import annotations

import re
import sys
from pathlib import Path

SYM_OOS = re.compile(
    r"^(?P<sym>[A-Z0-9]+USDT)\s+(?P<train>[+-]?\s*-?\d+)\s+(?P<test>[+-]?\s*-?\d+)\b"
)


def parse_int(s: str) -> int:
    return int(s.replace(" ", ""))


def load_backtest(path: Path) -> dict[str, int]:
    out = {}
    for line in path.read_text().splitlines():
        m = SYM_OOS.match(line.strip())
        if m:
            out[m["sym"]] = parse_int(m["train"]) + parse_int(m["test"])
    return out


def load_fresh(path: Path) -> dict[str, int]:
    pat = re.compile(r"^(?P<sym>[A-Z0-9]+USDT)\s+\d+\s+\d+\s+\S+\s+\S+\s+\S+\s+\S+\s+\S+\s+\S+h\s+net\s+(?P<net>[+-]?\d+)")
    out = {}
    for line in path.read_text().splitlines():
        m = pat.match(line.strip())
        if m:
            out[m["sym"]] = int(m["net"])
    return out


def main() -> None:
    root = Path(__file__).resolve().parent.parent
    backtest = load_backtest(root / "results/proto_oos_combined_2026-05-05.txt")
    fresh15 = load_fresh(root / "results/fresh_oos_2025-05_to_2026-04_slip15_2026-05-06.txt")
    fresh25 = load_fresh(root / "results/fresh_oos_2025-05_to_2026-04_slip25_2026-05-06.txt")

    universe = sorted(set(backtest) | set(fresh15))

    print()
    print("=" * 100)
    print("  PERSISTENT-ALPHA ANALYSIS")
    print("  Backtest: 2020-01..2025-04 (5.33y, slip=5bp)")
    print("  Fresh OOS: 2025-05..2026-04 (1y, slip=15bp + slip=25bp)")
    print("=" * 100)

    # Categorise
    persistent = []     # positive both
    dead = []           # positive backtest, negative fresh
    rising = []         # negative backtest, positive fresh
    confirmed_loser = []  # negative both

    for sym in universe:
        bt = backtest.get(sym, 0)
        fr15 = fresh15.get(sym, 0)
        if bt > 0 and fr15 > 0:
            persistent.append(sym)
        elif bt > 0 and fr15 <= 0:
            dead.append(sym)
        elif bt <= 0 and fr15 > 0:
            rising.append(sym)
        else:
            confirmed_loser.append(sym)

    # Sort persistent by combined evidence (rank by min(backtest_ann, fresh_15) * sign)
    persistent.sort(key=lambda s: -min(backtest.get(s, 0)/5.33, fresh15.get(s, 0)))

    print()
    print(f"  PERSISTENT ALPHA  (positive in both backtest AND fresh OOS)  — n={len(persistent)}")
    print(f"  These are the strongest candidates for a survivable subset deploy.")
    print()
    print(f"  {'symbol':<15} {'5y NET':>10} {'5y ann':>10} {'fresh@15':>10} {'fresh@25':>10}  in_dep_16")
    print(f"  {'-'*15} {'-'*10} {'-'*10} {'-'*10} {'-'*10}  ---------")
    deployed_16 = {"ROSEUSDT", "BCHUSDT", "GRTUSDT", "1INCHUSDT", "ADAUSDT", "KAVAUSDT",
                   "1000SHIBUSDT", "ENSUSDT", "XLMUSDT", "IMXUSDT", "ETCUSDT", "RUNEUSDT",
                   "AVAXUSDT", "APTUSDT", "DOTUSDT", "FILUSDT"}
    for sym in persistent:
        bt = backtest[sym]
        bt_ann = bt / 5.33
        fr15 = fresh15.get(sym, 0)
        fr25 = fresh25.get(sym, 0)
        in_dep = "✓ deployed" if sym in deployed_16 else ""
        print(f"  {sym:<15} ${bt:>+9,} ${bt_ann:>+9,.0f} ${fr15:>+9,} ${fr25:>+9,}  {in_dep}")

    # Sub-aggregate: what would deploying ONLY the persistent subset have produced in fresh OOS?
    pers_fresh15 = sum(fresh15.get(s, 0) for s in persistent)
    pers_fresh25 = sum(fresh25.get(s, 0) for s in persistent)
    pers_bt_ann = sum(backtest.get(s, 0)/5.33 for s in persistent)

    print()
    print(f"  PERSISTENT-{len(persistent)} SUB-AGGREGATE")
    print(f"  {'-'*82}")
    print(f"    Backtest annualised:           ${pers_bt_ann:>+10,.0f}/yr")
    print(f"    Fresh 12mo NET @ slip=15bp:    ${pers_fresh15:>+10,}")
    print(f"    Fresh 12mo NET @ slip=25bp:    ${pers_fresh25:>+10,}")
    print(f"    Capture vs backtest @ 15bp:    {100*pers_fresh15/pers_bt_ann:+.0f}%")

    # Compare to deployed-16
    dep_fresh15 = sum(fresh15.get(s, 0) for s in deployed_16)
    dep_fresh25 = sum(fresh25.get(s, 0) for s in deployed_16)
    dep_bt_ann = sum(backtest.get(s, 0)/5.33 for s in deployed_16)
    print()
    print(f"  DEPLOYED-16 SUB-AGGREGATE  (for comparison)")
    print(f"  {'-'*82}")
    print(f"    Backtest annualised:           ${dep_bt_ann:>+10,.0f}/yr")
    print(f"    Fresh 12mo NET @ slip=15bp:    ${dep_fresh15:>+10,}")
    print(f"    Fresh 12mo NET @ slip=25bp:    ${dep_fresh25:>+10,}")
    print(f"    Capture vs backtest @ 15bp:    {100*dep_fresh15/dep_bt_ann:+.0f}%")

    # Persistent ∩ deployed-16
    inter = sorted(set(persistent) & deployed_16)
    inter_fresh15 = sum(fresh15.get(s, 0) for s in inter)
    inter_fresh25 = sum(fresh25.get(s, 0) for s in inter)
    inter_bt_ann = sum(backtest.get(s, 0)/5.33 for s in inter)
    print()
    print(f"  PERSISTENT ∩ DEPLOYED-16  (the most-validated subset)  — n={len(inter)}")
    print(f"  Symbols: {inter}")
    print(f"  {'-'*82}")
    print(f"    Backtest annualised:           ${inter_bt_ann:>+10,.0f}/yr")
    print(f"    Fresh 12mo NET @ slip=15bp:    ${inter_fresh15:>+10,}")
    print(f"    Fresh 12mo NET @ slip=25bp:    ${inter_fresh25:>+10,}")

    # Show RISING (excluded by selection but worked fresh)
    rising.sort(key=lambda s: -fresh15.get(s, 0))
    print()
    print(f"  RISING — negative/zero backtest, positive fresh OOS  (n={len(rising)})")
    print(f"  These are the 'structural blind spots' the train-only filter missed.")
    print(f"  Inclusion in a future deploy would be honest only if a NEW selection")
    print(f"  methodology supports it (not retrofitting from fresh OOS).")
    print(f"  {'-'*82}")
    rising_fresh = 0
    for sym in rising[:15]:
        bt = backtest.get(sym, 0)
        fr = fresh15.get(sym, 0)
        rising_fresh += fr
        print(f"    {sym:<15} 5y={bt:>+9}  fresh={fr:>+9}")
    print(f"    Total rising fresh PnL contribution: ${rising_fresh:>+10,}")

    # Show DEAD
    dead.sort(key=lambda s: fresh15.get(s, 0))  # most negative fresh first
    print()
    print(f"  DEAD — positive backtest, negative fresh OOS (decay casualties)  (n={len(dead)})")
    print(f"  {'-'*82}")
    for sym in dead[:15]:
        bt = backtest.get(sym, 0)
        fr = fresh15.get(sym, 0)
        in_dep = " ✗ in deployed-16" if sym in deployed_16 else ""
        print(f"    {sym:<15} 5y={bt:>+9}  fresh={fr:>+9}{in_dep}")

    print()
    print(f"  CONFIRMED LOSERS — negative both periods  (n={len(confirmed_loser)})")
    print(f"  {'-'*82}")
    for sym in confirmed_loser:
        print(f"    {sym}")

    print()
    print(f"  CATEGORY SUMMARY")
    print(f"    persistent      {len(persistent):>3}/57")
    print(f"    rising          {len(rising):>3}/57")
    print(f"    dead            {len(dead):>3}/57")
    print(f"    confirmed_loser {len(confirmed_loser):>3}/57")


if __name__ == "__main__":
    main()
