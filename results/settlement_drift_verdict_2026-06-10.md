# Candidate #11 — Funding-settlement window drift — VERDICT: TAIL-MIRAGE / NO-GO (2026-06-10)

**Type:** verdict (pre-registered design applied to data → mechanical reject).
**Candidate:** #11 of the 20-candidate orthogonal search (`results/strategy_candidates_batch2_2026-06-10.md`).
**Script:** `scripts/settlement_drift_event_study.py` (self-documents the verdict on re-run).
**Data:** 100% local — 57-symbol funding CSVs + 1m klines, 2020–2026. Zero fetch.

## Pre-registration (frozen before the run, baked in the script header)

- Settlement clock: 00/08/16 UTC (confirmed 8h cadence, all 57 syms).
- Extreme buckets: per-symbol top/bottom 10% of funding (EXTREME_HIGH = longs pay; EXTREME_LOW = shorts pay).
- Event window: pre = (−120min, 0], post = (0, +120min], log-return of close anchored at settlement minute.
- Cost floor: 15 bp round-trip (10 fee + 5 slip, project standard). Fee-death pass = 2× = 30 bp.
- Power floor: n ≥ 60 per bucket to read.
- Headline tradeable rule (sign COMMITTED before run): EXTREME_LOW, LONG into settlement, exit at settlement.

## Result

| bucket | leg | n | mean bp | std bp | t |
|---|---|---|---|---|---|
| EXTREME_HIGH | pre | 32087 | +14.44 | 211.9 | 12.21 |
| EXTREME_HIGH | post | 32097 | −5.08 | 223.1 | −4.08 |
| **EXTREME_LOW** | **pre** | **7652** | **+39.81** | **368.4** | **9.45** |
| EXTREME_LOW | post | 7659 | −3.29 | 294.4 | −0.98 |
| MID | pre | 285198 | −0.50 | 162.9 | −1.64 |
| MID | post | 285224 | −3.19 | 166.4 | −10.25 |

The fee-death **screen** passes (EXTREME_LOW/pre = 39.81 bp > 30 bp). The committed-sign **honest test** also looked like a pass at first:

- pooled net **+24.81 bp/trade, t=5.89, n=7652**
- **positive in all 7 calendar years** 2020–2026 (gate-4 by-year decomposition passed)

This is exactly the trap. **Two deeper checks kill it:**

| check | value | implication |
|---|---|---|
| median net | **−1.03 bp** | the typical trade LOSES after cost |
| win rate | 49.6% | coin flip |
| drop top 1% (76 trades) | mean 24.81 → **4.78 bp** | edge mostly gone |
| drop top 5% (382 trades) | mean → **−28.76 bp** | edge **inverts** |
| top-1% share of total PnL | **80.9%** | ~76 trades carry the whole result |

## Verdict: TAIL-MIRAGE — NO-GO. No engine mode built.

The +24.81 bp mean is manufactured by ~76 violent up-snaps out of 7,652 events. EXTREME_LOW funding = capitulation/bear extremes, where the 2h pre-settlement window occasionally V-recovers hundreds of bp. A signal whose entire edge is a handful of unpredictable tail events is un-tradeable: the median trade is a net loss, the win rate is a coin flip, and real slippage on exactly those violent candles would erase the tail gains. Per-trade this is a coin-flip plus a lottery ticket.

This is the **same crash-window mirage** every prior project finding fell to (RSI/MACD/Momentum/cross-sectional), but hidden one layer deeper: not concentrated in one *year* (which would have failed gate 4), but concentrated in the *tail of the trade distribution within every year*. The by-year decomposition alone is **not sufficient** — median + tail-trim is the decision-grade test.

**Lesson banked:** gate-4 "positive across years" can be passed by a fat-tailed signal that is a coin-flip per trade. Future event-study candidates must report **median net + drop-top-k% + top-1% PnL share**, not just pooled mean and by-year. The `settlement_drift_event_study.py` HONEST-TEST block now enforces this and emits the TAIL-MIRAGE verdict mechanically.

## Gates not reached

Walk-forward (gate 1), overfit-matrix (gate 2), correlation-to-LIVE (gate 3) were **not run** — a fee-dead / un-tradeable signal does not consume those degrees of freedom. EXTREME_HIGH/pre (t=12.21 but net −0.56 bp) is fee-dead by construction. Post-settlement legs show no structure.

## Cross-references

- Spec: `results/strategy_candidates_batch2_2026-06-10.md` (#11)
- Plan: `tasks/todo.md` Phase 0
- Fee-death precedent: Purgatory −$40M (`project_purgatory_method_feedeath`)
- Prior crash-window mirages: `results/cross_sectional_verdict_2026-06-10.md`, `results/overfit_expansion_2026-06-09.md`
