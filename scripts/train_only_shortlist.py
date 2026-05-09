#!/usr/bin/env python3
"""Train-only shortlist diagnostic: quantify look-ahead bias in deployed-32 selection.

The deployed-32 list (CLAUDE.md "P4-Combined deploy candidate") was selected by
requiring positive PnL in BOTH train (2020-2022) AND test (2023-2025) halves.
That uses test data as a filter — look-ahead at the symbol-selection layer.

This script reads results/proto_oos_combined_*.txt and computes test_NET under
four selection rules:

  1. all-57            — no selection (control / headline)
  2. deployed-32       — train>0 AND test>0  (the look-ahead filter)
  3. train-positive    — train>0 only        (honest, all train winners)
  4. top-32-by-train   — top 32 by train_NET (honest, same size as deployed)

Difference between (2) and (3)/(4) is the dollar value of the look-ahead bias.

Usage:  python3 scripts/train_only_shortlist.py results/proto_oos_combined_2026-05-05.txt
"""
from __future__ import annotations

import re
import sys
from pathlib import Path


SYM_LINE = re.compile(
    r"^(?P<sym>[A-Z0-9]+USDT)\s+"
    r"(?P<train>[+-]?\s*-?\d+)\s+"
    r"(?P<test>[+-]?\s*-?\d+)\b"
)


def parse_int(s: str) -> int:
    return int(s.replace(" ", ""))


def load(path: Path) -> tuple[dict[str, tuple[int, int]], int]:
    out: dict[str, tuple[int, int]] = {}
    slip = 5
    for line in path.read_text().splitlines():
        if "stop-slippage-bps" in line:
            m = re.search(r"stop-slippage-bps=(\d+)", line)
            if m:
                slip = int(m.group(1))
        m = SYM_LINE.match(line.strip())
        if not m:
            continue
        out[m["sym"]] = (parse_int(m["train"]), parse_int(m["test"]))
    return out, slip


