#!/usr/bin/env python3
"""macro_event_study.py — candidate #14: macro-event window playbook (CPI/FOMC/NFP).

Event study on local 1m klines around deterministic, published US macro releases.
Since ~2022 BTC trades as a macro asset; event timestamps are exogenous — a clock the
price series cannot see. Per `results/strategy_candidates_batch2_2026-06-10.md` (#14).

CALENDAR (built in this file):
  NFP  — first Friday of month, 13:30 UTC (8:30 ET; ignores ~1h DST drift, fine for the
         post-event-drift windows we test).
  CPI  — BLS published release dates, 13:30 UTC (8:30 ET). Hardcoded from the BLS schedule.
  FOMC — statement release days, 19:00 UTC (2:00 ET). Hardcoded from the Fed calendar.

TWO PRE-REGISTERED VARIANTS ONLY (no grid — spec discipline):
  (a) MOMENTUM: at +30min after the event, take the sign of the 0..30min move and hold
      it for 24h. (follow the post-event direction)
  (b) FADE: at +15min, fade the first 15min move, hold 4h. (fade the spike)

================================  PRE-REGISTRATION  ============================
  Symbols:        BTCUSDT, ETHUSDT (the macro-sensitive majors).
  Cost:           15 bp round-trip.
  Gate-4 nuance:  macro coupling begins ~2022; 2020-21 expected dead. Report by-year;
                  2022-26 must span sub-regimes (it does: 2022 bear / 23-24 bull / 25-26).
  Honesty:        mean + median + win + drop-top-5% per variant per event type.
  ACCEPT:         a variant×event with gross>=30bp, median net>0, survives top-5%,
                  positive across 2022-26 sub-regimes.
===============================================================================
"""
import os, glob, csv, math, sys, datetime
from collections import defaultdict
import numpy as np

DATA = "data"
MIN_MS = 60_000
COST_BP = 15.0
SYMS = ["BTCUSDT", "ETHUSDT"]


def first_friday(y, m):
    d = datetime.date(y, m, 1)
    while d.weekday() != 4:
        d += datetime.timedelta(days=1)
    return d


# FOMC statement days (19:00 UTC). Published Fed calendar 2020-2026.
FOMC = [
    "2020-01-29", "2020-03-03", "2020-03-15", "2020-04-29", "2020-06-10", "2020-07-29",
    "2020-09-16", "2020-11-05", "2020-12-16",
    "2021-01-27", "2021-03-17", "2021-04-28", "2021-06-16", "2021-07-28", "2021-09-22",
    "2021-11-03", "2021-12-15",
    "2022-01-26", "2022-03-16", "2022-05-04", "2022-06-15", "2022-07-27", "2022-09-21",
    "2022-11-02", "2022-12-14",
    "2023-02-01", "2023-03-22", "2023-05-03", "2023-06-14", "2023-07-26", "2023-09-20",
    "2023-11-01", "2023-12-13",
    "2024-01-31", "2024-03-20", "2024-05-01", "2024-06-12", "2024-07-31", "2024-09-18",
    "2024-11-07", "2024-12-18",
    "2025-01-29", "2025-03-19", "2025-05-07", "2025-06-18", "2025-07-30", "2025-09-17",
    "2025-11-05", "2025-12-17",
    "2026-01-28", "2026-03-18", "2026-04-29",
]

# CPI release days (13:30 UTC). BLS published schedule 2020-2026.
CPI = [
    "2020-01-14", "2020-02-13", "2020-03-11", "2020-04-10", "2020-05-12", "2020-06-10",
    "2020-07-14", "2020-08-12", "2020-09-11", "2020-10-13", "2020-11-12", "2020-12-10",
    "2021-01-13", "2021-02-10", "2021-03-10", "2021-04-13", "2021-05-12", "2021-06-10",
    "2021-07-13", "2021-08-11", "2021-09-14", "2021-10-13", "2021-11-10", "2021-12-10",
    "2022-01-12", "2022-02-10", "2022-03-10", "2022-04-12", "2022-05-11", "2022-06-10",
    "2022-07-13", "2022-08-10", "2022-09-13", "2022-10-13", "2022-11-10", "2022-12-13",
    "2023-01-12", "2023-02-14", "2023-03-14", "2023-04-12", "2023-05-10", "2023-06-13",
    "2023-07-12", "2023-08-10", "2023-09-13", "2023-10-12", "2023-11-14", "2023-12-12",
    "2024-01-11", "2024-02-13", "2024-03-12", "2024-04-10", "2024-05-15", "2024-06-12",
    "2024-07-11", "2024-08-14", "2024-09-11", "2024-10-10", "2024-11-13", "2024-12-11",
    "2025-01-15", "2025-02-12", "2025-03-12", "2025-04-10", "2025-05-13", "2025-06-11",
    "2025-07-15", "2025-08-12", "2025-09-11", "2025-10-15", "2025-11-13", "2025-12-10",
    "2026-01-14", "2026-02-11", "2026-03-11", "2026-04-10", "2026-05-13",
]


