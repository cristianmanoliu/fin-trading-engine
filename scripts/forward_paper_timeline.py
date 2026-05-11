#!/usr/bin/env python3
"""
forward_paper_timeline.py — Project when each STAGE_0 promotion gate fires.

Operator-facing capability: given the current forward-paper state, project
the calendar dates when each LOCKED deploy gate (CLAUDE.md "## Forward-paper
go/no-go criteria") will be satisfied AT OBSERVED OR HISTORICAL TRADE RATE.

Why this matters: CLAUDE.md flags the internal inconsistency between the
two trade-count gates:

  "the documented '≥150 trades AND ≥60 days' gate is internally inconsistent
   at the historical fleet trade rate of ~1.18 trades/day. 60 days yields
   ~70 trades on average; ~127 days are needed to hit 150 trades."

Operator needs visibility into which gate fires last (binding constraint),
when STAGE_1 becomes mechanically reachable, and how today's trade rate
compares to the historical fleet rate. Without this projection, the
operator must compute mentally each week — error-prone under decision
pressure.

Output: a paste-ready table showing:
  - Days elapsed since forward-paper start (locked 2026-05-05)
  - Trades closed to date
  - Observed trade rate (trades / days_since_start)
  - Projected calendar date for each gate
  - Binding gate (the one that fires LAST)
  - Earliest STAGE_1 promotion-ready date

Exit codes:
  0  Projection successful
  2  Insufficient data (zero trades; can't compute observed rate)
  3  Input error (journal dir missing / corrupt / etc.)

Usage:
  python3 scripts/forward_paper_timeline.py [--journal-dir DIR] [--quiet]

  --journal-dir DIR   Local journal directory (default: /var/log/paper-live/journal)
  --rate RATE         Override observed-rate with explicit value (trades/day).
                      Useful for what-if scenarios; defaults to actual observed.
  --vps VPS           Pull journal from remote VPS via ssh+tar (e.g. root@host)
  --quiet             Single-line summary instead of full table

The locked forward-paper start (2026-05-05) is hardcoded from CLAUDE.md's
"Forward-paper validation started 2026-05-05 20:06 UTC" line. If this
date is ever amended via pre-reg, update FORWARD_PAPER_START_DATE.
"""
from __future__ import annotations

import argparse
import json
import shlex
import subprocess
import sys
import tempfile
from dataclasses import dataclass
from datetime import datetime, timedelta, timezone
from pathlib import Path


# CLAUDE.md "## Strategy status / forward-paper validation started 2026-05-05 20:06 UTC"
FORWARD_PAPER_START = datetime(2026, 5, 5, 20, 6, tzinfo=timezone.utc)

# Locked criteria (mirrored from CLAUDE.md "## Forward-paper go/no-go criteria")
# These MUST stay aligned with scripts/test_criterion_coverage.py LOCKED dict.
MIN_TRADES = 150
MIN_DAYS = 60

# Historical fleet rate for projection fallback when no trades observed yet.
# Per CLAUDE.md: "fleet rate 1.18 trades/day" (16 engines × ~0.074 trades/sym/day).
HISTORICAL_FLEET_RATE = 1.18


@dataclass
class Projection:
    days_elapsed: int
    trades_closed: int
    observed_rate_per_day: float        # actual rate from data
    used_rate_per_day: float            # rate used for projection (may be overridden)
    rate_source: str                    # "observed" | "historical_fleet" | "override"
    calendar_gate_date: datetime        # day MIN_DAYS from start
    trade_gate_date: datetime           # date n=MIN_TRADES reached at used_rate
    binding_gate: str                   # "calendar" | "trade-count"
    earliest_stage_1: datetime          # max of the two
    days_to_stage_1: int                # days from now to earliest_stage_1


