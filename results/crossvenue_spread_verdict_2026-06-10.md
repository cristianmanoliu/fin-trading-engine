# Candidate #13 — Cross-venue funding-spread carry (Binance vs Bybit/OKX) — VERDICT: NO-GO, dead-arbed (2026-06-10)

**Type:** verdict (pre-registered design applied to data → mechanical reject).
**Candidate:** #13 of the 20-candidate orthogonal search (`results/strategy_candidates_batch2_2026-06-10.md`).
**Scripts:** `scripts/fetch_crossvenue_funding.py` (Bybit+OKX funding) + `scripts/crossvenue_spread_study.py`.
**Data:** Binance funding (local) vs Bybit (full history, ~5–7k settlements/sym) and OKX (~3mo only — API limit) for the deployed-20 set.

## The bet

The same perp settles different funding on different venues. Long the low-funding venue + short the high-funding venue → price risk cancels (delta-neutral across venues), collect the funding spread each settlement. Arbitrage-class, structurally uncorrelated with everything else in the project. The spec called it "highest-probability-of-positive, lowest ceiling."

## Result

| pair | best thresh | n settlements | mean bp/settle | ann.Sharpe | pos years |
|---|---|---|---|---|---|
| Binance–Bybit | 5 bp | 3,384 | **−4.10** | **−10.4** | 4/7 |
| Binance–Bybit | 0 bp | 78,165 | −9.45 | −24.7 | 0/7 |
| Binance–OKX | 0 bp | 3,295 | −11.6 | −28.7 | 0/1 |

Negative at every threshold, every venue pair.

## Why (verified — economic, not a bug)

The Binance↔Bybit funding spread on the deployed majors is **dead-arbed**:

- **|spread| mean 0.96 bp, median 0.34 bp** (BTC, 6,697 aligned settlements). The two venues' funding is essentially identical.
- **Sign flips 24.6%** of settlements — the spread is noise around zero, so the favored direction flips ~every 4 settlements.
- A direction flip = 4 taker legs = **30 bp** round-trip cost (7.5 bp/leg). You collect ~1 bp/settlement and pay ~30 bp every fourth one. Cost swamps the spread by ~10×.
- Even filtering to |spread| > 5 bp (only 3.8% of settlements) collects ~6–11 bp but still loses to the flip cost.

This is exactly the spec's pre-registered falsification risk: *"spreads on majors are heavily arbed post-2022; capacity thin; 4-leg cost is the killer."* Confirmed. There is no harvestable spread left between two major retail-accessible venues after fees.

OKX is additionally underpowered (public funding-history API serves only ~3 months back to 2026-03) and not separately informative.

## Verdict: NO-GO — dead-arbed. No venue build.

The cross-venue funding spread does not exist net of retail fees on the deployed universe. We also cannot execute on Bybit/OKX today (Binance-only engine); this backtest answered "does the edge exist" — it does not — so no venue-integration build is warranted.

## Carry family CLOSED (the open question from #8)

The carry family was #3 (delta-neutral funding harvest), #8 (cross-sectional carry), #13 (this). With:
- **#8 MARGINAL** — selection-corrected ~0.4 Sharpe at 35 bp, not worth a two-leg harness standalone (`results/cross_sectional_carry_verdict_2026-06-10.md`).
- **#13 NO-GO** — dead-arbed.

…neither standalone carry bet clears. The two-leg delta-neutral harness build (#3) is **not justified** — it would be built to harvest a spread (#13: gone) or a cross-sectional carry premium (#8: ~0.4 Sharpe, sub-threshold). **Carry family yields no deployable edge.** #3 is downgraded to NO-BUILD unless a future data axis revives the premium.

## Gates not reached

Walk-forward / overfit / corr-to-LIVE not run — a uniformly negative-Sharpe result is rejected at the cost model.

## Cross-references

- Spec: `results/strategy_candidates_batch2_2026-06-10.md` (#13)
- Carry sibling: `results/cross_sectional_carry_verdict_2026-06-10.md` (#8)
- Fetchers: `scripts/fetch_crossvenue_funding.py` (data/xvenue/), reusable
