# Pre-registered decision rule for Cat A entry-mechanism alternatives
**Written: 2026-05-06 EOS, BEFORE running PDH/PDL or RSI tests.**
**Same shape as VWAP rule — applied uniformly across all Cat A candidates.**

## What we're testing

Each candidate replaces the EMA-cross entry trigger but uses the SAME exit
framework (wick stop, fixed RR=6 target, max-hold cap, fee/slip/funding modeling).
This isolates the entry-signal contribution.

| Strategy | Class | TF | Side | Mode flag |
|---|---|---|---|---|
| PDH/PDL break | momentum | 4H | short | --pdh-pdl-break-mode |
| RSI cross-50 | momentum | 4H | short | --rsi-mode |
| MACD bearish cross | momentum | 4H | short | (not yet built) |
| Bollinger break | momentum | 4H | short | (not yet built) |

All use mh504 (matching live), slip=15bp, fee=10bp, universe-57.

## Decision rule (Stage 1: standalone viability)

Compare each candidate to the deployed baseline (4H short EMA 9/21 mh504).
The baseline 6-window result: +$922k sum, 6/6 positive (from 6w validation).

| Outcome | Verdict | Action |
|---|---|---|
| Wins ≥5/6 windows AND mean delta > 0 vs baseline | STRONG | Deploy as live alternative or shadow |
| Wins 4/6 AND mean > 0 | WEAK | Deploy as shadow only |
| Wins 3/6 OR mean ≤ 0 | Inconclusive | Reject |
| Wins ≤2/6 | REJECTED | Reject |

If ALL candidates inconclusive/rejected → momentum-trigger choice is noise (consistent with EMA-pair sweep finding from earlier today). Don't change deploy.

## Stage 2: Diversification (only for STRONG/WEAK)

Compute correlation of per-window NET vs deployed baseline:
- r < 0.30 → DIVERSIFIER, deploy as shadow
- r ≥ 0.60 → REDUNDANT (same edge as baseline)

## Pre-registered hypothesis

Both PDH/PDL break and RSI cross-50 are momentum signals on the same
TF/cost stack as EMA cross. Prior expectation: both produce qualitatively
similar results (momentum captured roughly equivalently). Most likely
outcomes:
- Both clear Stage 1 WEAK or STRONG, but with high correlation (r > 0.5)
  to baseline → not deploy-worthy as diversifiers
- Or both REJECTED if their entry timing is materially worse than EMA
  cross at this TF

VWAP-fade was just REFUTED (mean-reversion fails in this framework). I
do NOT expect any momentum candidate to dramatically outperform EMA cross
— winner's curse pattern from EMA-pair sweep predicts noise.

A surprise upside (any candidate STRONG + DIVERSIFIER, r < 0.3) would be
significant — would suggest there IS untapped diversifier signal in
crypto perp shorts beyond just shorts-asymmetry.

## Commitments NOT to do regardless of results

1. NOT test more variants after seeing results
2. NOT redefine "win" beyond per-window NET > baseline NET
3. NOT switch to a different metric to break ties
4. NOT exclude windows for being "atypical"
5. Will deploy as shadow / live ONLY by the rule above