def build_calendar():
    ev = []
    for y in range(2020, 2027):
        for m in range(1, 13):
            d = first_friday(y, m)
            if d <= datetime.date(2026, 6, 1):
                ev.append((d.isoformat(), "13:30", "NFP"))
    ev += [(d, "13:30", "CPI") for d in CPI]
    ev += [(d, "19:00", "FOMC") for d in FOMC]
    return ev


def event_ms(date_str, hm):
    h, mi = map(int, hm.split(":"))
    dt = datetime.datetime.strptime(date_str, "%Y-%m-%d").replace(
        hour=h, minute=mi, tzinfo=datetime.timezone.utc)
    return int(dt.timestamp() * 1000)


def load_closes(sym):
    cl = {}
    for fp in sorted(glob.glob(os.path.join(DATA, f"{sym}-1m-*.csv"))):
        with open(fp) as f:
            r = csv.reader(f); next(r, None)
            for row in r:
                try:
                    cl[int(row[0])] = float(row[4])
                except (ValueError, IndexError):
                    continue
    return cl


def cl_at(cl, ms):
    return cl.get((ms // MIN_MS) * MIN_MS)


def ret_bp(cl, t0, t1):
    a = cl_at(cl, t0); b = cl_at(cl, t1)
    if a is None or b is None or a <= 0 or b <= 0:
        return None
    return math.log(b / a) * 1e4


def run():
    cal = build_calendar()
    print("=" * 82)
    print(f"MACRO-EVENT WINDOWS (#14) — {len(cal)} events (NFP/CPI/FOMC), BTC+ETH")
    print("variants: (a) momentum +30min->24h  (b) fade first 15min->4h; cost 15bp")
    print("=" * 82)
    # results[(variant, evtype)] -> list of (net_bp, year, gross_abs)
    res = defaultdict(list)
    for sym in SYMS:
        cl = load_closes(sym)
        if not cl:
            continue
        for date, hm, et in cal:
            t = event_ms(date, hm)
            # variant a: sign of 0..30min, hold +30min..+30min+24h
            m30 = ret_bp(cl, t, t + 30 * MIN_MS)
            if m30 is not None and m30 != 0:
                fwd = ret_bp(cl, t + 30 * MIN_MS, t + 30 * MIN_MS + 24 * 3600_000)
                if fwd is not None:
                    sign = 1 if m30 > 0 else -1
                    res[("a_momentum", et)].append((sign * fwd - COST_BP, int(date[:4]), abs(fwd)))
            # variant b: fade first 15min, hold 4h
            m15 = ret_bp(cl, t, t + 15 * MIN_MS)
            if m15 is not None and m15 != 0:
                fwd = ret_bp(cl, t + 15 * MIN_MS, t + 15 * MIN_MS + 4 * 3600_000)
                if fwd is not None:
                    sign = -1 if m15 > 0 else 1   # fade
                    res[("b_fade", et)].append((sign * fwd - COST_BP, int(date[:4]), abs(fwd)))

    print(f"{'variant':<12}{'event':<6}{'n':>6}{'gross':>8}{'mean':>8}{'med':>8}"
          f"{'win':>6}{'t':>6}{'drop5':>8}  flag")
    print("-" * 82)
    candidates = []
    for key in sorted(res):
        variant, et = key
        rows = res[key]
        if len(rows) < 20:
            continue
        net = np.array([r[0] for r in rows]); yrs = np.array([r[1] for r in rows])
        gross = np.mean([r[2] for r in rows])
        srt = np.sort(net); k5 = max(1, int(len(srt) * 0.05))
        med = float(np.median(srt)); wr = float((srt > 0).mean())
        t = srt.mean() / (srt.std(ddof=1) / math.sqrt(len(srt))) if len(srt) > 1 else 0
        drop5 = srt[:-k5].mean()
        # post-2022 sub-regime check
        post = net[yrs >= 2022]
        flag = ""
        if gross >= 30 and med > 0 and drop5 > 0 and len(post) > 10 and post.mean() > 0:
            flag = "CANDIDATE?"
            candidates.append((key, srt.mean(), med, wr))
        print(f"{variant:<12}{et:<6}{len(net):>6}{gross:>8.1f}{srt.mean():>8.1f}{med:>8.1f}"
              f"{wr:>6.0%}{t:>6.2f}{drop5:>8.1f}  {flag}")

    print()
    print("=" * 82)
    print("VERDICT (candidate #14)")
    if candidates:
        for (v, e), m, md, wr in candidates:
            print(f"  CANDIDATE: {v}/{e} mean={m:.1f}bp med={md:.1f} win={wr:.0%} "
                  f"-> by-year + corr-to-LIVE gate")
    else:
        print("  NO-GO: no variant×event clears 30bp gross + positive median + tail + "
              "post-2022 positive. Macro-event windows carry no fee-clearing directional "
              "edge on BTC/ETH at the two pre-registered horizons.")
    print("=" * 82)


if __name__ == "__main__":
    run()