def count_live_trades(journal_dir: Path) -> tuple[int, int]:
    """Walk top-level *.jsonl in journal_dir (NOT shadow/*) and count closed
    LIVE trades. Skips PARTIAL closes — matches stage_promotion_check's
    load_journal semantics so this tool's projection aligns with the formal
    gate evaluator.

    Returns (closes_count, files_scanned). closes_count is the trade-count
    that the n>=150 gate evaluates against."""
    if not journal_dir.is_dir():
        raise FileNotFoundError(f"journal dir not found: {journal_dir}")
    closes = 0
    files_scanned = 0
    for jf in sorted(journal_dir.glob("*-*.jsonl")):
        files_scanned += 1
        for line in jf.read_text().splitlines():
            if not line:
                continue
            try:
                ev = json.loads(line)
            except json.JSONDecodeError:
                continue
            if ev.get("event") != "close":
                continue
            if ev.get("outcome") == "PARTIAL":
                continue
            closes += 1
    return closes, files_scanned


def fetch_remote_trades(vps: str, remote_dir: str) -> int:
    """Tar+ssh fetch remote journal into a tempdir, count locally.
    List-arg subprocess + shlex.quote on remote_dir mirrors the
    locked shell-injection-via-f-string defense (T2 sibling fixes)."""
    remote_cmd = f"tar -cf - -C {shlex.quote(remote_dir)} . 2>/dev/null"
    with tempfile.TemporaryDirectory() as tmp:
        result = subprocess.run(
            ["ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10",
             vps, remote_cmd],
            check=True, capture_output=True)
        subprocess.run(["tar", "-xf", "-", "-C", tmp],
                       input=result.stdout, check=True)
        closes, _ = count_live_trades(Path(tmp))
        return closes


def project(trades_closed: int, override_rate: float | None = None,
            now: datetime | None = None) -> Projection:
    """Compute the projection given current state. Pure function — no I/O.

    If override_rate is set, uses it directly. Otherwise computes the
    observed rate from trades_closed / days_elapsed. If days_elapsed<1
    or observed_rate is 0 (no trades yet), falls back to historical
    fleet rate (1.18/day) per CLAUDE.md."""
    if now is None:
        now = datetime.now(timezone.utc)

    elapsed = now - FORWARD_PAPER_START
    days_elapsed = max(0, elapsed.days)

    observed_rate = trades_closed / days_elapsed if days_elapsed > 0 else 0.0

    if override_rate is not None:
        used_rate = override_rate
        rate_source = "override"
    elif observed_rate > 0:
        used_rate = observed_rate
        rate_source = "observed"
    else:
        used_rate = HISTORICAL_FLEET_RATE
        rate_source = "historical_fleet"

    # Calendar gate: day MIN_DAYS from start.
    calendar_gate_date = FORWARD_PAPER_START + timedelta(days=MIN_DAYS)

    # Trade gate: at used_rate trades/day, days-to-MIN_TRADES = MIN_TRADES/used_rate.
    if used_rate > 0:
        days_to_trade_gate = MIN_TRADES / used_rate
    else:
        days_to_trade_gate = float("inf")
    trade_gate_date = FORWARD_PAPER_START + timedelta(days=days_to_trade_gate)

    # Binding gate = the one that fires LATER (operator needs both).
    if trade_gate_date > calendar_gate_date:
        binding_gate = "trade-count"
        earliest = trade_gate_date
    else:
        binding_gate = "calendar"
        earliest = calendar_gate_date

    days_to_stage_1 = max(0, (earliest - now).days)

    return Projection(
        days_elapsed=days_elapsed,
        trades_closed=trades_closed,
        observed_rate_per_day=observed_rate,
        used_rate_per_day=used_rate,
        rate_source=rate_source,
        calendar_gate_date=calendar_gate_date,
        trade_gate_date=trade_gate_date,
        binding_gate=binding_gate,
        earliest_stage_1=earliest,
        days_to_stage_1=days_to_stage_1,
    )


