# Deployed-16 Direct Sweep — VERDICT: LIVE CONFIG IS NOT BEATEN

**Status:** Research finding, 2026-07-27. Follow-up to the atr1.0 transfer failure.
**Question:** Once we stop optimizing on the 57-symbol universe and sweep **directly on the 16
symbols actually traded**, does anything beat the live config?
**Answer: No.**

**Span:** TRAIN 2020-01 → 2023-12, deployed-16 only, `--fee-bps 10 --stop-slippage-bps 5`,
audit fills (`--exact-fills --include-boundary --pessimistic-ambiguous`), historical funding.
**Baseline = the LIVE config** (4H EMA 9/21, shorts-only, 6:1 RR, wick stop, mh504).

## Full ranking — 15 arms

| # | Arm | NET | vs baseline |
|---|---|---:|---:|
| 1 | mh336 | $393,300 | +$14,350 |
| **2** | **baseline (LIVE)** | **$378,951** | **—** |
| 3 | conf1d | $364,314 | −$14,637 |
| 4 | atr1.0 | $352,901 | −$26,050 |
| 5 | mh1008 | $347,404 | −$31,547 |
| 6 | mh720 | $339,249 | −$39,702 |
| 7 | atr1.25 | $332,820 | −$46,130 |
| 8 | atr0.75 | $267,777 | −$111,174 |
| 9 | atr1.5 | $226,562 | −$152,389 |
| 10 | atr1.75 | $167,452 | −$211,498 |
| 11 | atr2.0 | $147,038 | −$231,912 |
| 12 | vol-filter | $130,421 | −$248,530 |
| 13 | atr2.5 | $120,157 | −$258,794 |
| 14 | trailing-stop | $81,623 | −$297,327 |
| 15 | atr0.5 | $43,112 | −$335,839 |

**The live config ranks 2nd of 15, and the only arm above it is not significant.**

## The ATR direction is closed for this book

Full multiplier curve on the deployed-16 (TRAIN):

| Mult | Trades | Gross | Fees | NET |
|---|---:|---:|---:|---:|
| 0.5 | 1,779 | $205,953 | $126,765 | $43,112 |
| 0.75 | 1,642 | $351,899 | $76,510 | $267,777 |
| 1.0 | 1,493 | $400,021 | $51,279 | $352,901 |
| 1.25 | 1,316 | $358,187 | $35,409 | $332,820 |
| 1.5 | 1,187 | $240,216 | $26,456 | $226,562 |
| 1.75 | 1,113 | $176,353 | $21,227 | $167,452 |
| 2.0 | 1,052 | $151,529 | $17,498 | $147,038 |
| 2.5 | 961 | $121,818 | $12,564 | $120,157 |
| **wick (LIVE)** | 1,526 | **$471,081** | $79,935 | **$378,951** |

**The wick stop beats every one of the 8 ATR multipliers tested.** Note the gross column: the
wick stop produces the *highest gross of any arm* ($471,081). On the universe-57 the ATR stop
raised gross; on the deployed-16 it lowers it. This is the selection-interaction effect from
`atr1_0_holdout_verdict_2026-07-27.md`, seen from the other side — these symbols were chosen
*because* the wick stop works well on them.

## The one arm that beat baseline is noise

`mh336` (max-hold 336h instead of 504h), +$14,350 = **+3.8%** on a $378,951 base:

| Test | Value |
|---|---:|
| Paired t | **+0.82** |
| Sign-flip permutation p | **0.418** |
| Beats baseline | 10/16 |
| Median symbol | +$1,193 |
| Drop top-1 winner | +$5,668 |
| **Drop top-3 winners** | **−$4,623** |

Dropping three symbols flips the sign. This is not an effect; it is three symbols.

It is worth noting `mh336` is the max-hold used by the best-performing shadow (`alt5-15-336`),
so the direction is not absurd — but on the deployed-16 with the live EMA pair it does not
clear noise, and prior work already identified max-hold as the real lever separating the 336h
cohort (`shadow_regime_gate_shortonly_verdict_2026-06-08.md`). Nothing new here.

## Verdict

**No change to the live config is supported.** Fifteen arms, sweeping stop construction
(8 ATR multipliers), exit timing (3 max-holds), entry filtering (vol-filter, 1D confluence),
and exit mechanism (trailing stop) — on the actual deployed book, none beats the live config
by a statistically meaningful margin.

This is a **negative result, and it is a useful one**: it means the live config is not
obviously leaving money on the table in any of the directions available via existing CLI
levers. The config that has been running since 2026-05-05 is, as far as this sweep can tell,
the best available parameterization of this strategy class for these 16 symbols.

## What this does NOT resolve

The live book is still **net-negative in forward-paper** (−$7,750 over 118 trades, gross
+$3,489 against $11,238 of costs). This sweep says the *parameters* are not the problem. It
says nothing about whether the *strategy* has a durable edge — that remains the ~2026-08-25
forward-paper question, and the cost geometry (67× leverage, breakeven fee 0.20 bp) remains
the structural obstacle identified in
`stop_distance_cost_filter_verdict_2026-07-26.md`.

## Budget note

This arc adds ~15 more trials. Combined with the earlier arcs today, N has gone from ~39 to
~73 in a single session. **The luck bar rises accordingly** (SR0 at N=73 ≈ 0.359 monthly,
≈1.24 annualized). This is a real cost and is the reason the search should now stop: every
additional arm makes it harder for anything — including the live config — to clear the
multiple-testing haircut.

## Reproduction

```bash
source scripts/lib/symbols.sh; D=$(get_symbols deployed)
SYMBOLS="$D" VARIANT=d16-atr1.0 EXTRA="--atr-stop-mult 1.0" \
  START_YEAR=2020 END_YEAR=2023 END_YEAR_MONTH=12 \
  bash scripts/cost_geometry_sweep.sh results/d16_train/atr1.0.txt
```

Raw per-symbol outputs: `results/d16_train/`.

## Cross-references

- `atr1_0_holdout_verdict_2026-07-27.md` — the universe-57 result and its transfer failure.
- `atr_stop_cost_geometry_verdict_2026-07-26.md` — atr1.5 PARTIAL.
- `stop_distance_cost_filter_verdict_2026-07-26.md` — live cost decomposition.
