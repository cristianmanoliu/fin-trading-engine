#!/usr/bin/env python3
"""
forward_paper_trajectory.py — render the forward-paper trajectory across
all dated snapshots in results/forward_paper_snapshots/.

forward_paper_status.sh produces a point-in-time view; weekly_audit.sh
captures it to a dated text file. With ≥2 snapshots, the operator wants
to see *direction-of-travel* — is PnL trending toward kill territory, are
costs creeping up, is single-symbol concentration drifting?

This script parses the snapshots and emits a per-cohort trajectory table:
one column per dated snapshot, one row per metric (trades, WR, PnL, fee,
slip, single-sym %). Δ-from-first appended for quick visual scan.

Usage:
  python3 scripts/forward_paper_trajectory.py
  python3 scripts/forward_paper_trajectory.py --dir results/forward_paper_snapshots
  python3 scripts/forward_paper_trajectory.py --cohort live   # single cohort

Exit codes:
  0  rendered ≥1 snapshot successfully
  2  no snapshots found / dir missing
  3  parse error on ≥1 snapshot (continues rendering valid ones)
"""
from __future__ import annotations

import argparse
import re
import sys
from dataclasses import dataclass, field
from pathlib import Path

DEFAULT_DIR = Path("results/forward_paper_snapshots")

# Cohort header markers — `── live ───...` or `── shadow/alt5-15-336 ──...`
COHORT_RE = re.compile(r"──\s+(live|shadow/[A-Za-z0-9_-]+)\s+─+")

# No-closes cohort lines emitted by forward_paper_status.sh when a cohort has
# zero terminal closes — either `  shadow/bb20  no closes yet — N open` or
# `  shadow/bb20  (no data yet)`. Without this match, cohorts in their pre-
# first-close window are silently invisible across the whole trajectory.
NO_CLOSES_RE = re.compile(
    r"^\s+(live|shadow/[A-Za-z0-9_-]+)\s+(?:no closes yet|\(no data yet\))"
)

# Metric line patterns. Tolerant of whitespace; capture numeric content.
# Numerics allow optional decimal portion so that a future format change in
# forward_paper_status.sh (e.g. emitting cents on PnL) doesn't silently
# truncate to integer dollars and quietly mis-render the trajectory delta.
METRICS = [
    ("days",       re.compile(r"Days elapsed:\s+(\d+)\s*/")),
    ("trades",     re.compile(r"Trades closed:\s+(\d+)\s*/")),
    ("wr_pct",     re.compile(r"Wins\s*/\s*WR:\s+\d+\s*/\s*([\d.]+)%")),
    ("pnl_usd",    re.compile(r"Net PnL:\s+\$([\-+]?\d+(?:\.\d+)?)")),
    ("fee_bps",    re.compile(r"Realized fee bps:\s+([\d.]+)")),
    ("slip_bps",   re.compile(r"Realized slip bps:\s+([\d.]+)")),
    ("single_pct", re.compile(r"Single-sym pct:\s+([\d.]+)%")),
]


@dataclass
class Snapshot:
    date: str  # YYYY-MM-DD from filename
    cohorts: dict[str, dict[str, str]] = field(default_factory=dict)


def parse_snapshot(path: Path) -> Snapshot:
    """Parse one snapshot file. Returns Snapshot with per-cohort metric dicts."""
    snap = Snapshot(date=path.stem)  # e.g. "2026-05-08"
    text = path.read_text()

    # Split into cohort sections by the header line. A no-closes cohort
    # (single-line `shadow/bb20  no closes yet — N open`) terminates the
    # current section without starting a new ── header section, but still
    # registers the cohort so trajectory tracks its first close when it lands.
    sections: list[tuple[str, str]] = []
    no_closes_cohorts: list[str] = []
    current_cohort: str | None = None
    current_lines: list[str] = []
    for line in text.splitlines():
        m = COHORT_RE.search(line)
        if m:
            if current_cohort is not None:
                sections.append((current_cohort, "\n".join(current_lines)))
            current_cohort = m.group(1)
            current_lines = []
            continue
        nc = NO_CLOSES_RE.match(line)
        if nc:
            if current_cohort is not None:
                sections.append((current_cohort, "\n".join(current_lines)))
                current_cohort = None
                current_lines = []
            no_closes_cohorts.append(nc.group(1))
            continue
        if current_cohort is not None:
            current_lines.append(line)
    if current_cohort is not None:
        sections.append((current_cohort, "\n".join(current_lines)))

    for cohort, body in sections:
        metrics = {}
        for name, pat in METRICS:
            m = pat.search(body)
            if m:
                metrics[name] = m.group(1)
        snap.cohorts[cohort] = metrics
    for cohort in no_closes_cohorts:
        # Only register if we didn't already see a full ── section for the
        # same cohort in the same file (defensive against malformed input).
        snap.cohorts.setdefault(cohort, {"trades": "0"})
    return snap