def report(rows: dict[str, tuple[int, int]], slip: int = 5) -> str:
    items = sorted(rows.items(), key=lambda kv: -kv[1][0])  # sort by train_NET desc

    train_pos = {s for s, (tr, _) in items if tr > 0}
    test_pos = {s for s, (_, te) in items if te > 0}
    deployed = train_pos & test_pos                              # train+ AND test+
    top32_by_train = {s for s, _ in items[:32]}

    def agg(cohort: set[str], field: int) -> int:
        return sum(rows[s][field] for s in cohort)

    all57 = set(rows.keys())

    cohorts = [
        ("all-57            (control)",           all57,            "no filter"),
        ("deployed-32       (LOOK-AHEAD filter)", deployed,         "train>0 AND test>0"),
        ("train-positive    (honest, n=var)",     train_pos,        "train>0 only"),
        ("top-32-by-train   (honest, n=32)",      top32_by_train,   "rank by train_NET, take top 32"),
    ]

    lines = []
    lines.append("")
    lines.append("=" * 82)
    lines.append("  TRAIN-ONLY SHORTLIST DIAGNOSTIC  (P4-Combined, signal_tf=4H, target_rr=6)")
    lines.append("  Train: 2020-01..2022-12   Test: 2023-01..2025-04")
    lines.append(f"  Costs: --fee-bps=10  --stop-slippage-bps={slip}  --funding=CSV")
    lines.append("=" * 82)
    lines.append("")
    lines.append(f"  {'Cohort':<42} {'n':>4} {'train_NET':>12} {'test_NET':>12}")
    lines.append(f"  {'-'*42} {'-'*4} {'-'*12} {'-'*12}")
    for label, cohort, _rule in cohorts:
        lines.append(f"  {label:<42} {len(cohort):>4} ${agg(cohort, 0):>+11,} ${agg(cohort, 1):>+11,}")
    lines.append("")

    deployed_test = agg(deployed, 1)
    train_pos_test = agg(train_pos, 1)
    top32_train_test = agg(top32_by_train, 1)

    lines.append("  Look-ahead inflation in deployed-32 test_NET")
    lines.append(f"  {'-'*82}")
    diff_vs_trainpos = deployed_test - train_pos_test
    diff_vs_top32 = deployed_test - top32_train_test
    pct_vs_trainpos = 100.0 * diff_vs_trainpos / max(abs(train_pos_test), 1)
    pct_vs_top32 = 100.0 * diff_vs_top32 / max(abs(top32_train_test), 1)
    lines.append(f"    vs train-positive  (n={len(train_pos)}): "
                 f"${diff_vs_trainpos:>+10,}  ({pct_vs_trainpos:+.1f}% inflation)")
    lines.append(f"    vs top-32-by-train (n=32): "
                 f"${diff_vs_top32:>+10,}  ({pct_vs_top32:+.1f}% inflation)")
    lines.append("")

    swap_out = top32_by_train - deployed
    swap_in = deployed - top32_by_train
    lines.append("  Swap accounting: deployed-32 vs top-32-by-train")
    lines.append(f"  {'-'*82}")
    lines.append(f"    Removed from top-32-by-train (looked-ahead-out): {sorted(swap_out)}")
    lines.append(f"      Their train_NET sum: ${sum(rows[s][0] for s in swap_out):>+10,}")
    lines.append(f"      Their test_NET  sum: ${sum(rows[s][1] for s in swap_out):>+10,}")
    lines.append(f"    Added by deployed (looked-ahead-in):             {sorted(swap_in)}")
    lines.append(f"      Their train_NET sum: ${sum(rows[s][0] for s in swap_in):>+10,}")
    lines.append(f"      Their test_NET  sum: ${sum(rows[s][1] for s in swap_in):>+10,}")
    lines.append("")

    flippers = sorted(s for s in train_pos if rows[s][1] <= 0)
    recoveries = sorted(s for s in test_pos if rows[s][0] <= 0)
    persistent_loss = sorted(s for s in all57 if rows[s][0] <= 0 and rows[s][1] <= 0)
    lines.append("  Cohort composition")
    lines.append(f"  {'-'*82}")
    lines.append(f"    Persistent winners (deployed-32):  {len(deployed)} symbols")
    lines.append(f"    Train-WIN, Test-LOSS (regime flip): {flippers}")
    lines.append(f"      Test contribution: ${sum(rows[s][1] for s in flippers):>+10,}")
    lines.append("    Train-LOSS, Test-WIN (recoveries — INVISIBLE to honest filter):")
    lines.append(f"      Symbols: {recoveries}")
    lines.append(f"      Test contribution: ${sum(rows[s][1] for s in recoveries):>+10,}")
    lines.append(f"    Persistent losers (drop): {persistent_loss}")
    lines.append(f"      Test contribution: ${sum(rows[s][1] for s in persistent_loss):>+10,}")
    lines.append("")

    period_years = (2025 - 2023) + 4 / 12
    lines.append(f"  Annualised test PnL (period = {period_years:.2f} years, slip={slip}bp)")
    lines.append(f"  {'-'*82}")
    for label, cohort, _ in cohorts:
        ann = agg(cohort, 1) / period_years
        lines.append(f"    {label:<42} ${ann:>+11,.0f}/yr")
    lines.append("")

    lines.append("  Verdict")
    lines.append(f"  {'-'*82}")
    if train_pos_test > 0 and top32_train_test > 0:
        lines.append("    PASS: strategy is positive on test under BOTH honest selection rules.")
        lines.append("    Look-ahead bias inflated deployed-32 test by "
                     f"{pct_vs_top32:+.0f}% vs same-size honest top-32-by-train,")
        lines.append(f"    but did NOT flip the sign. Honest annualised test ≈ "
                     f"${top32_train_test / period_years:,.0f}/yr at slip={slip}bp.")
        lines.append("    The strategy survives the diagnostic with a haircut.")
    else:
        lines.append("    FAIL: strategy goes non-positive under honest train-only selection.")
        lines.append("    The 'edge' was substantially symbol-selection look-ahead. Reconsider.")
    lines.append("")
    return "\n".join(lines)


def main() -> None:
    if len(sys.argv) < 2:
        print("usage: train_only_shortlist.py <proto_oos_combined.txt>", file=sys.stderr)
        sys.exit(1)
    rows, slip = load(Path(sys.argv[1]))
    if not rows:
        print("no symbol rows parsed — check input format", file=sys.stderr)
        sys.exit(1)
    print(report(rows, slip))


if __name__ == "__main__":
    main()
