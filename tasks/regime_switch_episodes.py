#!/usr/bin/env python3
# tasks/regime_switch_episodes.py — dwell-aware SHORT-episode builder for the
# regime-SWITCH (full-stop) test. Converts a daily SHORT/LONG/FLAT timeline into
# SHORT trading episodes. LONG and FLAT both = OFF (no long side). An OFF gap
# shorter than `dwell` days is absorbed into the surrounding SHORT episode
# (whipsaw guard); a SHORT episode ends only on a confirmed >=dwell-day OFF run,
# at the FIRST day of that run (force-close fires there). Pure + causal:
# the dwell look-ahead is bounded and only used to decide whether an already-
# observed OFF gap closes the episode — backtests still run forward inside the
# slice, so no future price leaks into a trade decision.
#
# Episodes are returned as (start_date, end_date_exclusive) ISO-date strings.
import csv
import datetime
import sys


def _next_day(d):
    return (datetime.date.fromisoformat(d) + datetime.timedelta(days=1)).isoformat()


def short_episodes(rows, dwell):
    """rows: list of (iso_date, label). label in {SHORT,LONG,FLAT}.
    Returns list of (start, end_exclusive) SHORT episodes, dwell-aware."""
    # Binary: is each day SHORT?
    days = [(d, lab == "SHORT") for d, lab in rows]
    n = len(days)
    eps = []
    i = 0
    while i < n:
        if not days[i][1]:
            i += 1
            continue
        # Start of a SHORT episode.
        start = days[i][0]
        last_short_idx = i
        j = i + 1
        while j < n:
            if days[j][1]:
                last_short_idx = j
                j += 1
                continue
            # OFF day at j — measure the OFF run length.
            k = j
            while k < n and not days[k][1]:
                k += 1
            off_len = k - j
            if off_len >= dwell:
                # Confirmed regime exit — episode ends at first OFF day (j).
                break
            # Sub-dwell gap — absorb, keep scanning from k.
            j = k
        else:
            # reached end of data without a confirmed OFF — close at series end.
            j = n
        # end-exclusive: if we broke on a confirmed-OFF at index j, the episode
        # ends at days[j] (the first OFF day) → end-exclusive = that date.
        # if we ran to the end, end-exclusive = day after last SHORT.
        if j < n:
            end_excl = days[j][0]
        else:
            end_excl = _next_day(days[last_short_idx][0])
        eps.append((start, end_excl))
        i = j
        # skip the confirmed-OFF run before the next possible episode
        while i < n and not days[i][1]:
            i += 1
    return eps


def _load(path):
    with open(path, newline="") as f:
        r = csv.reader(f)
        rows = list(r)
    return rows[1:] if rows and rows[0][0] == "date" else rows


def main(argv=None):
    import argparse
    ap = argparse.ArgumentParser()
    ap.add_argument("--timeline", required=True,
                    help="CSV date,label from regime_label.py")
    ap.add_argument("--dwell", type=int, default=3)
    args = ap.parse_args(argv)
    rows = _load(args.timeline)
    w = csv.writer(sys.stdout)
    for s, e in short_episodes(rows, args.dwell):
        w.writerow([s, e, "SHORT", s[:4]])
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
