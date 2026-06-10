# Candidate #15 — DVOL volatility-risk-premium — VERDICT: MARGINAL (standalone NO-GO; regime-gate suggestive, not deployable) (2026-06-10)

**Type:** verdict (pre-registered design applied to data → reject standalone; gate flagged).
**Candidate:** #15 of the 20-candidate search (`results/strategy_candidates_batch2_2026-06-10.md`).
**Scripts:** `scripts/fetch_dvol.py` (Deribit DVOL) + `scripts/dvol_vrp_study.py`.
**Data:** Deribit DVOL daily close BTC+ETH **2021-03-24 → 2026-06** (1864 overlap days — the spec's "from 2021-03" was right; an earlier narrow-window probe wrongly suggested 2022) + local 1m klines for realized vol.

## The bet

VRP = DVOL (implied vol) − realized vol. High VRP = market overpaying for protection = fear → contrarian long; negative VRP = complacency → flat/short. Options-market information, absent from any price series. Two pre-registered variants: A standalone contrarian daily, B regime-gate on a short book.

## Result

**Variant A (standalone, the deployable one): DEAD.**
- BTC: n=707, mean −0.21 bp, median −5.0, Sharpe −0.01, drop-top-5% −33.8.
- ETH: n=715, mean +5.7 bp, median −1.1, Sharpe 0.30, drop-top-5% −36.9.
- Both: median negative, edge inverts on top-5% drop. No standalone VRP timing edge.

**Variant B (regime gate — short-return conditional on VRP-z): suggestive, ETH only.**
- ETH: short next-day return given complacency (z<0) = +19.6 bp vs given fear (z>0) = −16.5 bp, **Welch t=2.04**. And robust across years — positive split in 5/6 (2021 +41, 2023 +22, 2024 +75, 2025 +36, 2026 +145; only 2022 inverts −15). The contrarian thesis holds: after fear extremes the market mean-reverts up, so shorts do worse.
- BTC: same sign (+1.0 vs −9.5) but **Welch t=0.79** — not significant.

## Verdict: MARGINAL.

This is the closest any candidate has come to a real signal across the search so far: an exogenous (options-derived), multi-year-robust regime effect with the economically correct sign. But it does NOT clear the pre-registered bar:

1. **Standalone (variant A) is dead** — Sharpe ~0, median negative, tail-inverting. The directly-deployable form has no edge.
2. **The gate (variant B) is clean on ETH (t=2.04) but weak on BTC (t=0.79).** DVOL only covers BTC+ETH; a gate on the live multi-symbol short book would have to generalize, and it is significant on only 1 of the 2 symbols where it can even be measured. One significant symbol is not a deployable cross-sectional overlay.
3. The spec pre-registered "Sharpe>1 standalone OR a clean, significant gate." ETH-only t=2.04 is suggestive, not clean across the testable universe.

**Disposition:** NO-GO for deployment now. Flagged for revisit IF (a) per-symbol options-vol indices become available beyond BTC/ETH (so the gate can be tested across the deployed book), or (b) a future milestone tests the ETH-specific VRP gate on the ETH shadow in isolation. Not actionable against the current forward-paper run (don't touch live config — project rule).

## Honest note

The 5/6-year robustness of the ETH split is the strongest non-price evidence the search has produced. It is recorded so that if the project ever extends to per-symbol IV data, this is the first thing to re-test. It is NOT promoted now because a 1-of-2-symbol gate is not a robust overlay, and the deployable standalone form is dead.

## Gates partially reached

Variant B got a by-year decomposition (gate 4 — passes for ETH). Walk-forward / overfit / corr-to-LIVE not run — standalone dead, gate not robust enough across symbols to justify the DoF.

## Cross-references

- Spec: `results/strategy_candidates_batch2_2026-06-10.md` (#15)
- Fetcher: `scripts/fetch_dvol.py` (data/dvol/, reusable)
- Prior gate skepticism: regime-gate F=0/9 (`project_regime_gate_shortonly_degenerate` memory)
