# Candidate #12 — New-listing drift (short the hype unload) — VERDICT: TAIL-MIRAGE / NO-GO (2026-06-10)

**Type:** verdict (pre-registered design applied to data → mechanical reject).
**Candidate:** #12 of the 20-candidate orthogonal search (`results/strategy_candidates_batch2_2026-06-10.md`).
**Scripts:** `scripts/fetch_listing_data.sh` (de-survivorship fetch) + `scripts/listing_drift_study.py` (harness).
**Data:** de-survivorship — first ~2 months of daily klines + funding for **732 USDT perps ever listed** (incl. delisted), fetched from data.binance.vision. The 57 local survivors were NOT enough.

## The bet

New Binance perp listings underperform their first weeks (airdrop farmers + hype unload + MM unwind). SHORT at close of listing-day N, hold M days, exit at close. Net = price drop − 25 bp slip − funding bleed.

## Why de-survivorship was mandatory (and what it did)

The 57 local CSVs are survivors. A survivor-only probe (`tasks/listing_drift_probe.py`, gross-of-funding) looked excellent:

| | survivor-only probe (57) | full de-survivorship (732, funding-accrued) |
|---|---|---|
| N1/M7 mean | **+7.74%** | **+0.73%** |
| N1/M7 median | +9.14% | +7.20% |
| N1/M7 win | 74% | 63% |
| N1/M7 t | **+2.78** | **+0.52** |

The fetch of 702 additional (mostly delisted) symbols + the funding model collapsed the apparent edge by ~10×. Two mechanisms:

1. **Funding bleed.** Early-listing perps run deeply negative funding, and a SHORT PAYS negative funding (1000LUNC day-1 was −21 bp/8h ≈ −64 bp/day). Accrued from real settled rates, this ate most of the survivor +7.7%.
2. **Survivorship was hiding the loss tail.** The full universe includes the violent short-SQUEEZE listings (pump-and-delist names). The median stays +7.2% (most listings DO drift down) but the **mean collapses to +0.73%** because a handful of catastrophic-for-shorts pumps dominate the average.

## Result (full universe, all 9 N×M cells)

Every cell: median positive (+2.3% to +7.2%), win-rate 58–63% — a real downward tendency — BUT mean net ≈ 0 (−0.4% to +0.7%, all |t| < 0.55) and **drop-top-5% is negative in every cell** (the mean inverts when the best 5% of shorts are removed). Best cell N1/M7: pooled mean +0.73%, t=0.52, drop-top-5% **−1.95%**. By-year median is positive every year 2020–2026, but mean is insignificant and 2/7 years negative.

## Verdict: TAIL-MIRAGE — NO-GO. No engine mode.

There is a genuine central tendency for new listings to drift down (median +7% at N1/M7, win 63%), but it is **un-tradeable as a short**: the mean net is statistically zero (t=0.52) after funding, and the edge inverts when the top 5% of trades are dropped — i.e. the positive average depends on a few violent dumps while the fat LOSS tail (getting squeezed on a pump-listing) is large enough that you cannot size the position. This is the same tail-mirage structure that killed #11 (settlement drift), mirror-imaged: there the edge was a few violent up-snaps; here the risk is a few violent up-squeezes against the short.

The honesty checks (median + win-rate + drop-top-5% + by-year) are what separate this from the survivor mirage. The harness emits the verdict mechanically.

## What would change the verdict (not pursued now)

- A **size/stop overlay** capping the squeeze tail could in principle salvage the positive median, but that is a sizing-strategy build (cf. #10), not a clean signal, and the funding bleed remains. Not justified on a t=0.52 base.
- The **per-symbol funding interval varies** (newer perps settle 4×/day, modeled via real `calc_time`); the bleed is structural, not a modeling artifact.

## Gates not reached

Walk-forward / overfit / corr-to-LIVE not run — a mean-net t=0.52 signal whose edge inverts on top-5% drop does not merit consuming the degrees of freedom.

## Cross-references

- Spec: `results/strategy_candidates_batch2_2026-06-10.md` (#12)
- Survivor probe (the mirage): `tasks/listing_drift_probe.py`
- Honesty lens precedent: `results/settlement_drift_verdict_2026-06-10.md` (#11)
- De-survivorship fetch (reusable for future research): `scripts/fetch_listing_data.sh` → `data/listing/`
