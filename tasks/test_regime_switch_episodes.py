# tasks/test_regime_switch_episodes.py
import datetime
from regime_switch_episodes import short_episodes

def L(*labels):
    """Helper: build (date,label) rows starting 2020-01-01."""
    base = datetime.date(2020, 1, 1)
    return [((base + datetime.timedelta(days=i)).isoformat(), lab)
            for i, lab in enumerate(labels)]

def test_single_short_run():
    # 3 SHORT days, then OFF forever → one episode covering the 3 days,
    # end-exclusive = day 4 (2020-01-04).
    rows = L("SHORT", "SHORT", "SHORT", "FLAT", "FLAT", "FLAT")
    eps = short_episodes(rows, dwell=3)
    assert eps == [("2020-01-01", "2020-01-04")]

def test_short_gap_below_dwell_is_absorbed():
    # SHORT, 2-day FLAT gap (< dwell=3), SHORT → ONE merged episode.
    # The sub-dwell OFF days stay inside the episode (we keep trading through them).
    rows = L("SHORT", "FLAT", "FLAT", "SHORT", "SHORT", "LONG", "LONG", "LONG")
    eps = short_episodes(rows, dwell=3)
    # episode spans day0..day4 inclusive; ends at first confirmed-OFF day (day5,
    # the start of the 3-day LONG run) → end-exclusive = 2020-01-06.
    assert eps == [("2020-01-01", "2020-01-06")]

def test_short_gap_at_or_above_dwell_splits():
    # SHORT, 3-day FLAT (>= dwell), SHORT → TWO episodes (force-close at the gap).
    rows = L("SHORT", "FLAT", "FLAT", "FLAT", "SHORT", "SHORT")
    eps = short_episodes(rows, dwell=3)
    assert eps == [("2020-01-01", "2020-01-02"), ("2020-01-05", "2020-01-07")]

def test_trailing_short_run_closed_at_series_end():
    # SHORT run extending to the end of data → episode ends day-after-last.
    rows = L("FLAT", "SHORT", "SHORT")
    eps = short_episodes(rows, dwell=3)
    assert eps == [("2020-01-02", "2020-01-04")]

def test_no_short_days():
    rows = L("FLAT", "LONG", "FLAT")
    assert short_episodes(rows, dwell=3) == []

def test_dwell_one_single_off_splits():
    # dwell=1: every single OFF day ends the episode (boundary of off_len >= dwell).
    rows = L("SHORT", "FLAT", "SHORT")
    assert short_episodes(rows, dwell=1) == [("2020-01-01", "2020-01-02"), ("2020-01-03", "2020-01-04")]

def test_trailing_absorbed_gap_then_series_end():
    # Episode ends via series-end (else: branch) but the final days were an
    # absorbed sub-dwell OFF gap — exercises last_short_idx tracking, not j-1.
    rows = L("SHORT", "FLAT", "SHORT", "FLAT", "FLAT")  # trailing gap [3,4] len 2 < dwell 3 → absorbed
    assert short_episodes(rows, dwell=3) == [("2020-01-01", "2020-01-04")]
