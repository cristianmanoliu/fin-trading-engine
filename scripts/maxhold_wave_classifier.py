#!/usr/bin/env python3
"""
maxhold_wave_classifier.py — read-only classifier for positions nearing the 504h
max-hold deadline.

WHY THIS EXISTS
---------------
The engine journals max-hold force-closes with outcome="TARGET" regardless of the
actual exit R, because the code treats the position as "completed" at the max-hold
tick. For Cond-3 (closed-winner count in the drift precheck), a trade is a WINNER
iff realized R > 0, NOT if outcome=="TARGET". This script computes realized R from
entry/stop geometry so the next checkpoint correctly classifies each close.

WHAT IT DOES
------------
- Scans results/journal_cache/*.jsonl (top-level only; skips shadow/, layer3/, etc.)
  for the LIVE cohort.
- Finds open positions whose entry_ts + 504h falls within --window-days of now (default 21).
- For each, prints:
    symbol | opened | max-hold close (UTC) | days left | current R (from fwd_paper_status)
    entry | stop | target | stop_dist_r | what R=0 means in price terms
- Also re-classifies any ALREADY-CLOSED trades in the wave window by realized R
  (catches lying "TARGET" labels).

USAGE
-----
  python3 scripts/maxhold_wave_classifier.py              # upcoming closes, window=21d
  python3 scripts/maxhold_wave_classifier.py --window-days 30
  python3 scripts/maxhold_wave_classifier.py --reclassify  # re-examine closed trades

EXIT CODES
----------
  0  — ran successfully (may show 0 upcoming closes)
  1  — runtime error

READ-ONLY. No files are written or modified.
"""
import argparse
import glob
import json
import sys
from datetime import datetime, timezone, timedelta

MAX_HOLD_HOURS = 504
JOURNAL_DIR = "results/journal_cache"


def load_events(reclassify: bool) -> list[dict]:
    """Load all top-level live-cohort journal events (skips subdirs)."""
    events = []
    pattern = f"{JOURNAL_DIR}/*.jsonl"
    for path in sorted(glob.glob(pattern)):
        with open(path) as f:
            for line in f:
                line = line.strip()
                if not line:
                    continue
                try:
                    events.append(json.loads(line))
                except json.JSONDecodeError:
                    pass
    return events


def parse_ts(ts_str: str) -> datetime:
    return datetime.fromisoformat(ts_str.replace("Z", "+00:00"))


def compute_r(entry: float, stop: float, exit_price: float, side: str) -> float:
    """Realized R: (exit - entry) / (entry - stop) for SHORT (negated for LONG)."""
    risk = abs(entry - stop)
    if risk == 0:
        return 0.0
    move = (entry - exit_price) if side == "SHORT" else (exit_price - entry)
    return move / risk


