# 10 MORE backtestable strategy candidates (batch 2) — orthogonal-axis spec (2026-06-10)

**Author:** Claude (claude-fable-5). **Status:** design spec, NOT yet implemented or tested.
**Companion to:** `results/strategy_candidates_2026-06-10.md` (batch 1, candidates #1–#10).
**Numbering continues: #11–#20.** Both batches run under the SAME 5-gate acceptance test.

## Scope difference from batch 1

Batch 1 exhausted the **Binance-futures-derivable** orthogonal axes: open interest, L/S ratios,
taker flow, basis, liquidations, funding carry. Batch 2 deliberately goes where batch 1 could not:

- **Clock/event structure** (funding settlement times, macro releases, listing events, unlocks)
- **Cross-venue data** (Bybit/OKX funding, Coinbase spot)
- **Options market** (Deribit implied vol)
- **On-chain / flow data** (stablecoin supply, realized cap, ETF flows)

None of these are functions of the Binance OHLCV series, so the PR≈1.9 collapse
(`results/overfit_expansion_2026-06-09.md`) does not apply a priori — each must still PROVE
low correlation via gate 3.

### The 5 gates (unchanged — apply to every candidate)
1. 3-window walk-forward positive
2. Raises family DSR when added to the overfit matrix (`backtest_overfit_analysis.py`)
3. Correlation-to-LIVE < 0.5 (`candidate_correlation.py`)
4. Per-year decomposition not concentrated in one era. **One nuance for event/macro candidates:**
   crypto-macro coupling structurally began ~2022; a candidate dead 2020-21 but alive 2022-26 is a
   *structural break*, not automatically a mirage — but then 2022-26 must itself span ≥2 sub-regimes
   (2022 bear, 2023-24 chop/bull, 2025-26). Document the call explicitly either way.
5. Realistic costs: fee=10bp + slip=5bp minimum; alt shorts add borrow/funding; any hold < 4h must
   first pass the Option-C fee-death screen (expected gross move per trade vs round-trip cost).

**Power floor:** any candidate with < ~60 independent signals in the sample window cannot clear the
gates honestly. Flagged per-candidate below.

---

## DATA AXIS MAP (batch 2)

| Axis | Have it? | Source | Cost | Backtestable? |
|---|---|---|---|---|
| Funding settlement clock | ✅ implicit | funding CSVs + 1m klines (local) | free | yes — NOW |
| Listing dates | ✅ implicit | first kline per symbol in archive | free | yes — NOW (survivorship caveat) |
| Bybit/OKX funding history | ❌ | Bybit + OKX public REST (full history) | free | yes |
| Deribit DVOL (BTC/ETH implied vol) | ❌ | Deribit public API, history from 2021-03 | free | yes (5y) |
| Macro event timestamps (CPI/FOMC/NFP) | ❌ | BLS/Fed published calendars (deterministic) | free | yes |
| Stablecoin aggregate supply | ❌ | DefiLlama stablecoins API, daily to 2020 | free | yes |
| BTC/ETH realized cap → MVRV | ❌ | Coin Metrics Community CSVs | free | yes (low n) |
| Coinbase spot OHLCV | ❌ | Coinbase Exchange public candles API | free | yes |
| US spot ETF daily flows | ❌ | Farside / SoSoValue (scrape or API) | free | yes (2024+ only) |
| Token unlock schedules | ❌ | DefiLlama emissions/unlocks API | free | yes (alts subset) |

---

## THE 10 CANDIDATES (#11–#20, ranked: orthogonality × plausibility × feasibility)

### Tier A — data in hand or one cheap fetch; decent n; documented effects

#### 11. Funding-settlement window drift (microstructure clock) ⭐ CHEAPEST — DATA IN HAND
- **Thesis:** Funding settles at 00/08/16 UTC. When funding is extremely positive, longs pay at the
  timestamp → systematic pre-settlement positioning (longs trim before, re-add after) creates a
  predictable dip-into / recover-after pattern around settlement. Mirror for extreme negative.
  Documented in perp-microstructure literature.
- **Signal:** |next-period funding| > Xth percentile → enter mean-reversion 1–2h before settlement,
  exit 1–2h after. Direction: against the side that pays.
- **Data:** ✅ all local (funding CSVs + 1m klines). Zero fetch.
- **n:** 3 settlements/day × 5y × 57 sym, filtered to extremes → thousands. Excellent power.
- **Implement:** standalone event-study script first (NOT an engine mode) — align 1m returns around
  settlement timestamps, bucket by funding percentile. If the event-study shows structure, then an
  engine mode.
- **Falsification risk:** hold time 2–4h → fee-death screen MANDATORY before any engine build.
  Expected move must clear ~15bp round-trip with slip. This is the #1 way it dies.

#### 12. New-listing drift (short the hype unload) ⭐ DATA IN HAND
- **Thesis:** New Binance perp listings systematically underperform their first weeks (airdrop
  farmers + hype unload + market-maker unwind). Documented across multiple studies; the effect is
  structural (supply unlock at listing), not price-derived.
- **Signal:** short each new perp at close of listing day N (e.g., day 2, after initial chaos),
  hold M days, exit. Grid over N×M is small — pre-register it.
- **Data:** listing date = first kline in the archive. **Survivorship caveat: our 57 CSVs are
  survivors.** MUST fetch the full perp symbol list from data.binance.vision (including delisted)
  and download first-N-days klines for every symbol ever listed. The archive keeps delisted
  symbols — this is doable and the de-survivorship is the whole point.
- **n:** ~200–300 listings 2020–2026. Good power.
- **Falsification risk:** early-listing funding is often deeply negative (shorts PAY heavily) +
  slippage at listing is brutal. Funding accrual + elevated slip (≥25bp) mandatory in the harness.
  The edge must survive the carry bleed.

#### 13. Cross-venue funding-spread carry (Binance vs Bybit/OKX) ⭐ TRUE ARB CLASS
- **Thesis:** The same perp on different venues settles different funding. Long the perp on the
  low-funding venue, short on the high-funding venue → price risk fully cancels, collect the
  spread. This is an arbitrage-class return stream — structurally uncorrelated with EVERYTHING
  in the project.
- **Signal:** rank |funding_binance − funding_other| per symbol; hold the spread position while
  spread persists above cost threshold.
- **Data:** Bybit + OKX public APIs serve full funding history free. One fetch script.
- **Implement:** pure CSV arithmetic — no engine needed. Backtest = Σ(spread collected) −
  (4 legs of fees + rebalance). A standalone script answers it.
- **n:** continuous daily across symbols. Excellent power.
- **Falsification risk:** spreads on majors are heavily arbed post-2022; capacity thin; 4-leg cost
  is the killer. ALSO: we cannot execute on Bybit/OKX today (Binance-only engine) — the backtest
  answers "does the edge exist"; venue build only if it survives. Honest framing: this is the
  highest-probability-of-positive but lowest-ceiling candidate.

#### 14. Macro-event window playbook (CPI / FOMC / NFP) ⭐
- **Thesis:** Since 2022, BTC trades as a macro asset. Documented TradFi effects (post-FOMC drift,
  CPI-surprise momentum) transferred to crypto. Event timestamps are deterministic and published —
  a genuinely exogenous clock the price series cannot see.
- **Signal:** two pre-registered variants ONLY (no grid): (a) follow the 30m post-event direction
  for 24h; (b) fade the first 15m spike. Pick per event type from the event study, then freeze.
- **Data:** BLS CPI schedule + FOMC calendar + NFP dates (all published, free). 1m klines local.
- **n:** ~12 CPI + 8 FOMC + 12 NFP per year × 5y ≈ 160 events. Adequate.
- **Falsification risk:** macro coupling starts 2022 (see gate-4 nuance above). 2020-21 will show
  nothing — that's expected, not disqualifying, but 2022-26 must span sub-regimes.

#### 15. Volatility-risk-premium regime (Deribit DVOL vs realized) ⭐
- **Thesis:** VRP = implied vol (DVOL) − realized vol. High VRP = market overpaying for protection
  = fear extreme → contrarian long bias profitable. Negative VRP (RV > IV) = complacency/shock
  regime → flat or short bias. Options-market information, absent from any price series we hold.
- **Signal:** daily VRP z-score; (a) standalone: long when VRP > +X, flat/short when < −X;
  (b) regime gate on the live config (does it improve live Sharpe? — the one gate-type that could
  HELP the deployed strategy).
- **Data:** Deribit public API, DVOL history from 2021-03 (BTC + ETH). One fetch script.
- **n:** daily continuous, ~1900 days. Good.
- **Falsification risk:** confluence-filter history is bad (−23/−59% on price filters; regime-gate
  F=0/9) — but those were all PRICE-derived gates. This is the first options-derived gate. Still:
  prior says gates disappoint.

### Tier B — one fetch, good effect documentation, moderate confidence

#### 16. Coinbase–Binance premium (US institutional demand flow)
- **Thesis:** Coinbase spot premium over Binance = US demand pressure (institutions/retail on the
  regulated venue). Positive premium leads returns at 1h–1d horizon; popularized by CryptoQuant,
  measurable from free public data. Cross-venue flow information, not in the Binance series.
- **Signal:** premium z-score (rolling) → directional tilt; or gate the live short config (only
  short when premium negative — US not bidding).
- **Data:** Coinbase Exchange public candles API (full history, free) + Binance spot archive.
- **n:** continuous hourly/daily. Good.
- **Falsification risk:** premium is mostly noise inside fee width on majors; the documented signal
  strength is modest. Also partially correlated with #19 (ETF flows) and #16-batch-1 — gate 3
  applies WITHIN batch 2 as well: flow-family candidates must show <0.5 corr to each other or
  count as one bet.

#### 17. Stablecoin supply impulse (aggregate liquidity tide)
- **Thesis:** USDT+USDC aggregate supply growth = new dry powder entering crypto; contraction =
  liquidity drain. Supply growth leads returns at weekly–monthly horizon (documented in issuance
  studies; the 2022 contraction + 2023-24 expansion both led price).
- **Signal:** 30d supply-growth z-score → long bias above +X, flat/short below −X. Weekly rebalance.
  Best framed as a REGIME GATE (tide in/out) rather than standalone alpha.
- **Data:** DefiLlama stablecoins API, daily history to 2020, free.
- **n:** ~310 weekly observations, but effective independent bets far fewer (signal is slow).
  **Power flag: marginal.** Honest framing: supporting-evidence gate, not standalone edge.
- **Falsification risk:** three regime flips in 5y ≈ 3 independent bets. Cannot clear gate 1 alone;
  test ONLY as a gate on other candidates / live config.

#### 18. US spot-ETF flow momentum (2024+)
- **Thesis:** Daily BTC/ETH spot-ETF net flows are the dominant marginal-buyer datapoint post-2024.
  Large inflow days → continued strength next 1–5 days (flow persistence + underreaction).
  Documented since launch; genuinely new flow data unavailable pre-2024.
- **Signal:** 5d rolling net-flow z-score → directional tilt on BTC/ETH only.
- **Data:** Farside Investors table (scrape) or SoSoValue API, daily since 2024-01. Free.
- **n:** ~580 trading days, BTC+ETH. **History flag: 2.4y only — walk-forward shrinks to 3×9mo
  windows, all within one structural regime.** Pre-register that limitation; a pass here is
  weaker evidence than a pass on 5y candidates.
- **Falsification risk:** short history + flows are themselves price-chasing (reverse causality
  risk). The lead-lag direction must be established in the event study first.

### Tier C — real orthogonality, but n or data-quality limits honesty

#### 19. Token-unlock front-run (supply-event short)
- **Thesis:** Scheduled token unlocks > ~1% of float → token underperforms into and through the
  event (documented across 2023-24 studies; sellers front-run the supply). Deterministic calendar,
  fully orthogonal to price.
- **Signal:** short perp N days before unlock ≥ X% of circulating supply, cover at event.
- **Data:** DefiLlama emissions/unlocks API (free). Maps to the alt subset of our universe with
  vesting schedules (not BTC/ETH/DOGE).
- **n:** few hundred unlock events across alts 2022-26. Adequate IF the mapping work succeeds.
- **Falsification risk:** alt-short carry bleed (funding + borrow) — the same thing flagged on
  every alt-short candidate; effect is now widely known (front-runners front-running front-runners);
  data-mapping effort is the real cost (~1-2 days).

#### 20. On-chain cost-basis regime (MVRV-z / SOPR, BTC+ETH)
- **Thesis:** MVRV (market cap / realized cap) extremes mark cycle tops/bottoms; SOPR < 1 =
  holders selling at a loss = capitulation. Cost-basis information structurally absent from
  exchange OHLCV.
- **Signal:** MVRV-z percentile bands as a slow regime tilt (long-bias low band, de-risk high band).
- **Data:** Coin Metrics Community CSVs (realized cap, free) for BTC/ETH.
- **n:** **~1 full cycle in 5y. Power flag: SEVERE — effectively 2-4 independent signals.** Cannot
  honestly clear gate 1 standalone. Include ONLY as a context gate, same treatment as #17.
- **Why it's still listed:** it is the most-cited on-chain signal with the longest pedigree; if the
  project ever extends the sample to 2017+, it becomes testable. Documented here so the negative/
  untestable verdict is on record.

---

## Recommended implementation ORDER (batch 2 alone)

1. **#11 settlement drift** — zero fetch, event-study script, same-day answer. Fee-death screen first.
2. **#12 new-listing drift** — needs the de-survivorship symbol fetch (half-day), then same harness
   pattern. The de-survivorship list also benefits future research permanently.
3. **#13 cross-venue funding spread** — one fetch script + pure CSV math. True-arb class.
4. **#14 macro events** — calendar is a static file; event study on local 1m data.
5. **#15 DVOL VRP** — one fetch; test BOTH standalone and as live-config gate.
6. **#16 Coinbase premium → #18 ETF flows** (flow family — test corr to each other).
7. **#19 unlocks** — after the DefiLlama mapping work.
8. **#17 stablecoin + #20 MVRV** — gate-context only, never standalone verdicts.

## Combined-batch note (20 candidates total)

Batch 1 #8 (cross-sectional carry) and batch 2 #13 (cross-venue funding spread) are BOTH
funding-carry family — run `candidate_correlation.py` between them if both survive. Same for the
flow family (#16/#17/#18 batch 2). The overfit gate (gate 2) must be run on the FULL 20-candidate
matrix at the end — 20 candidates ≈ the haircut of 20 trials ONLY if they're truly independent;
the per-family correlation checks tell us the real trial count.

**Honest prior, batch 2:** #13 most likely positive but tiny; #11/#12/#14 genuine maybes with
good n; #15/#16/#18 plausible regime/flow tilts; #17/#20 structurally underpowered (gates only);
#19 real effect, decaying. If 1-2 of the 20 survive all 5 gates, that's a SUCCESS.

## Cross-references
- Batch 1 spec: `results/strategy_candidates_2026-06-10.md`
- Why price-only is closed: `results/overfit_expansion_2026-06-09.md` (DSR→0, PR≈1.9)
- Gate tooling: `scripts/walk_forward.sh`, `scripts/backtest_overfit_analysis.py`,
  `scripts/candidate_correlation.py`
- Fee-death screen precedent: Purgatory −$40M (`project_purgatory_method_feedeath` memory,
  commit 3b9fd6f)
