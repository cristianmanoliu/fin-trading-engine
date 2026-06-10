# #23 BTC→alt lead-lag spillover — VERDICT: NO-GO (screen failed, no build). (2026-06-10)

**Pre-reg:** `results/strategy_candidates_batch3_2026-06-10.md` (#23), locked screen.
**Script:** `scripts/btc_leadlag_study.py`. **Cells CSV:** `results/btc_leadlag_cells_2026-06-10.csv`.

## Setup

Event study, 2020→2026-06, 56 alts. Events: BTC trailing 1H return z-scored on rolling
30d σ, |z| ≥ {2, 3}, deduped to ≥4h spacing (no overlap inflation). Forward alt returns
at 15m/1H/4H in the BTC-move direction, with fill at signal close (delay=0) and at +15m
(delay=1) for execution realism. Screen: mean > 15bp cost, event-level |t| > 3, median > 0,
drop-top-5% > 0.

## The look-ahead trap (methodological note — the real finding)

First run produced t=25–37, net +140–197bp/event, 7/7 years — absurdly strong. Audit
found the cause before any conclusion was drawn: `pandas.resample().last()` labels bars
at the LEFT edge, so the "10:00" hourly BTC close is the 11:00 price while the "10:00"
alt entry is the 10:15 price — the entry predates the signal by 45 minutes and the
"forward" window overlaps the BTC event hour. The entire effect was contemporaneous
co-movement booked as prediction. Right-edge relabeling (every timestamp = moment of its
price) eliminates it completely. **Any future study resampling these CSVs must use
right-edge labels** — this is the third instance of the silent-alignment bug class.

## Corrected results (12 cells: 2 z × 3 horizons × 2 delays)

| z | horizon | delay | n_ev | gross bp | median bp | t | drop5 bp | net bp | yrs+ |
|---|---|---|---:|---:|---:|---:|---:|---:|---|
| 2 | 15m | 0 | 2134 | +2.3 | −5.1 | 0.18 | −13.7 | −12.7 | 4/7 |
| 2 | 1H | 0 | 2134 | +0.6 | −7.6 | −0.72 | −24.5 | −14.4 | 2/7 |
| 2 | 4H | 0 | 2134 | −10.5 | −10.5 | −2.44 | −49.2 | −25.5 | 3/7 |
| 3 | 15m | 0 | 872 | +6.3 | −3.9 | 0.60 | −14.6 | −8.7 | 5/7 |
| 3 | 1H | 0 | 872 | +2.8 | −5.8 | 0.02 | −27.6 | −12.3 | 2/7 |
| 3 | 4H | 0 | 872 | −3.2 | −0.4 | −0.74 | −46.8 | −18.2 | 3/7 |

(delay=1 uniformly worse; full grid in CSV.)

**Screen-passing cells: 0 of 12.**

## Conclusion

Post-2020 the BTC→alt spillover is fully contemporaneous — alts reprice within the same
hour as BTC's move, leaving 2–6bp gross at best against 15bp cost, t ≈ 0. The weak 4H
NEGATIVE drift (t=−2.4, continuation-fade) is also inside the cost floor. Consistent with
the heavily-arbed prior and with the search-wide finding (stress observable, direction
not). **NO-GO; no strategy build; family closed.**