def main():
    parser = argparse.ArgumentParser(description="Max-hold wave classifier")
    parser.add_argument("--window-days", type=float, default=21,
                        help="Show positions whose max-hold close is within this many days (default 21)")
    parser.add_argument("--reclassify", action="store_true",
                        help="Re-examine already-closed trades in the wave window by realized R")
    args = parser.parse_args()

    now = datetime.now(timezone.utc)
    window_end = now + timedelta(days=args.window_days)

    events = load_events(args.reclassify)

    # Build open-position map: symbol -> last-open event without a subsequent close
    open_map: dict[str, dict] = {}
    closed_pairs: list[tuple[dict, dict]] = []  # (open_event, close_event)

    opens_by_symbol: dict[str, list[dict]] = {}
    for ev in events:
        sym = ev.get("symbol", "")
        evt = ev.get("event", "")
        if evt == "open":
            opens_by_symbol.setdefault(sym, []).append(ev)
        elif evt == "close":
            sym_opens = opens_by_symbol.get(sym, [])
            if sym_opens:
                open_ev = sym_opens.pop(0)
                closed_pairs.append((open_ev, ev))

    # Remaining unmatched opens = currently open positions
    for sym, sym_opens in opens_by_symbol.items():
        if sym_opens:
            open_map[sym] = sym_opens[-1]  # latest unmatched open

    # --- UPCOMING MAX-HOLD CLOSES ---
    upcoming = []
    for sym, open_ev in open_map.items():
        opened_ts = parse_ts(open_ev["ts"])
        max_hold_close = opened_ts + timedelta(hours=MAX_HOLD_HOURS)
        if now <= max_hold_close <= window_end:
            days_left = (max_hold_close - now).total_seconds() / 86400
            upcoming.append((days_left, sym, open_ev, max_hold_close))

    upcoming.sort()

    if upcoming:
        print(f"\n{'='*80}")
        print(f"  MAX-HOLD WAVE — closes within {args.window_days:.0f}d  [{now.strftime('%Y-%m-%d %H:%M UTC')}]")
        print(f"{'='*80}\n")
        print(f"  {'Symbol':<16} {'Opened':<22} {'Max-hold close':<22} {'Days left':>10}  {'Outcome label'}")
        print(f"  {'-'*16} {'-'*22} {'-'*22} {'-'*10}  {'-'*14}")
        for days_left, sym, open_ev, max_hold_close in upcoming:
            print(f"  {sym:<16} {open_ev['ts']:<22} {max_hold_close.strftime('%Y-%m-%d %H:%M UTC'):<22} {days_left:>9.1f}d  will be 'TARGET' (journal label lies)")

        print()
        print("  Entry/stop/target geometry + realized-R breakeven prices:")
        print()
        for days_left, sym, open_ev, max_hold_close in upcoming:
            entry  = open_ev.get("entry", 0)
            stop   = open_ev.get("stop",  0)
            target = open_ev.get("target", 0)
            side   = open_ev.get("side", "SHORT")
            risk   = abs(entry - stop)
            # Price at R=0 is the entry price (exit == entry → 0 gain/loss on pts)
            # Price for the winner threshold is entry ± epsilon (just above 0R)
            # r0_price is the entry either way; stop (−1R) is printed directly below
            r0_price = entry

            print(f"  {sym}")
            print(f"    side={side}  entry={entry}  stop={stop}  target={target}")
            print(f"    risk_per_1R={risk:.6g}  →  WINNER iff exit < {r0_price:.6g} (SHORT: price must drop from entry)")
            print(f"    LOSER  iff exit > {r0_price:.6g} (stop hit or expired above entry)")
            print(f"    TARGET iff exit reaches {target:.6g} (full 6R)")
            print()
    else:
        print(f"\nNo open positions with max-hold close within {args.window_days:.0f} days (from {now.strftime('%Y-%m-%d %H:%M UTC')}).\n")

    # --- RECLASSIFY CLOSED TRADES IN WAVE WINDOW ---
    if args.reclassify:
        wave_closed = []
        wave_start = now - timedelta(days=args.window_days)
        for open_ev, close_ev in closed_pairs:
            close_ts = parse_ts(close_ev["ts"])
            if wave_start <= close_ts <= now:
                wave_closed.append((close_ts, open_ev, close_ev))

        wave_closed.sort(key=lambda x: x[0])

        if wave_closed:
            print(f"\n{'='*80}")
            print(f"  RECENTLY CLOSED (last {args.window_days:.0f}d) — re-classified by realized R")
            print(f"{'='*80}\n")
            print(f"  {'Symbol':<16} {'Close ts':<22} {'Journal label':<14} {'Realized R':>10}  {'Correct class'}")
            print(f"  {'-'*16} {'-'*22} {'-'*14} {'-'*10}  {'-'*13}")
            for close_ts, open_ev, close_ev in wave_closed:
                sym    = open_ev["symbol"]
                entry  = close_ev.get("entry", open_ev.get("entry", 0))
                stop   = close_ev.get("stop",  open_ev.get("stop",  0))
                exit_p = close_ev.get("exit", 0)
                side   = close_ev.get("side", open_ev.get("side", "SHORT"))
                label  = close_ev.get("outcome", "?")
                r      = compute_r(entry, stop, exit_p, side)
                correct = "WINNER" if r > 0 else "LOSER"
                flag = "  ← LABEL LIES" if (label == "TARGET" and r <= 0) or (label == "STOP" and r > 0) else ""
                print(f"  {sym:<16} {close_ts.strftime('%Y-%m-%d %H:%M'):<22} {label:<14} {r:>+9.3f}R  {correct}{flag}")
            print()
        else:
            print(f"\nNo closed trades in the last {args.window_days:.0f} days to reclassify.\n")

    return 0


if __name__ == "__main__":
    sys.exit(main())
