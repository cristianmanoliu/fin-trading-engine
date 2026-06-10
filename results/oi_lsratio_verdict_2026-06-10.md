# Candidates #1 (OI divergence) + #2 (L/S-ratio fade) — VERDICT: both NO-GO (2026-06-10)

**Type:** verdict (pre-registered designs applied to data → mechanical reject). Combined doc — both die of the same root cause.
**Candidates:** #1, #2 of the 20-candidate search (`results/strategy_candidates_2026-06-10.md`).
**Scripts:** `scripts/oi_divergence_study.py`, `scripts/ls_ratio_study.py`.
**Data:** `data/metrics/<SYM>.csv` for the deployed-20 — OI + L/S ratios + taker ratio, full history 2020/21→2026-06 (fetched via `scripts/download_metrics.py`, ~475k 5-min rows/sym). + local 1m klines for forward price.

## Data note (fetcher rebuild)

The first fetcher (`scripts/download_metrics.sh`) silently truncated several symbols at the S3 1000-key first page (143,994-row = 500-day stubs, all ending 2023-04). Rebuilt as `scripts/download_metrics.py` with correct marker pagination + per-symbol day-level threadpool (24 concurrent; 30× faster — 56s/sym vs ~28min). All 20 symbols verified reaching 2026-06-10 before these studies ran. The `.sh` version is superseded.

## #1 — OI / price divergence

Bet: sign(Δprice_4H) × sign(ΔOI_4H) maps to a continuation/reversal bias (price↑OI↑ = new longs → long; price↑OI↓ = short-cover → fade; etc.). 4 combos × {4h,12h,24h} forward.

Result: **every combo "PASSES" the gross screen** (gross moves 130–334 bp — the combos DO sort volatile bars) but the **directional bias is noise-to-wrong**: mean net negative in 11/12 cells, median net negative in all 12, win-rate 42–50%, drop-top-5% deeply negative everywhere (−36 to −94 bp). Best cell (P-OI+ contShort, H24) mean +3.1 bp but median −6.8 bp, t=1.3 → tail-mirage even there.

## #2 — L/S-ratio extreme fade

Bet A (crowd fade): extreme global account L/S ratio → fade the crowd. Bet B (divergence): top-trader vs global L/S z-score → follow smart money. Both {1h,4h,24h}, n ≈ 2.5–2.6M signals.

Result: **both NO-GO.** Gross moves clear the screen (62–320 bp) but mean net is negative across all horizons, median negative, win 38–48%, drop-top-5% negative, t as low as −218 on the no-edge (at n=2.6M the negative is overwhelmingly significant — it reliably LOSES, i.e. extremes do not fade in a tradeable direction net of cost).

## Shared root cause (the durable finding)

Both OI and L/S-ratio extremes **identify WHEN the market is stressed/volatile** (gross forward moves are large — 130–334 bp) but **carry no information about WHICH WAY the move resolves** net of fees. The directional edge that would be needed to monetize the volatility is simply absent: every directional mapping (continuation OR fade OR divergence) lands at a coin-flip-or-worse win rate with a negative median. This is the positioning-data analogue of the price-only PR≈1.9 collapse — a different data axis, same verdict: the axis sees activity, not direction.

This also lowers the prior on **#6 (OI-confirmed breakout)** and **#9 (positioning-stress composite)**, which reuse these same series as gates/components. #9 in particular was gated on "≥1 of #1/#2/#3 surviving alone" — none did, so **#9 is now precluded** by its own pre-registration. #6 (OI gate on the EMA cross) remains testable as a confluence FILTER (different question — does OI confirmation improve the LIVE entry, not standalone direction), kept in Phase 3 but evidence-downgraded.

## Verdicts

- **#1 OI divergence: NO-GO** — sorts volatility, not direction.
- **#2 L/S-ratio fade (A and B): NO-GO** — extremes don't fade tradeably; t=−218 against the edge at n=2.6M.
- **#9 positioning composite: PRECLUDED** — its pre-reg required ≥1 of #1/#2/#3 to survive standalone; none did.

## Gates not reached

Walk-forward / overfit / corr-to-LIVE not run — none cleared the screen with a positive-median directional edge.

## Cross-references

- Spec: `results/strategy_candidates_2026-06-10.md` (#1, #2, #6, #9)
- Honesty lens: `results/settlement_drift_verdict_2026-06-10.md` (#11)
- Metrics fetcher: `scripts/download_metrics.py` (replaces .sh; data/metrics/)
