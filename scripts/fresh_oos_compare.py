#!/usr/bin/env python3
"""Compare per-symbol PnL: original 5y backtest vs fresh 12-month OOS.

Inputs:
  results/proto_oos_combined_2026-05-05.txt — per-symbol train (3y) + test (2.33y)
                                                NET at slip=5bp
  results/fresh_oos_2025-05_to_2026-04_slip15_*.txt — per-symbol 12mo NET at slip=15

For each symbol, computes:
  - backtest_annualized: (train + test NET) / 5.33 years
  - fresh_12mo: NET from fresh window (already 1 year)
  - decay_pct: (fresh - backtest_annualized) / backtest_annualized

Then categorizes:
  - MAINTAINER: |decay_pct| < 30%
  - DECAYER: decay_pct < -50%
  - IMPROVER: decay_pct > +50%
  - REVERSAL: sign change
"""
from __future__ import annotations

import re
import sys
from pathlib import Path

# Backtest data: train + test halves (slip=5)
SYM_OOS = re.compile(
    r"^(?P<sym>[A-Z0-9]+USDT)\s+(?P<train>[+-]?\s*-?\d+)\s+(?P<test>[+-]?\s*-?\d+)\b"
)


def parse_int(s: str) -> int:
    return int(s.replace(" ", ""))


def load_backtest_5y(path: Path) -> dict[str, int]:
    """Returns {symbol: 5.33y_NET_at_slip5}."""
    out = {}
    for line in path.read_text().splitlines():
        m = SYM_OOS.match(line.strip())
        if m:
            out[m["sym"]] = parse_int(m["train"]) + parse_int(m["test"])
    return out


def load_fresh_oos(path: Path) -> dict[str, int]:
    """Parse the formatted fresh-OOS output to {symbol: net}."""
    # Lines look like: "BTCUSDT              27     4  14.8%     -9069      2994        +0    -16237  99.9h   net -16237"
    out = {}
    pat = re.compile(r"^(?P<sym>[A-Z0-9]+USDT)\s+\d+\s+\d+\s+\S+\s+\S+\s+\S+\s+\S+\s+\S+\s+\S+h\s+net\s+(?P<net>[+-]?\d+)")
    for line in path.read_text().splitlines():
        m = pat.match(line.strip())
        if m:
            out[m["sym"]] = int(m["net"])
    return out


def main() -> None:
    root = Path(__file__).resolve().parent.parent
    backtest = load_backtest_5y(root / "results/proto_oos_combined_2026-05-05.txt")

    # Default fresh-OOS file (shorts at slip=15)
    fresh_path = root / "results/fresh_oos_2025-05_to_2026-04_slip15_2026-05-06.txt"
    if len(sys.argv) > 1:
        fresh_path = Path(sys.argv[1])
    fresh = load_fresh_oos(fresh_path)

    if not fresh:
        print(f"FAIL: no per-symbol rows parsed from {fresh_path}", file=sys.stderr)
        sys.exit(1)

    print()
    print("=" * 100)
    print(f"  FRESH OOS COMPARISON  ({fresh_path.name})")
    print("  Backtest period: 2020-01..2025-04 (5.33y, slip=5bp)")
    print("  Fresh period:    2025-05..2026-04 (1.00y, slip=15bp)")
    print("=" * 100)
    print()
    print(f"  {'symbol':<14} {'5.33y':>10} {'ann (5y)':>10} {'fresh 1y':>10} {'gap $':>10} {'gap %':>10}  category")
    print(f"  {'-'*14} {'-'*10} {'-'*10} {'-'*10} {'-'*10} {'-'*10}  --------")

    rows = []
    for sym in sorted(set(backtest) | set(fresh)):
        bt = backtest.get(sym, 0)
        bt_ann = bt / 5.33
        fr = fresh.get(sym, 0)
        gap = fr - bt_ann
        if abs(bt_ann) > 100:
            gap_pct = 100.0 * gap / abs(bt_ann)
        else:
            gap_pct = float("nan")

        # Categorize
        if (bt_ann > 0) != (fr > 0) and abs(bt_ann) > 1000 and abs(fr) > 1000:
            cat = "REVERSAL"
        elif gap_pct != gap_pct:  # NaN
            cat = "small-bt"
        elif gap_pct > 50:
            cat = "IMPROVER"
        elif gap_pct < -50:
            cat = "DECAYER"
        else:
            cat = "maintainer"

        rows.append((sym, bt, bt_ann, fr, gap, gap_pct, cat))

    rows.sort(key=lambda r: -r[3])  # sort by fresh NET desc

    for sym, bt, bt_ann, fr, gap, gap_pct, cat in rows:
        gap_pct_str = f"{gap_pct:+.0f}%" if gap_pct == gap_pct else "—"
        print(f"  {sym:<14} ${bt:>+9,} ${bt_ann:>+9,.0f} ${fr:>+9,} ${gap:>+9,.0f} {gap_pct_str:>10}  {cat}")

    print()
    print("  CATEGORY COUNTS")
    cats: dict[str, list[str]] = {}
    for sym, _, _, _, _, _, c in rows:
        cats.setdefault(c, []).append(sym)
    for c in ("REVERSAL", "DECAYER", "maintainer", "IMPROVER", "small-bt"):
        if c in cats:
            print(f"    {c:<11}: {len(cats[c])} symbols  {cats[c][:5]}...")

    print()
    print("  HEADLINES")
    total_bt_ann = sum(r[2] for r in rows)
    total_fr = sum(r[3] for r in rows)
    print(f"    Backtest annualised total:  ${total_bt_ann:>+10,.0f}/yr")
    print(f"    Fresh 12mo total:           ${total_fr:>+10,.0f}/yr")
    print(f"    Aggregate gap:              ${total_fr - total_bt_ann:>+10,.0f}  ({100*(total_fr - total_bt_ann)/abs(total_bt_ann):+.0f}%)")

    # Deployed-16 sub-aggregate
    deployed = {"ROSEUSDT", "BCHUSDT", "GRTUSDT", "1INCHUSDT", "ADAUSDT", "KAVAUSDT",
                "1000SHIBUSDT", "ENSUSDT", "XLMUSDT", "IMXUSDT", "ETCUSDT", "RUNEUSDT",
                "AVAXUSDT", "APTUSDT", "DOTUSDT", "FILUSDT"}
    dep_bt_ann = sum(r[2] for r in rows if r[0] in deployed)
    dep_fr = sum(r[3] for r in rows if r[0] in deployed)
    print()
    print(f"    Deployed-16 backtest annualised: ${dep_bt_ann:>+10,.0f}/yr")
    print(f"    Deployed-16 fresh 12mo:          ${dep_fr:>+10,.0f}/yr")
    print(f"    Deployed-16 capture: {100*dep_fr/dep_bt_ann:+.0f}% of expectation")


if __name__ == "__main__":
    main()
