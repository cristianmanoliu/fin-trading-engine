# Candidates #5 (basis), #20 (MVRV), #19 (unlocks), #7 (liquidations) — closing the 20 (2026-06-10)

**Type:** verdict — the last 4 candidates. #5 + #20 tested (NO-GO); #19 + #7 data-access-blocked / untestable (documented).
**Candidates:** #5, #20, #19, #7 of the 20-candidate search.
**Scripts:** `scripts/basis_study.py` (#5), `scripts/mvrv_regime_study.py` (#20).

## #5 — Spot-perp basis dislocation — NO-GO

Bet: perp − spot basis; rich perp (longs crowded) → fade down, cheap perp → fade up. **Must be SAME-VENUE spot.** Data: Binance perp (local) vs Binance spot (`data/spot/`, fetched).

**Critical methodology note:** an initial version used Coinbase spot as the spot leg and produced a "significant" gate (BTC t=2.50, ETH t=2.32) — but those numbers are *identical* to #16's Coinbase-Binance premium gate. The Coinbase-vs-Binance cross-venue price gap dominates that difference, so it was just #16 in disguise, not a perp-spot basis. Re-run with **Binance spot** (same venue), isolating the true basis:

| sym | A standalone Sharpe | B gate Welch t |
|---|---|---|
| BTC | 0.21 | **+1.18** (weak) |
| ETH | −1.03 | **+0.89** (weak) |

**NO-GO.** Standalone dead; the same-venue basis gate is insignificant (t≈1). The perp-spot basis on a single venue is too small/arbed to time shorts. (The lesson: a cross-venue "basis" is the cross-venue premium #16, not a funding/basis signal — keep the spot leg same-venue.)

## #20 — On-chain cost-basis regime (MVRV) — NO-GO

Bet: MVRV (market cap / realized cap) extremes mark tops/bottoms; overvalued → short bias. Data: Coin Metrics community CSVs (`data/onchain/cm_{btc,eth}.csv`), **full history BTC 2010→, ETH 2015→** — much better powered than the spec's "~1 cycle in 5y" worry (≈3-4 cycles). Trailing-2y percentile bands.

| sym | A band-tilt Sharpe | B gate Welch t (shorts in overvalued band) |
|---|---|---|
| BTC | −1.92 | **−3.36** |
| ETH | −1.55 | **−2.00** |

**NO-GO.** Standalone deeply negative. The gate IS significant — but the **WRONG SIGN**: shorts *lose* in the overvalued (high-MVRV) band (BTC −45.9 bp vs +6.6 bp low band). That means high MVRV / euphoria keeps ripping up — momentum dominates the cost-basis fade at tradeable horizons. A gate that says "short more when undervalued" is economically perverse and unusable. MVRV gives no tradeable short-timing edge. (A loose |t|>2 check initially mislabeled this; the sign requirement is what makes the verdict correct.)

## #19 — Token-unlock front-run — UNTESTABLE (data-access blocked)

DefiLlama emissions/unlocks API is now **paywalled** (HTTP 402, "upgrade to the paid API plan"). Free programmatic access to the unlock calendar is gone. The spec already flagged #19 as a decaying effect (front-runners front-running front-runners) with the data-mapping as the real cost, and alt-short carry bleed as the killer (same bleed that helped sink #12). No free source found → documented as untestable in this pass. Revisit only if an unlock-calendar source opens up; given the carry-bleed precedent, the prior is low.

## #7 — Liquidation-cascade reversal — UNTESTABLE (archive empty)

The `data.binance.vision/.../liquidationSnapshot/` archive returns **0 keys** for BTC — Binance discontinued/removed the historical liquidation snapshot feed. The spec pre-flagged this as "sparse → proxy from 1m wick+volume spikes or document untestable." Given that #11 (settlement-window) and #2 (L/S extremes) already showed that volatility/stress markers carry no tradeable direction, a wick+volume liquidation *proxy* would near-certainly reproduce that result (it is another "WHEN volatility happens" marker). Documented as untestable with the data gone; the proxy is not built because the conclusion is already established by #1/#2/#11.

## Combined verdict

- **#5 basis: NO-GO** — same-venue perp-spot basis too small/arbed (t≈1).
- **#20 MVRV: NO-GO** — significant but wrong-sign; momentum dominates the cost-basis fade.
- **#19 unlocks: UNTESTABLE** — DefiLlama emissions paywalled.
- **#7 liquidations: UNTESTABLE** — Binance liquidation archive removed; proxy redundant with #1/#2/#11.

This closes the 20-candidate search. See the synthesis doc for the full ledger and conclusion.

## Cross-references

- Specs: `results/strategy_candidates_2026-06-10.md` (#5, #7), `..._batch2_2026-06-10.md` (#19, #20)
- The #16 leakage lesson: `results/coinbase_premium_verdict_2026-06-10.md`
- Stress-not-direction precedent: `results/oi_lsratio_verdict_2026-06-10.md` (#1/#2), `results/settlement_drift_verdict_2026-06-10.md` (#11)
