# Pre-registered decision rule for 6-window parameter validation
**Written: 2026-05-06, BEFORE running any of the 6-window tests.**
**Purpose: prevent retroactive goalpost-moving when results land.**

## What we're testing

4 configurations on 4H short, slip=15bp, universe-57, across 6 non-overlapping 12-month windows:

| Config | EMA pair | Max-hold | Notes |
|---|---|---|---|
| C1 | 9/21 | 336h (14d) | **Current deployed baseline** |
| C2 | 9/21 | 504h (21d) | Test if longer max-hold has signal |
| C3 | 5/15 | 336h | Test if faster EMA has signal |
| C4 | 5/15 | 504h | Test joint candidate |

## Windows (6 non-overlapping, 12-month each)

- W-2: 2020-05 → 2021-04 (in-sample to selection)
- W-1: 2021-05 → 2022-04 (in-sample)
- W0:  2022-05 → 2023-04 (in-sample)
- W1:  2023-05 → 2024-04 (in-sample)
- W2:  2024-05 → 2025-04 (in-sample)
- W3:  2025-05 → 2026-04 (true OOS for original universe selection)

Note: ALL 6 windows are in-sample for the original universe-57 selection (which used 2020-2025). But for **parameter** selection (5/15 vs 9/21), only the windows we haven't already optimized against count as OOS. Since we identified 5/15 / mh504 as candidates from a 3-window test (W1/W2/W3), the 3 NEW windows (W-2, W-1, W0) are the cleanest OOS for THAT specific question. The old 3 windows are partially in-sample for parameter selection.

## Decision rules — applied LITERALLY after results land

### For 5/15 vs 9/21 EMA (compare C3 vs C1):

| Outcome | Verdict | Action |
|---|---|---|
| 5/15 beats 9/21 in **≥5/6 windows** AND mean delta > 0 | **STRONG signal** | Switch deployed config to 5/15 EMA |
| 5/15 beats 9/21 in **exactly 4/6 windows** AND mean delta > 0 | **WEAK signal** | Build shadow-mode for forward validation |
| 5/15 beats 9/21 in **3/6 windows** | **Inconclusive** | Keep 9/21 (chance baseline = 3/6) |
| 5/15 beats 9/21 in **≤2/6 windows** | **REFUTED** | Keep 9/21, retire question |

### For mh504 vs mh336 (compare C2 vs C1):

Same rule as above, with C2 vs C1.

### For joint (C4 vs C1):

If C4 beats C1 in ≥5/6 windows AND each individual move (C2, C3) also showed signal — joint deployment justified. Otherwise treat as confirmation of individual results.

### Tiebreakers (if ambiguous):
- "Beats" means strictly NET_C > NET_baseline in that window
- "Mean delta" = arithmetic mean of (NET_C - NET_baseline) across all 6 windows
- Window weighting: equal weight per window (no special treatment for true-OOS W3)

## What I commit NOT to do regardless of results

1. **Will NOT** test additional configs after seeing results ("but what about 6/13 EMA?")
2. **Will NOT** redefine "win" to mean something other than per-window NET comparison
3. **Will NOT** weight windows differently after seeing the data
4. **Will NOT** discard windows for being "atypical" or "regime-specific"
5. **Will NOT** invent a new criterion (Sharpe ratio, drawdown, etc.) to break ties retroactively
6. **Will NOT** test the next-most-promising EMA pair if 5/15 is REFUTED
7. **Will deploy or not** purely based on the rule above

## Pre-registered hypothesis

Based on the 3-window result (anti-correlated train/test ranks), the prior is **5/15 will fall in the "Inconclusive" or "REFUTED" bucket** (≤3/6 wins). If it lands in STRONG (≥5/6), that's strong evidence I was wrong about the parameter-selection-is-noise interpretation.

Same prior for mh504: expected REFUTED.

If both candidates land in STRONG: significant update, parameter tuning may have real signal contrary to today's mechanism finding.

If both land in REFUTED: strong reinforcement of "parameters are noise within robust region" interpretation.