def render_trajectory(snapshots: list[Snapshot], cohort_filter: str | None = None) -> str:
    """Render per-cohort trajectory tables. Returns the full string."""
    if not snapshots:
        return "(no snapshots)\n"

    # Collect cohort union across snapshots; preserve canonical order.
    canonical_order = ["live", "shadow/alt5-15-336", "shadow/alt5-15-504", "shadow/bb20"]
    seen = {c for s in snapshots for c in s.cohorts}
    cohorts = [c for c in canonical_order if c in seen] + sorted(seen - set(canonical_order))
    if cohort_filter:
        cohorts = [c for c in cohorts if c == cohort_filter]
        if not cohorts:
            return f"(cohort {cohort_filter!r} not found in any snapshot)\n"

    sep = "═" * 78
    out: list[str] = []
    out.append(sep)
    if len(snapshots) == 1:
        out.append(f"  Forward-paper trajectory — 1 snapshot ({snapshots[0].date})")
        out.append("  Need ≥2 snapshots to show direction-of-travel; current view is")
        out.append("  baseline-only. Re-run after the next weekly_audit cron firing.")
    else:
        out.append(f"  Forward-paper trajectory — {len(snapshots)} snapshots "
                   f"({snapshots[0].date} → {snapshots[-1].date})")
    out.append(sep)
    out.append("")

    metric_labels = [
        ("days",       "Days elapsed"),
        ("trades",     "Trades closed"),
        ("wr_pct",     "WR (%)"),
        ("pnl_usd",    "Net PnL ($)"),
        ("fee_bps",    "Fee (bp)"),
        ("slip_bps",   "Slip (bp)"),
        ("single_pct", "Single-sym (%)"),
    ]

    for cohort in cohorts:
        out.append(f"  cohort: {cohort}")
        # Header: metric column + N date columns + Δ
        date_cols = [s.date for s in snapshots]
        header = f"    {'metric':<15} " + " ".join(f"{d:>11}" for d in date_cols)
        if len(snapshots) >= 2:
            header += f"  {'Δ':>9}"
        out.append(header)
        out.append("    " + "─" * (15 + 12 * len(date_cols) + (12 if len(snapshots) >= 2 else 0)))

        for key, label in metric_labels:
            row = f"    {label:<15} "
            values: list[str] = []
            numerics: list[float] = []
            for s in snapshots:
                v = s.cohorts.get(cohort, {}).get(key, "—")
                values.append(v)
                try:
                    numerics.append(float(v))
                except (ValueError, TypeError):
                    numerics.append(float("nan"))
            row += " ".join(f"{v:>11}" for v in values)
            if len(snapshots) >= 2:
                first = numerics[0]
                last = numerics[-1]
                if first == first and last == last:  # not NaN
                    delta = last - first
                    sign = "+" if delta >= 0 else ""
                    row += f"  {sign}{delta:>8.2f}"
                else:
                    row += f"  {'—':>9}"
            out.append(row)
        out.append("")

    out.append(sep)
    return "\n".join(out) + "\n"


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--dir", type=Path, default=DEFAULT_DIR,
                    help=f"Snapshot directory (default: {DEFAULT_DIR})")
    ap.add_argument("--cohort", default=None,
                    help="Render only this cohort (e.g. 'live', 'shadow/bb20')")
    args = ap.parse_args()

    if not args.dir.is_dir():
        print(f"snapshot dir not found: {args.dir}", file=sys.stderr)
        return 2

    files = sorted(args.dir.glob("*.txt"))
    if not files:
        print(f"no snapshots in {args.dir}", file=sys.stderr)
        return 2

    snapshots: list[Snapshot] = []
    parse_errors: list[tuple[Path, Exception]] = []
    for f in files:
        try:
            snapshots.append(parse_snapshot(f))
        except Exception as e:  # noqa: BLE001 — caller-friendly catch
            parse_errors.append((f, e))

    # Cohort filter validation runs BEFORE rendering so a typo'd --cohort
    # surfaces as exit 2 (no data) instead of exit 0 + diagnostic-in-stdout
    # — same fail-open shape as realized_cost_trajectory's bad --cohort fix.
    if args.cohort:
        all_cohorts = {c for s in snapshots for c in s.cohorts}
        if args.cohort not in all_cohorts:
            print(f"cohort {args.cohort!r} not found in any of "
                  f"{len(snapshots)} snapshot(s); available cohorts: "
                  f"{', '.join(sorted(all_cohorts)) or '(none)'}",
                  file=sys.stderr)
            return 2

    print(render_trajectory(snapshots, cohort_filter=args.cohort), end="")

    if parse_errors:
        print(f"\n⚠ {len(parse_errors)} snapshot(s) failed to parse:", file=sys.stderr)
        for path, err in parse_errors:
            print(f"  {path}: {err}", file=sys.stderr)
        return 3
    # Files parsed without exceptions but no cohort sections matched anywhere.
    # Without this guard, a format change in forward_paper_status.sh (different
    # header characters, renamed cohorts, etc.) renders a near-empty trajectory
    # and exits 0 — the audit-pattern fail-open ("missing input → silent
    # success"). Exit 3 surfaces it loudly.
    if snapshots and not any(s.cohorts for s in snapshots):
        print(f"\n⚠ parsed {len(snapshots)} snapshot(s) but extracted zero "
              "cohorts — forward_paper_status.sh format may have changed; "
              "check COHORT_RE / NO_CLOSES_RE patterns.", file=sys.stderr)
        return 3
    return 0


if __name__ == "__main__":
    sys.exit(main())
