# Candidate #14 — Macro-event windows (CPI/FOMC/NFP) — VERDICT: NO-GO (2026-06-10)

**Type:** verdict (pre-registered design applied to data → mechanical reject).
**Candidate:** #14 of the 20-candidate search (`results/strategy_candidates_batch2_2026-06-10.md`).
**Script:** `scripts/macro_event_study.py` (calendar built in-file: NFP computed, CPI+FOMC hardcoded from published BLS/Fed schedules).
**Data:** local 1m klines, BTC+ETH, 206 events 2020–2026 (152 CPI/NFP + 104 FOMC across 2 symbols).

## The bet

Since ~2022 BTC trades as a macro asset; CPI/FOMC/NFP timestamps are exogenous and deterministic — a clock the price series cannot see. Two pre-registered variants only (no grid): (a) momentum — follow the +0..30min post-event direction for 24h; (b) fade — fade the first 15min spike for 4h.

## Result

| variant | event | n | gross | mean | med | win | t | drop5 |
|---|---|---|---|---|---|---|---|---|
| momentum | CPI | 152 | 302.7 | −18.5 | −22.0 | 43% | −0.42 | −93.9 |
| momentum | FOMC | 104 | 317.5 | +12.2 | **+41.3** | 56% | 0.29 | −31.0 |
| momentum | NFP | 152 | 214.8 | +9.2 | −29.8 | 46% | 0.34 | −36.4 |
| fade | CPI | 152 | 159.7 | −12.0 | −29.7 | 45% | −0.69 | −34.2 |
| fade | FOMC | 104 | 162.2 | −44.6 | −24.9 | 44% | −2.01 | −69.8 |
| fade | NFP | 152 | 147.2 | −36.3 | −28.1 | 41% | −2.13 | −62.2 |

## Verdict: NO-GO.

No variant×event clears the bar (gross≥30bp AND positive median AND survives top-5% drop AND post-2022 positive). Findings:

- **Fade is uniformly wrong** (all three events negative, FOMC/NFP t≈−2.0): macro spikes **continue**, they don't revert at 4h. Fading loses significantly.
- **Momentum/FOMC is the only flicker**: median +41bp, win 56% — following the post-FOMC direction has some persistence (the documented post-FOMC drift). But n=104, **t=0.29 (insignificant)**, drop-top-5% −31bp (tail-dependent), gross 318bp (enormous variance). Not tradeable — the positive median rides a few big drifts, swamped by noise at this n.
- Momentum/CPI and NFP have negative medians — no clean directional persistence.

Same structural result as the rest of the search: macro events mark **when** volatility happens (gross moves 147–318 bp) but carry no fee-clearing, statistically-resolvable **direction** at the two pre-registered horizons. The one economically-sensible effect (post-FOMC momentum) exists directionally but is too weak/noisy to monetize at n=104.

## Gate-4 note

The post-2022 check was applied (macro coupling era). Even restricting to 2022-26, no cell turned tradeable. 2020-21 being dead was expected and not the disqualifier — the post-2022 window also fails.

## Gates not reached

Walk-forward / overfit / corr-to-LIVE not run — nothing cleared the screen with a significant positive-median edge.

## Cross-references

- Spec: `results/strategy_candidates_batch2_2026-06-10.md` (#14)
- Honesty lens: `results/settlement_drift_verdict_2026-06-10.md` (#11)
