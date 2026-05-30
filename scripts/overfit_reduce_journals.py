#!/usr/bin/env python3
"""
overfit_reduce_journals.py — Reduce per-config trade journals into a
months × configs NET returns matrix.

Usage:
    python3 scripts/overfit_reduce_journals.py <journals_root> <manifest_csv> <out_csv>

Arguments:
    journals_root  Directory containing one sub-dir per config label, each
                   holding SYMBOL-YYYY-MM.jsonl journal files.
    manifest_csv   Config manifest (label,extra_flags header + 34 data rows).
                   Column order in output follows manifest order.
    out_csv        Output CSV path.

Entry-month pairing contract (CRITICAL):
    The close event's `ts` field is wall-clock (time.Now() in the Stub) and
    is therefore useless for dating historical replay trades — every close
    would map to the current month.  The open event's `ts` IS the true
    exchange timestamp.  Each trade is bucketed by open-event ts[:7].

    Walk each file in chronological order:
    - on "open"  → record current_entry_month = ts[:7]
    - on "close" → add pnl_usd to matrix[label][current_entry_month]
    A PARTIAL + final close both credit the same entry month (do NOT reset
    current_entry_month on close).
"""
import csv
import glob
import json
import os
import sys


def load_labels(manifest_csv: str) -> list[str]:
    """Return config labels in manifest order (skip header)."""
    labels = []
    with open(manifest_csv, newline="") as f:
        reader = csv.reader(f)
        header = True
        for row in reader:
            if header:
                header = False
                continue
            if row:
                labels.append(row[0].strip())
    return labels


def reduce_label(journals_root: str, label: str) -> dict[str, float]:
    """
    Process all journal files for one config label.
    Returns {month_str: net_pnl} where month_str is YYYY-MM.
    """
    pattern = os.path.join(journals_root, label, "*.jsonl")
    files = sorted(glob.glob(pattern))

    month_pnl: dict[str, float] = {}
    malformed_closes = 0
    malformed_lines = 0
    total_closes = 0

    for fpath in files:
        current_entry_month: str | None = None
        try:
            with open(fpath, encoding="utf-8") as f:
                for raw_line in f:
                    raw_line = raw_line.strip()
                    if not raw_line:
                        continue
                    try:
                        obj = json.loads(raw_line)
                    except json.JSONDecodeError:
                        # Count + surface corrupt lines rather than silently
                        # dropping them — truncated/disk-full journal writes
                        # would otherwise understate the matrix with no signal.
                        malformed_lines += 1
                        continue

                    event = obj.get("event", "")

                    if event == "open":
                        ts = obj.get("ts", "")
                        # ts is RFC3339: "2021-03-15T04:00:00Z" → slice first 7 chars
                        current_entry_month = ts[:7] if len(ts) >= 7 else None

                    elif event == "close":
                        total_closes += 1
                        pnl = obj.get("pnl_usd", 0.0)
                        if pnl is None:
                            pnl = 0.0
                        if current_entry_month is None:
                            malformed_closes += 1
                            print(
                                f"WARNING: close event without preceding open in {fpath}",
                                file=sys.stderr,
                            )
                            continue
                        month_pnl[current_entry_month] = (
                            month_pnl.get(current_entry_month, 0.0) + pnl
                        )
                        # Do NOT reset current_entry_month — PARTIAL + final
                        # close both credit the same entry month.
        except OSError as e:
            print(f"WARNING: cannot read {fpath}: {e}", file=sys.stderr)

    if malformed_lines > 0:
        print(
            f"WARNING: {label}: {malformed_lines} malformed JSON line(s) skipped "
            "(truncated/corrupt journal writes — matrix may understate trades)",
            file=sys.stderr,
        )
    if malformed_closes > 0:
        print(
            f"WARNING: {label}: {malformed_closes}/{total_closes} close events "
            "had no preceding open (malformed journal lines)",
            file=sys.stderr,
        )

    return month_pnl


def main() -> int:
    if len(sys.argv) != 4:
        print(
            "Usage: overfit_reduce_journals.py <journals_root> <manifest_csv> <out_csv>",
            file=sys.stderr,
        )
        return 3

    journals_root, manifest_csv, out_csv = sys.argv[1], sys.argv[2], sys.argv[3]

    # Load labels in manifest order
    labels = load_labels(manifest_csv)
    if not labels:
        print("ERROR: no labels found in manifest", file=sys.stderr)
        return 1

    # Reduce each label
    all_data: dict[str, dict[str, float]] = {}
    for label in labels:
        jdir = os.path.join(journals_root, label)
        if not os.path.isdir(jdir):
            # Legitimate — a config that never matched any symbol or had errors
            all_data[label] = {}
        else:
            all_data[label] = reduce_label(journals_root, label)

    # Collect all months across all labels
    all_months: set[str] = set()
    for month_pnl in all_data.values():
        all_months.update(month_pnl.keys())

    # Guard: if every label has zero closes, something went wrong
    total_close_count = sum(len(mp) for mp in all_data.values())
    if total_close_count == 0:
        print(
            "ERROR: zero close events found across all labels — "
            "either the sweep produced no trades or journal dirs are empty. "
            "Check that --journal-dir was set and the backtest ran successfully.",
            file=sys.stderr,
        )
        return 1

    # Sort months ascending
    sorted_months = sorted(all_months)

    # Write output CSV
    os.makedirs(os.path.dirname(out_csv) if os.path.dirname(out_csv) else ".", exist_ok=True)
    with open(out_csv, "w", newline="") as f:
        writer = csv.writer(f)
        writer.writerow(["month"] + labels)
        for month in sorted_months:
            row = [month]
            for label in labels:
                net = all_data[label].get(month, 0.0)
                row.append(f"{net:.2f}")
            writer.writerow(row)

    print(f"→ Matrix written: {out_csv}  ({len(sorted_months)} months × {len(labels)} configs)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
