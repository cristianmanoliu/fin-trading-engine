# Executing plan: 20 orthogonal strategy candidates — backtest for a real edge

**Specs:** `results/strategy_candidates_2026-06-10.md` (#1–#10) + `results/strategy_candidates_batch2_2026-06-10.md` (#11–#20).
**Goal:** find ≥1 candidate that clears ALL 5 acceptance gates. 1–2 survivors out of 20 = success.
**Created:** 2026-06-10. Live EMA 9/21 config untouched throughout — research only.

## The 5 gates (every candidate, no exceptions)

1. 3-window walk-forward positive (`scripts/walk_forward.sh` discipline)
2. Raises family DSR in the overfit matrix (`scripts/backtest_overfit_analysis.py`)
3. Correlation-to-LIVE < 0.5 (`scripts/candidate_correlation.py`)
4. Per-year decomposition not regime-concentrated (macro/event nuance: post-2022 structural break OK if 2022-26 spans sub-regimes — document the call)
5. Realistic costs: fee=10bp + slip=5bp min; alt shorts add carry bleed; hold <4h → Option-C fee-death screen FIRST

**Store every result, including no-gos.** Negative results are results.

## Phase 0 — zero-fetch candidates (data already local)

- [x] **#11 Funding-settlement window drift** — DONE 2026-06-10. NO-GO (TAIL-MIRAGE). `scripts/settlement_drift_event_study.py`; verdict `results/settlement_drift_verdict_2026-06-10.md`. Screen passed (EXTREME_LOW/pre 39.81bp) + positive all 7 years, but median net −1.03bp, top-1% of trades = 80.9% of PnL, edge inverts dropping top 5%. Coin-flip + lottery ticket; un-tradeable. No engine mode.
- [x] **#8 Cross-sectional carry** — DONE 2026-06-10. MARGINAL → NO-GO standalone. `scripts/cross_sectional_carry.py`; verdict `results/cross_sectional_carry_verdict_2026-06-10.md`. Best cell Sharpe 0.79 (5/7 yrs positive, NOT pre-2022 mirage — better than momentum), but selection-corrected across 9 cells the factor is only ~0.4 Sharpe (mean 0.39, 2/9 cells >0.5) at 35bp before unmodeled alt borrow. Carry-harness build deferred to post-#13 (true-arb, cheaper). Gates 1-3 not run.
- [x] **#12 New-listing drift** — DONE 2026-06-10. TAIL-MIRAGE / NO-GO. `scripts/fetch_listing_data.sh` (de-survivorship: 732 perps ever listed) + `scripts/listing_drift_study.py`; verdict `results/listing_drift_verdict_2026-06-10.md`. Survivor-only probe looked great (N1/M7 +7.7%, t=2.78) but de-survivorship + funding bleed collapsed it 10× (mean +0.73%, t=0.52, drop-top-5% INVERTS to -1.95%). Real downward median (+7%, win 63%) but un-tradeable short: fat squeeze loss-tail, mean-net insignificant. De-survivorship was decisive — exactly why the fetch was mandatory.
- [x] **#4 Taker-flow imbalance (first pass)** — DONE 2026-06-10. FEE-DEAD / NO-GO. `scripts/taker_flow_study.py`; verdict `results/taker_flow_verdict_2026-06-10.md`. Full universe n=501,826: gross move on extreme taker-flow z = 1-7bp vs 15bp cost (t=-94 on the no-edge), negative every year. Zero forward predictive power — aggressor side is contemporaneous, not leading. Cleanest no-go of Phase 0. v2 (metrics-archive ratio) stays in Phase 2, evidence-downgraded.

## Phase 1 — infra builds (unlock the rest)

- [ ] **`scripts/download_metrics.sh`** — data.binance.vision `futures/um/daily/metrics/<SYM>/` → `data/metrics/<SYM>.csv` (OI, L/S ratios, taker ratio). Unlocks #1, #2, #4-v2, #6, #9. ~1 day. HIGHEST-LEVERAGE BUILD.
- [ ] **Cross-venue funding fetch** — Bybit + OKX public REST full funding history → CSVs. Unlocks #13.
- [ ] **Deribit DVOL fetch** — public API, BTC+ETH from 2021-03. Unlocks #15.
- [ ] **Macro event calendar** — static file: CPI + FOMC + NFP timestamps 2020–2026 (published schedules). Unlocks #14.
- [ ] **Coinbase spot candles fetch** — public API, majors. Unlocks #16 (+ #5 spot leg).
- [ ] **DefiLlama fetches** — stablecoin supply daily (#17) + unlocks/emissions mapping (#19).
- [ ] **Coin Metrics Community CSVs** — realized cap BTC/ETH (#20 context gate).

## Phase 2 — Tier-A backtests (after fetchers)

- [ ] **#1 OI divergence** — `OIMode` in `pkg/strategy/entry.go`, mirror `checkFundingCross` accessor pattern. TDD: mirror `TestPurgatory_*` / side-filter test pattern. Δprice×ΔOI sign combos at 4H close.
- [ ] **#2 L/S extreme fade** — `LSRatioMode`, percentile-threshold entry; top-trader vs global divergence variant.
- [ ] **#13 Cross-venue funding spread** — pure CSV math script, 4-leg cost model. No engine.
- [ ] **#14 Macro-event windows** — event study on local 1m data; two pre-registered variants ONLY (follow 30m post-event direction 24h / fade first 15m spike). No grid.
- [ ] **#15 DVOL VRP** — both standalone signal AND gate-on-live-config variants.

## Phase 3 — second wave

- [ ] **#5 Basis spot-perp dislocation** — `BasisMode`, parallel spot CSV.
- [ ] **#6 OI-confirmed breakout** — OI gate on `checkEMACrossover` (could improve LIVE config; beware confluence-filter history −23/−59%).
- [ ] **#16 Coinbase premium** — z-score tilt; check corr vs #18 (flow family).
- [ ] **#18 ETF flow momentum** — 2024+ only; pre-register the short-history evidence downgrade.
- [ ] **#19 Token-unlock front-run** — DefiLlama mapping → short into unlock ≥1% supply; alt-short carry bleed in harness.
- [ ] **#3 Funding carry harvest (delta-neutral)** — needs two-leg harness (perp short + spot long). Biggest build, highest ceiling. Decide build after #8/#13 results (they test carry cheaply first).
- [ ] **#7 Liquidation-cascade reversal** — liquidationSnapshot archive; if too sparse, proxy from 1m wick+volume spikes; else document as untestable.

## Phase 4 — gates-only / conditional

- [ ] **#17 Stablecoin supply impulse** — gate-context only (~3 independent flips, never standalone).
- [ ] **#20 MVRV-z / SOPR** — gate-context only (power flag SEVERE, ~1 cycle).
- [ ] **#9 Positioning-stress composite** — ONLY if ≥1 of #1/#2/#3 survives alone.
- [ ] **#10 Vol-targeted sizing overlay** — on live config; targets DSR/Sharpe directly. Cheap, price-only-exempt (same bet, better sized).

## Phase 5 — family-level honesty checks (after all individual runs)

- [ ] Intra-family correlation: carry family (#3, #8, #13), flow family (#16, #17, #18), OI family (#1, #6, #9) — `candidate_correlation.py`. Correlated pairs count as ONE bet.
- [ ] Full 20-candidate overfit matrix → `backtest_overfit_analysis.py`: report family DSR + participation ratio with real (correlation-deflated) trial count.
- [ ] Verdict doc per candidate in `results/` (incl. no-gos) + synthesis doc.
- [ ] Any survivor → propose shadow (promotion-LOCKED, research-only — same as existing 8 shadows).

## Standing rules

- Pre-register thresholds before running (project discipline; results/INDEX.md catalog).
- Big raw numbers → audit confounds (min-universe, fantasy-compounding, optimistic cost) before believing.
- Every "beats baseline" → per-year decomposition + corr-to-LIVE BEFORE celebrating.
- Don't touch live config / VPS. Forward-paper continues independently.

## Review

(fill as phases complete)