def render(p: Projection, quiet: bool = False) -> str:
    """Format the projection as paste-ready text. Returns a single string."""
    if quiet:
        return (
            f"forward-paper: day {p.days_elapsed} / "
            f"{p.trades_closed} trades / "
            f"rate={p.used_rate_per_day:.2f}/d ({p.rate_source}) / "
            f"binding={p.binding_gate} / "
            f"STAGE_1 earliest {p.earliest_stage_1.strftime('%Y-%m-%d')} "
            f"({p.days_to_stage_1}d)")

    sep = "─" * 70
    lines = [
        sep,
        "  Forward-paper STAGE_1 promotion timeline",
        f"  Start: {FORWARD_PAPER_START.strftime('%Y-%m-%d %H:%M UTC')} (locked per CLAUDE.md)",
        f"  Now:   {datetime.now(timezone.utc).strftime('%Y-%m-%d %H:%M UTC')}",
        sep,
        "",
        f"  Days elapsed:        {p.days_elapsed:>5d}d",
        f"  Trades closed:       {p.trades_closed:>5d}  (live cohort, excludes PARTIAL)",
        f"  Observed rate:       {p.observed_rate_per_day:>5.2f} trades/day",
        f"  Rate used for proj:  {p.used_rate_per_day:>5.2f} trades/day  ({p.rate_source})",
        "",
        "  Locked criteria (CLAUDE.md):",
        f"    ≥{MIN_DAYS}d calendar elapsed       fires {p.calendar_gate_date.strftime('%Y-%m-%d')}",
        f"    ≥{MIN_TRADES} trades at {p.used_rate_per_day:.2f}/d   fires {p.trade_gate_date.strftime('%Y-%m-%d')}",
        "",
        f"  Binding gate: {p.binding_gate.upper()}",
        f"  Earliest STAGE_1 ready: {p.earliest_stage_1.strftime('%Y-%m-%d')}  ({p.days_to_stage_1}d remaining)",
        "",
    ]

    # Add advisory notes for common cases.
    if p.rate_source == "historical_fleet":
        lines.append("  Note: zero observed trades yet — projection uses historical")
        lines.append(f"  fleet rate {HISTORICAL_FLEET_RATE}/day. Re-run after first close for")
        lines.append("  observed-rate projection.")
        lines.append("")
    elif p.rate_source == "observed":
        if p.observed_rate_per_day < HISTORICAL_FLEET_RATE * 0.7:
            lines.append("  Note: observed rate is well below historical fleet rate")
            lines.append(f"  ({p.observed_rate_per_day:.2f}/d vs historical {HISTORICAL_FLEET_RATE}/d).")
            lines.append("  Trade-gate projection may slip if rate doesn't recover.")
            lines.append("")
        elif p.observed_rate_per_day > HISTORICAL_FLEET_RATE * 1.3:
            lines.append("  Note: observed rate well above historical fleet rate.")
            lines.append("  Trade-gate projection may arrive sooner than expected.")
            lines.append("")

    if p.binding_gate == "trade-count":
        lines.append("  Binding constraint is trade-count, NOT calendar. Calendar")
        lines.append(f"  alone is satisfied at day {MIN_DAYS}, but n=150 is the gate")
        lines.append("  that holds back STAGE_1.")
    else:
        lines.append("  Binding constraint is calendar; trade-count gate projects to")
        lines.append("  fire earlier. Both must hold for STAGE_1 readiness.")

    lines.append("")
    lines.append(sep)
    return "\n".join(lines)


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--journal-dir", default="/var/log/paper-live/journal",
                    help="Local journal directory")
    ap.add_argument("--vps", default="",
                    help="Remote VPS to pull journal from (e.g. root@host); "
                         "implies --journal-dir is interpreted as the REMOTE path")
    ap.add_argument("--rate", type=float, default=None,
                    help="Override the trade rate (trades/day) for what-if scenarios")
    ap.add_argument("--quiet", action="store_true",
                    help="One-line summary instead of full table")
    args = ap.parse_args()

    try:
        if args.vps:
            trades = fetch_remote_trades(args.vps, args.journal_dir)
        else:
            jdir = Path(args.journal_dir)
            trades, files = count_live_trades(jdir)
            if files == 0:
                print(f"ERROR: no *.jsonl files found in {jdir}",
                      file=sys.stderr)
                print("  (this looks like a fresh-deploy or wrong directory; "
                      "set --journal-dir explicitly)", file=sys.stderr)
                return 3
    except FileNotFoundError as e:
        print(f"ERROR: {e}", file=sys.stderr)
        return 3
    except subprocess.CalledProcessError as e:
        print(f"ERROR: ssh fetch failed: rc={e.returncode}",
              file=sys.stderr)
        return 3

    p = project(trades, override_rate=args.rate)
    print(render(p, quiet=args.quiet))
    return 0


if __name__ == "__main__":
    sys.exit(main())
