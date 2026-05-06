#!/usr/bin/env python3
"""walk_forward_compare.py — aggregate results from multiple walk_forward.sh runs
into a single comparison matrix.

Reads results/walk_forward_*.txt files (or those passed on the command line),
extracts per-window NET, and produces a config × window table with verdict.

Usage:
    ./scripts/walk_forward_compare.py                          # all walk_forward_*.txt
    ./scripts/walk_forward_compare.py results/wf_1.txt ...     # specific files
"""
from __future__ import annotations

import re
import sys
from pathlib import Path

WINDOW_LINE = re.compile(
    r"^(W\d+_\d{4}-\d{2}_to_\d{4}-\d{2})\s+([+-]?\d+)\s+(\d+)\s+([\d.]+)%"
)
LABEL_LINE = re.compile(r"WALK-FORWARD VALIDATION\s+(\S+)")


def parse_one(path: Path) -> dict | None:
    text = path.read_text()
    label_match = LABEL_LINE.search(text)
    if not label_match:
        return None
    label = label_match.group(1)

    rows = []
    for line in text.splitlines():
        m = WINDOW_LINE.match(line.strip())
        if m:
            rows.append({
                "window": m.group(1),
                "net": int(m.group(2)),
                "trades": int(m.group(3)),
                "wr": float(m.group(4)),
            })
    if not rows:
        return None
    return {"label": label, "windows": rows}


def main() -> None:
    if len(sys.argv) > 1:
        paths = [Path(p) for p in sys.argv[1:]]
    else:
        root = Path(__file__).resolve().parent.parent
        paths = sorted((root / "results").glob("walk_forward_*.txt"))

    configs = []
    for p in paths:
        parsed = parse_one(p)
        if parsed:
            parsed["source"] = p.name
            configs.append(parsed)

    if not configs:
        print("no walk_forward results found", file=sys.stderr)
        sys.exit(1)

    # Collect all window labels (assumed identical across configs)
    all_windows = sorted({w["window"] for c in configs for w in c["windows"]})

    print()
    print("=" * 110)
    print("  WALK-FORWARD COMPARISON  (universe-57, shorts only, target_rr=6.0, fee=10bp, slip=15bp)")
    print(f"  {len(configs)} configurations × {len(all_windows)} windows")
    print("=" * 110)
    print()

    # Header
    hdr_cols = [f"{w[3:18]:>16}" for w in all_windows]  # show "2023-05_to_2024" portion
    print(f"  {'config':<18} {'sum_3yr':>11} {'mean/yr':>10} {'pos/N':>6}  {'  '.join(hdr_cols)}")
    print(f"  {'-'*18} {'-'*11} {'-'*10} {'-'*6}  {'  '.join('-'*16 for _ in all_windows)}")

    config_summary = []
    for c in configs:
        win_map = {w["window"]: w for w in c["windows"]}
        total = sum(w["net"] for w in c["windows"])
        n = len(c["windows"])
        mean = total // n if n else 0
        n_pos = sum(1 for w in c["windows"] if w["net"] > 0)
        cells = []
        for w in all_windows:
            entry = win_map.get(w)
            if entry:
                cells.append(f"{entry['net']:>+16,}")
            else:
                cells.append(f"{'—':>16}")
        print(f"  {c['label']:<18} {total:>+11,} {mean:>+10,} {n_pos}/{n:<3}  {'  '.join(cells)}")
        config_summary.append({"label": c["label"], "sum": total, "mean": mean, "n_pos": n_pos, "n": n})

    print()
    print("  Verdict by configuration")
    print("  " + "-" * 90)
    for s in config_summary:
        n_pos, n, mean = s["n_pos"], s["n"], s["mean"]
        if n_pos == n and mean > 0:
            verdict = "STRONG (positive in ALL windows)"
        elif n_pos > n / 2 and mean > 0:
            verdict = "SUPPORTIVE (positive majority + positive mean)"
        elif n_pos == 0:
            verdict = "REJECTED (no window positive)"
        elif mean < 0:
            verdict = "REJECTED (negative mean)"
        else:
            verdict = "REJECTED (minority positive)"
        print(f"    {s['label']:<22} {n_pos}/{n} positive, mean ${mean:+,}/yr  →  {verdict}")

    print()
    print("  Interpretation guide")
    print("  " + "-" * 90)
    print("  - Walk-forward = test on N non-overlapping OOS windows the strategy")
    print("    was never tuned against. n=3 with one losing window is consistent")
    print("    with normal regime variance, NOT a free pass to deploy.")
    print("  - SUPPORTIVE means the strategy survived a stricter test than single-window")
    print("    fresh OOS, but is NOT proof of forward edge. The variance is enormous.")
    print("  - REJECTED means the strategy did not survive — across multiple windows it")
    print("    failed to produce positive expected value. This is high-confidence evidence")
    print("    that the configuration has no exploitable edge.")
    print("  - This framework is designed to REJECT bad strategies efficiently, not to")
    print("    validate good ones. A SUPPORTIVE result is necessary but not sufficient.")


if __name__ == "__main__":
    main()
