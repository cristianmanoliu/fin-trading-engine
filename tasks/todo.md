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

- [x] **#1 OI divergence** — DONE 2026-06-10. NO-GO. `scripts/oi_divergence_study.py`; verdict `results/oi_lsratio_verdict_2026-06-10.md`. All 4 Δprice×ΔOI combos clear the gross screen (130-334bp) but direction is noise: mean/median net negative, win 42-50%, drop5 deeply negative. Sorts volatility not direction. No engine mode (no `OIMode` built — screen killed it pre-engine).
- [x] **#2 L/S extreme fade** — DONE 2026-06-10. NO-GO (both crowd-fade A + divergence B). Same verdict doc. n=2.5-2.6M: gross 62-320bp but mean/median net negative, win 38-48%, t=-218 against the edge. Extremes don't fade tradeably. No `LSRatioMode` built.
- [x] **#13 Cross-venue funding spread** — DONE 2026-06-10. NO-GO (dead-arbed). `scripts/fetch_crossvenue_funding.py` + `scripts/crossvenue_spread_study.py`; verdict `results/crossvenue_spread_verdict_2026-06-10.md`. Binance↔Bybit |spread| mean 0.96bp (heavily arbed), sign flips 24.6% → 30bp 4-leg flip cost swamps ~1bp/settle collection. Negative Sharpe every threshold/venue. **CARRY FAMILY CLOSED**: #8 marginal (~0.4 Sh) + #13 dead → #3 delta-neutral harness NO-BUILD.
- [x] **#14 Macro-event windows** — DONE 2026-06-10. NO-GO. `scripts/macro_event_study.py` (NFP computed + CPI/FOMC published dates); verdict `results/macro_event_verdict_2026-06-10.md`. 206 events BTC+ETH. Fade uniformly wrong (spikes continue, t≈-2). Momentum/FOMC only flicker (med +41bp win 56%) but t=0.29 insignificant, tail-dependent. Events mark vol not direction. Post-2022 also fails.
- [x] **#15 DVOL VRP** — DONE 2026-06-10. MARGINAL (standalone NO-GO; regime-gate suggestive not deployable). `scripts/fetch_dvol.py` + `scripts/dvol_vrp_study.py`; verdict `results/dvol_vrp_verdict_2026-06-10.md`. Variant A (standalone) dead (Sharpe ~0, median neg, tail-inverts). Variant B (regime gate): ETH short-ret split by VRP regime t=2.04, robust 5/6 years, correct sign — but BTC only t=0.79, and DVOL covers only BTC+ETH so can't gate the multi-symbol book. Strongest non-price evidence yet, but 1-of-2-symbol gate ≠ deployable. Flagged for revisit if per-symbol IV data appears.

## Phase 3 — second wave

- [ ] **#5 Basis spot-perp dislocation** — `BasisMode`, parallel spot CSV.
- [x] **#6 OI-confirmed breakout** — DONE 2026-06-10. NO-GO. `scripts/oi_gate_study.py`; verdict `results/live_overlay_verdict_2026-06-10.md`. OI-rising gate on live shorts (2830 trades): Sharpe 1.361→1.448 (+0.087, below +0.2 bar), halves net$. Gates disappoint (confluence history holds). No engine change.
- [x] **#16 Coinbase premium** — DONE 2026-06-10. MARGINAL (strongest gate of search; not deployable book-wide). `scripts/fetch_flow_data.py` + `scripts/coinbase_premium_study.py`; verdict `results/coinbase_premium_verdict_2026-06-10.md`. Standalone dead. Gate (short-timing by premium-z): clean on BOTH BTC (t=2.39) + ETH (t=2.29), 6/7 years each — when US not bidding, shorts work. INDEPENDENT of #15 (corr -0.12) = 2 distinct exogenous signals. But Coinbase covers only BTC/ETH, can't gate the alt-heavy live book. Flagged for a future BTC/ETH-only sub-book (combine #15+#16 gate).
- [~] **#18 ETF flow momentum** — DEFERRED 2026-06-10 (data-access blocked). Farside Cloudflare-403, SoSoValue endpoint moved/auth. Spec already flagged #18 as weakest (2.4y, one regime). Given #16 (the stronger flow signal, full history) is MARGINAL-not-deployable, #18 would at best replicate that conclusion on shorter data. Revisit only if a free ETF-flow source is found. Not a gap in the conclusion.
- [ ] **#19 Token-unlock front-run** — DefiLlama mapping → short into unlock ≥1% supply; alt-short carry bleed in harness.
- [~] **#3 Funding carry harvest (delta-neutral)** — NO-BUILD 2026-06-10 (carry family closed: #8 ~0.4 Sharpe marginal + #13 dead-arbed). Two-leg harness not justified — nothing for it to harvest. Revisit only if a future data axis revives the carry premium. See `results/crossvenue_spread_verdict_2026-06-10.md`.
- [ ] **#7 Liquidation-cascade reversal** — liquidationSnapshot archive; if too sparse, proxy from 1m wick+volume spikes; else document as untestable.

## Phase 4 — gates-only / conditional

- [x] **#17 Stablecoin supply impulse** — DONE 2026-06-10. NO-GO. `scripts/stablecoin_supply_study.py`; verdict appended to `results/coinbase_premium_verdict_2026-06-10.md` + own section. Standalone Sharpe 0.34 (dead). Gate split t=0.20, 3/7yr — no liquidity-regime signal. Underpowered exactly as the ~3-regime power flag predicted.
- [ ] **#20 MVRV-z / SOPR** — gate-context only (power flag SEVERE, ~1 cycle).
- [~] **#9 Positioning-stress composite** — PRECLUDED 2026-06-10. Pre-reg required ≥1 of #1/#2/#3 to survive standalone; #1 NO-GO, #2 NO-GO, #3 NO-BUILD. None survived → #9 precluded by its own gate. See `results/oi_lsratio_verdict_2026-06-10.md`.
- [x] **#10 Vol-targeted sizing overlay** — DONE 2026-06-10. NO-GO. `scripts/vol_sizing_overlay_study.py` + `scripts/gen_live_journals.sh`; verdict `results/live_overlay_verdict_2026-06-10.md`. VOLTGT Sharpe 0.725 < baseline 0.809 (WORSE). Live $-risk sizing already normalizes by stop-distance (= vol read); re-weighting double-counts and hurts. Deployed sizing is already vol-aware — nothing to capture.

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
