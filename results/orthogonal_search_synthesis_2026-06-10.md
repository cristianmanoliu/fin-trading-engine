# 20-Candidate Orthogonal Strategy Search — SYNTHESIS (2026-06-10)

> **RESOLVED 2026-08-07 — the two MARGINAL signals are now NO-GO.** The
> follow-up this document recorded as the one actionable lead (§"Recommendation"
> item 2, the combined #15+#16 gate on a BTC/ETH-only short book) was
> pre-registered and executed: `results/btceth_regime_subbook_verdict_2026-08-07.md`.
> The gate is real — it opens on ~21% of days, both symbols positive and
> same-sign, and stacking the two roughly doubles the conditional edge — but it
> delivers **20 bp gross against a 45 bp requirement** and **inverts on
> drop-top-5%** (−20.9 BTC / −23.8 ETH). Final tally: **17 NO-GO, 3
> data-blocked, 0 survivors.**
>
> ⚠️ **SIGN ERROR BELOW.** §"The two real signals" and §"Recommendation" say
> *"short only when fear-high"*. That is **inverted**: `dvol_vrp_verdict`
> measures shorts at −16.5 bp under fear (VRP-z>0) and **+19.6 bp under
> complacency (VRP-z<0)**. The correct gate is `VRP-z < 0`. Do not quote those
> sentences without this correction.

**Status:** CLOSED. All 20 candidates resolved (18 tested, 2 data-blocked). Zero deployable survivors. Two independent marginal regime signals surfaced (BTC/ETH-only, not deployable to the alt-heavy live book) — **both resolved NO-GO 2026-08-07, see banner.**

**Specs:** `results/strategy_candidates_2026-06-10.md` (#1–#10), `results/strategy_candidates_batch2_2026-06-10.md` (#11–#20).
**Plan:** `tasks/todo.md`. **Per-candidate verdicts:** see ledger below.

---

## The question

After the price-only search collapsed (`results/overfit_expansion_2026-06-09.md`: DSR→0.001, participation ratio ≈1.9 ⇒ ~2 independent bets), the open question was: **does ANY orthogonal data axis — a signal the price backtest cannot see — carry a real, low-correlation, deployable edge?** 20 candidates were pre-registered across 10 data axes (OI, L/S ratios, taker flow, basis, liquidations, funding carry, settlement clock, listing events, cross-venue funding, options IV, macro calendar, on-chain flows, cost-basis).

## The ledger

| # | candidate | axis | verdict | why |
|---|---|---|---|---|
| 11 | settlement drift | clock | NO-GO | tail-mirage (median −1bp, top-1% = 81% PnL) |
| 8 | cross-sectional carry | carry | NO-GO | selection-corrected ~0.4 Sharpe |
| 12 | new-listing drift | listing event | NO-GO | de-survivorship + funding bleed collapsed it 10× |
| 4 | taker-flow imbalance | flow | NO-GO | gross 1-7bp vs 15bp cost; t=−94 on no-edge |
| 1 | OI divergence | OI | NO-GO | sorts volatility, not direction |
| 2 | L/S-ratio fade | positioning | NO-GO | t=−218 against the edge at n=2.6M |
| 13 | cross-venue funding spread | carry | NO-GO | dead-arbed (|spread| 0.96bp, 4-leg cost 30bp) |
| 3 | funding carry harvest | carry | NO-BUILD | family closed (#8 marginal + #13 dead) |
| 9 | positioning-stress composite | composite | PRECLUDED | pre-reg required ≥1 of #1/#2/#3; none survived |
| 14 | macro-event windows | macro clock | NO-GO | events mark vol not direction; FOMC flicker t=0.29 |
| 10 | vol-sizing overlay | sizing | NO-GO | live $-risk sizing already vol-aware; overlay hurts |
| 6 | OI-confirmed gate | OI | NO-GO | +0.087 Sharpe at 42% retention, below bar |
| 17 | stablecoin supply | on-chain flow | NO-GO | underpowered (~3 regimes), gate t=0.20 |
| 5 | spot-perp basis | basis | NO-GO | same-venue basis too small/arbed (t≈1) |
| 20 | MVRV cost-basis | on-chain | NO-GO | gate significant but WRONG sign (momentum dominates) |
| **15** | **DVOL VRP** | **options IV** | **MARGINAL** | **ETH VRP gate robust 5/6yr; BTC weak; BTC/ETH-only** |
| **16** | **Coinbase premium** | **cross-venue flow** | **MARGINAL** | **gate t=2.4/2.3 both majors, 6/7yr; BTC/ETH-only** |
| 18 | ETF flow momentum | flow | DATA-BLOCKED | Farside Cloudflare-403, SoSoValue moved |
| 19 | token unlocks | supply event | DATA-BLOCKED | DefiLlama emissions paywalled (402) |
| 7 | liquidation reversal | liquidations | UNTESTABLE | Binance liq archive removed; proxy redundant |

**Tally: 15 NO-GO/NO-BUILD/PRECLUDED · 2 MARGINAL · 3 data-blocked/untestable.**

## The cross-cutting finding

Across **every** orthogonal axis that could be tested directionally — OI, L/S ratios, taker flow, settlement clock, listing events, macro events, MVRV — the result is the same: **the axis identifies WHEN volatility/stress happens (gross forward moves of 60–330 bp) but carries NO fee-clearing information about WHICH WAY it resolves.** Every directional mapping lands at a coin-flip-or-worse win rate with a negative median net. This is the non-price analogue of the PR≈1.9 price-only collapse: a different data source, the same verdict — *activity is observable, tradeable direction is not.*

The honesty discipline that made this trustworthy: **median + win-rate + drop-top-5% + by-year**, not just mean and t. Three candidates (#11, #12, #20) produced significant means or by-year positivity that the median/tail/sign checks then killed as tail-mirages or wrong-sign artifacts. By-year positivity alone is NOT sufficient.

## The two real signals (independent, exogenous, but not deployable)

The search did surface something genuine — **two independent regime signals that improve SHORT-timing**, both exogenous to the price series:

- **#15 DVOL VRP** — high implied-vs-realized vol (fear) precedes shorts doing worse; ETH gate t=2.04, robust 5/6 years.
- **#16 Coinbase premium** — weak US demand (negative premium) precedes shorts doing better; BTC t=2.39 + ETH t=2.29, robust 6/7 years each (the strongest single result).

**They are independent** (corr(VRP-z, premium-z) = −0.12), so they are two distinct bets, not one. Both have the economically correct sign and multi-year robustness.

**Why neither is deployable now:**
1. Both work only as a **gate** on short-timing — the standalone directional forms are dead.
2. Both data sources cover **only BTC + ETH** (Deribit DVOL; Coinbase majors). The deployed live book is alt-heavy (BTC is 1 of 16 symbols), so a BTC/ETH-only gate cannot time the actual book.
3. The forward-paper run must not be perturbed (project rule).

## Recommendation

1. **Do not deploy anything from this search.** No candidate clears the bar for the deployed universe. The live EMA-9/21 short stays as-is; its sizing is already optimal (#10 confirmed).
2. **Record #15 + #16 as the one actionable lead** for a *future* milestone: if a BTC/ETH-only short sub-book is ever run, test the combined gate (short only when fear-high AND US-not-bidding) on it. Two independent t>2.3 exogenous filters with correct sign and 6/7-year robustness is the best non-price evidence the project has produced.
3. **The strategy-class search is now closed** across price AND every accessible orthogonal axis. The conclusion from `docs/RESEARCH_BACKLOG.md` is reinforced and extended: operate the deployed strategy and let forward-paper resolve; do not keep mining for a new signal class — the accessible axes are exhausted.

## Reusable assets built

Fetchers (all idempotent, gitignored data): `fetch_listing_data.sh` (de-survivorship perps), `download_metrics.py` (OI/L-S/taker, paginated+parallel), `fetch_crossvenue_funding.py` (Bybit/OKX), `fetch_dvol.py` (Deribit), `fetch_flow_data.py` (Coinbase+stablecoin), Binance spot + Coin Metrics fetches. Study harnesses with pre-registered headers + the median/tail/by-year honesty block for all 18 tested candidates. `gen_live_journals.sh` regenerates the deployed-book trade stream for overlay tests.

## Cross-references

- Phase-0 lesson (memory): `project_phase0_tail_mirage_lesson`
- Per-candidate verdicts: `results/{settlement_drift,cross_sectional_carry,taker_flow,oi_lsratio,crossvenue_spread,macro_event,dvol_vrp,coinbase_premium,live_overlay,listing_drift,final_four}_verdict_2026-06-10.md`
- Prior closure: `docs/RESEARCH_BACKLOG.md`, `results/overfit_expansion_2026-06-09.md`
