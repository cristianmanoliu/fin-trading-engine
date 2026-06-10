# #24 Vol-event two-sided breakout — VERDICT: NO-GO (0/3 gate families). (2026-06-10)

**Pre-reg:** `results/strategy_candidates_batch3_2026-06-10.md` (#24), locked bar.
**Script:** `scripts/vol_event_breakout_study.py`. **Cells CSV:**
`results/vol_event_breakout_cells_2026-06-10.csv`.

## Setup

The direction-free expression of the search's one robust finding (gates mark WHEN
moves happen). Brackets ±0.75×ATR(4H,14) armed 4h at gate-fire; fill at bracket; stop =
opposite bracket; +2R or 24h; 15bp on filled leg; 1m closes, deployed-16. Gate families:
macro (CPI/FOMC/NFP), OI 1h-z spike |z|>2 (2021-12+), settlement EXTREME_LOW (#11 gate,
armed −2h).

## Results

| gate | fills | mean bp | median bp | t | drop-top-5% bp | whipsaw% | yrs+ |
|---|---:|---:|---:|---:|---:|---:|---|
| macro | 1,686 | +17.0 | **−31.3** | 1.38 | −48.6 | 29.2 | 3/7 |
| oi_z | 9,340 | +10.6 | **−62.0** | 1.98 | −57.2 | 36.0 | 5/6 |
| settlement | 1,039 | +45.3 | **−48.1** | 1.80 | −60.9 | 31.9 | 6/7 |

**Locked bar (mean AND median > 0, drop5 > 0, in ≥2/3 families): 0/3 pass.**

## Conclusion

The cleanest possible confirmation of the cross-cutting finding, now from the OPPOSITE
direction: the vol-timing information is real (all three means positive; settlement
+45bp/event, 6/7 years), but the two-sided harvest is a pure lottery ticket — the median
filled event LOSES 31–62bp (bracket spread + 29–36% whipsaw), and removing the top 5% of
events flips every family deeply negative. This is the same tail-mirage profile that
killed #11/#12/#20, here by construction: a breakout straddle is long-vol option
replication paid for in whipsaw, and the premium exceeds the realized tail income at
median discipline. Direction-free does NOT rescue the WHEN-not-WHICH-WAY signal.
**NO-GO. The vol-timing expression space (directional gates AND direction-free
brackets) is now closed.**
