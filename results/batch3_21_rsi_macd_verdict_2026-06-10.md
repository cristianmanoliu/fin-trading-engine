# #21 RSI/MACD deep validation — VERDICT: NO-GO (both). Ledger item CLOSED. (2026-06-10)

**Pre-reg:** `results/strategy_candidates_batch3_2026-06-10.md` (#21), locked criteria.
**Scripts:** `scripts/gen_batch3_journals.sh` (per-trade journals, MACD + RSI cells) +
`scripts/batch3_21_analysis.py`. **Cells CSV:** `results/batch3_21_cells_2026-06-10.csv`.
**Settles:** the open CANDIDATE item from `alt_signals_phantom_corrected_verdict_2026-06-09.md`
/ `docs/RESEARCH_BACKLOG.md` ("pending fresh pre-registration") — now closed as NO-GO.

## Setup

Per-trade journals, continuous 2020→2026-06, deployed-20 journal universe
(`gen_live_journals.sh` SYMS), live cost model (fee=10bp, slip=5bp, mh504, 4H,
short-only, funding CSV). Three locked cells: EMA-9/21 baseline, MACD-12/26/9, RSI-14.

## Results

| cell | trades | total | mean | median | WR% | drop-top-5% mean | full-yr windows + | years + | t | corr-to-baseline (monthly) |
|---|---:|---:|---:|---:|---:|---:|---|---|---:|---:|
| baseline | 3,491 | **$467,009** | 133.8 | −1,089 | 20.1 | −217 | 5/6 | 5/7 | 2.89 | — |
| MACD | 5,523 | $333,251 | 60.3 | −1,087 | 18.6 | −297 | 2/6 | 3/7 | 1.67 | 0.779 |
| RSI | 5,916 | $516,283 | 87.3 | −1,069 | 20.0 | −260 | 3/6 | 3/7 | 2.57 | **0.844** |

Yearly net ($k): baseline 1/20/299/−160/192/120/−5 · MACD −35/−17/177/−79/−27/301/13 ·
RSI −125/−49/362/−232/228/367/−34 (2020…2026).

## Verdict per locked criteria

- **MACD: NO-GO** — fails (a) median, (b) drop5, (c) 2/6 windows, (d) 3/7 years,
  (e) corr 0.779 with total BELOW baseline. Fails everything. Dead.
- **RSI: NO-GO** — fails (a) median, (b) drop5, (c) 3/6 windows, (d) 3/7 years.
  Passes only (e) via total > baseline (+10.5%). The +10.5% is concentrated in
  2022+2025 (crash/down regimes); RSI LOSES money in 4 of 7 calendar years including
  −$232k in 2023. Monthly corr 0.844 = same bet as the live config, levered harder
  into its good regimes and bleeding harder in its bad ones. Zero diversifier value.

## Methodology notes (honest caveats, both directions)

1. **Criterion (b) drop-top-5% fails the BASELINE too** (mean −217 after drop). For a
   6:1-RR / ~20%-WR trade stream, payoff concentration in the top trades is the design,
   not a mirage — so (b) is structurally unpassable at trade level for this strategy
   class and is NOT the discriminating evidence here. The discriminators are (c), (d),
   (e): regime breadth and independence — and both variants fail those decisively.
2. **Universe deviation from the +79% claim:** the corrected 2026-06-09 numbers were
   3-window walk-forward deltas on the 57-symbol universe; this study is per-trade on
   the deployed-20 (the decision-relevant book). On this book RSI's edge over baseline
   shrinks to +10.5% total with 3/7 positive years. Even taking the 57-sym +79% at face
   value, corr 0.844 and 4-losing-years regime profile kill the diversifier case —
   which was the only path to value (the DSR(34)=0.658 haircut already barred a
   straight swap).

## Conclusion

The phantom-corrected RSI/MACD "CANDIDATE" status is settled: **NO-GO, permanently.**
RSI is the live edge wearing a different hat — more trades, same regime bet, worse
breadth, corr 0.84. The backlog's deeper closure reason (crash-dependent profile,
fails multiple-testing haircut) is CONFIRMED by per-trade evidence. No shadow, no
further re-tests of this family.
