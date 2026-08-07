# Candidate #16 — Coinbase-Binance premium — VERDICT: MARGINAL (strongest gate of the search; not deployable book-wide) (2026-06-10)

> **RESOLVED NO-GO 2026-08-07.** The BTC/ETH-only sub-book follow-up this
> verdict flags below was pre-registered and run:
> `results/btceth_regime_subbook_verdict_2026-08-07.md`. Combined with #15 the
> gate reaches 15.8 bp (BTC) / 20.2 bp (ETH) gross against a 45 bp requirement,
> and inverts on drop-top-5%. This verdict's numbers reproduce exactly
> (t=+2.39 / +2.29, 6/7 years) — the signal is real, the magnitude is not
> enough. **No further follow-up; do not re-open.**

**Type:** verdict (pre-registered; standalone reject, gate flagged as the strongest finding).
**Candidate:** #16 of the 20-candidate search (`results/strategy_candidates_batch2_2026-06-10.md`).
**Scripts:** `scripts/fetch_flow_data.py` (Coinbase candles) + `scripts/coinbase_premium_study.py`.
**Data:** Coinbase Exchange daily close BTC-USD/ETH-USD 2020-01→2026-06 (2353 days) + local Binance daily close. Premium = (CB − Binance)/Binance.

## The bet

Coinbase spot premium over Binance = US institutional/retail demand on the regulated venue. Positive premium documented to lead returns. Variant A standalone (premium-z directional), Variant B gate (does the live short time better when US is NOT bidding?).

## Result

**Variant A (standalone): DEAD.** BTC Sharpe 0.16, ETH 0.05; both median negative (−10 bp), tail-inverting (drop5 −41/−54). No standalone premium timing edge.

**Variant B (gate on short timing): the strongest signal in the entire 20-candidate search — and clean on BOTH majors:**

| sym | short-ret \| prem-z<0 (US not bidding) | short-ret \| prem-z>0 (US bidding) | Welch t | years split-positive |
|---|---|---|---|---|
| BTC | **+8.1 bp** | **−24.9 bp** | **+2.39** | **6/7** |
| ETH | **+12.2 bp** | **−30.0 bp** | **+2.29** | **6/7** |

The economic story is clean and consistent: **when US demand (Coinbase premium) is weak/negative, shorts work; when US is bidding hard, shorts get run over.** Significant on both BTC and ETH (t>2.3), robust 6/7 years each.

## Independence from #15 (the meaningful finding)

corr(premium-z, VRP-z) = **−0.12** over 1789 common days → #16 and #15 (DVOL VRP) are **largely independent signals**, not the same macro regime. So the search has surfaced **two distinct, exogenous, multi-year regime signals that both improve short-timing**: high fear (#15, VRP) and weak US demand (#16, premium). Independent confirmation strengthens both.

## Verdict: MARGINAL — strongest finding, still not deployable.

Same structural fate as #15, but stronger (both symbols significant vs #15's ETH-only):

1. **Standalone (variant A) is dead** — the directly-deployable form has no edge.
2. **The gate works only on BTC+ETH** — Coinbase doesn't list most deployed alts, so the premium can't be computed for the alt-heavy live book (BTC is 1 of 16 deployed). A gate measurable on 2 of 16 symbols is not a book-wide overlay.
3. The live forward-paper run must not be touched (project rule).

**Disposition:** NO-GO for deployment now. But this + #15 are the two real signals the search produced, and they are independent. Flagged for a future milestone: IF the project ever runs a **BTC/ETH-only short sub-book**, the combined (#15 fear-high AND #16 US-not-bidding) gate is the first thing to test on it — two independent t>2.3 regime filters with the correct economic sign and 6/7-year robustness. Not actionable against the current alt-heavy deployed book.

## Honest note

This is the closest the search has come to a deployable edge. It does not clear the bar because (a) the standalone form is dead and (b) the gate's data coverage (BTC/ETH) does not match the deployed universe. Recorded prominently so the BTC/ETH-only-subbook idea is on the record.

## Gates

Variant B passed by-year (gate 4) on both symbols + independence-from-#15 check. Walk-forward / overfit / full corr-to-LIVE not run — gate not applicable to the deployed alt book.

## Cross-references

- Spec: `results/strategy_candidates_batch2_2026-06-10.md` (#16)
- Independent sibling signal: `results/dvol_vrp_verdict_2026-06-10.md` (#15, corr −0.12)
- Flow family (corr check if any deploy): #16, #17 (stablecoin), #18 (ETF, data-blocked)
- Fetcher: `scripts/fetch_flow_data.py` (data/flow/, reusable)

---

# Candidate #17 — Stablecoin supply impulse — VERDICT: NO-GO (2026-06-10)

**Script:** `scripts/stablecoin_supply_study.py`. **Data:** DefiLlama aggregate stablecoin circulating supply (USD), daily 2020→2026 (3.2B → 189.5B), vs BTC daily close.

Bet: 30d supply growth (z-scored 180d) = liquidity tide; contraction → shorts win. Pre-registered as POWER-FLAGGED (≈3 independent regimes in 5y, gate-context only).

Result:
- Variant A standalone: Sharpe 0.34, median −5.2 bp — dead (as the power flag predicted).
- Variant B gate: short-ret | supply-z<0 (contracting) = −8.0 bp vs z>0 (expanding) = −10.7 bp, **Welch t=0.20, 3/7 years** — no clean liquidity-regime split.

**Verdict: NO-GO.** Stablecoin supply impulse carries no usable short-timing signal. The ~3-regime power floor was the killer exactly as pre-registered — there are too few independent liquidity regimes in the sample to resolve an edge. Confirms the spec's "supporting-context-only, likely nothing" prior.

# Candidate #18 — ETF flow momentum — DEFERRED (data-access blocked)

Farside (Cloudflare 403) and SoSoValue (endpoint moved/auth) both block free programmatic access to BTC/ETH spot-ETF daily flows. The spec already flagged #18 as the weakest flow candidate (2.4y history, single regime, "weaker evidence than 5y candidates"). Given #16 — the stronger, full-history flow signal — resolved to MARGINAL/not-deployable, #18 on shorter data could at best replicate that conclusion. Documented as deferred (revisit if a free source appears), not a gap in the search conclusion.
