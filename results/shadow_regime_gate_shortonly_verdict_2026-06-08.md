# BTC-anchored regime gate (SHORT_ONLY) — cross-cohort verdict

**Date:** 2026-06-08
**Question:** For each EMA configuration (live + 8 deployed shadows), does the BTC short-gate
(sit FLAT during non-bearish BTC regimes) beat always-short in OOS walk-forward?

**Gate:** BTC trailing return ≤ −X% over Y days → SHORT regime; else → FLAT (LONG suppressed).
**SHORT_ONLY=1:** LONG days from the regime labeler are relabeled FLAT. The strategy *only*
sits out; it never flips long. This is the deployable hypothesis — LONG was dropped as unstable
noise (breadth verdict 2026-06-08).

**Criteria (pre-registered, locked):**
1. Gate OOS total beats always-short OOS total.
2. No two adjacent losing OOS years.
3. ≤2 distinct (X,Y,Z) picks across 4 folds, same X.

**Anchor:** `data/anchor/BTCUSDT-1d.csv`
**Universe:** 57 symbols, 5y continuous, fee=10bp, slip=5bp, exact-fills, include-boundary.

---

## Results table (ranked by OOS edge = gate − baseline)

| cohort | gate OOS | base OOS | edge | verdict | picks |
|--------|----------|----------|------|---------|-------|
| alt5-15-336 | $+1,434,955 | $+207,210 | $+1,227,745 | **POSITIVE** | (5,30,14) / (5,30,7) |
| live-9/21-504 | $+356,329 | $+356,329 | $+0 | **NEGATIVE** | (5,30,7) ⚠️ degenerate |
| alt5-15-504 | $+323,217 | $+323,217 | $+0 | **NEGATIVE** | (5,30,7) ⚠️ degenerate |
| bb20 | $+0 | $+0 | $+0 | **NEGATIVE** | (20,7,7) ⚠️ degenerate |
| alt5-21-504 | $+270,664 | $+270,664 | $+0 | **NEGATIVE** | (5,30,7) ⚠️ degenerate |
| alt7-14-504 | $+326,572 | $+326,572 | $+0 | **NEGATIVE** | (5,30,7) ⚠️ degenerate |
| alt10-30-504 | $+133,559 | $+133,559 | $+0 | **NEGATIVE** | (5,30,7) ⚠️ degenerate |
| alt12-26-504 | $+23,901 | $+23,901 | $+0 | **NEGATIVE** | (20,30,7) / (5,30,7) ⚠️ degenerate |
| alt21-50-504 | $-55,703 | $-55,703 | $+0 | **NEGATIVE** | (5,14,7) / (5,30,7) ⚠️ degenerate |

---

## Findings

**POSITIVE cohorts (1):** alt5-15-336

**NEGATIVE cohorts (8):** live-9/21-504, alt5-15-504, bb20, alt5-21-504, alt7-14-504, alt10-30-504, alt12-26-504, alt21-50-504

**Degenerate (gate == baseline):** live-9/21-504, alt5-15-504, bb20, alt5-21-504, alt7-14-504, alt10-30-504, alt12-26-504, alt21-50-504

A degenerate result means the gate's FLAT carve-outs never fire differently from always-short
for that config — typically fast-EMA + long max-hold (few, long trades that span regimes).
Interpret as: gate has no effect, not as NEGATIVE evidence of harm.

---

## Caveats

1. **SHORT_ONLY ≠ original live verdict.** The original BTC POSITIVE (+$700k, full gate) was
   ~50% LONG P&L (decomposition: short +$531k, long +$529k; 2023 was 66% long). This sweep
   removes that LONG leg entirely. A NEGATIVE short-only result for a cohort that was POSITIVE
   full-gate is *expected* — it means the edge was in the LONG flip, not the sit-out.

2. **Gross of switching costs.** Each regime boundary incurs a force-close + re-entry. Not
   modeled as an extra cost here (episode slicing force-closes at tick price, which includes
   slippage, but there's no explicit "cross the spread twice on a regime switch" cost).

3. **Episode-length confound.** SHORT_ONLY removes LONG episodes, biasing toward shorter hold
   times during SHORT episodes. Configs with tight max-hold (alt5-15-336, 336h) naturally
   produce more frequent entries/exits within a SHORT episode; configs with loose max-hold
   (504h) may see fewer trades per episode.

4. **Research-only.** No live/shadow/VPS changes. Per locked decision rule: any deployment
   decision requires a separate productionization discussion.
