# Cat F1 — Funding-Cross Standalone Signal — PRE-REGISTERED Decision Rule

**Status:** PRE-REGISTERED — committed before any backtest is run.

This file is the contract. The implementation, walk-forward sweep, and verdict
that follow MUST honor the rule below as written. Any post-hoc adjustment to
the threshold, window definition, or decision criteria invalidates the test.

## Hypothesis

The 8h funding-rate level on Binance perp futures, when used as a STANDALONE
entry trigger (not as a filter on the deployed EMA-cross signal), generates
positive expected NET. Mechanism: extreme funding marks position-crowding
extremes, which mean-revert.

This is **orthogonal** to the deployed strategy. EMA cross fires on price
momentum; funding cross fires on positioning crowding. Tested as a
genuinely-different mechanism — not as another sweep over the existing
EMA-cross parameter space.

## Entry trigger (locked)

At each 4H candle close, observe the last-known 8h funding rate published by
the exchange. Compute:

```
funding_per_day_bps = rate_8h × 3 × 10000
```

Three branches:

| funding_per_day_bps | Entry |
|---|---|
| > +30 | **SHORT** entry at the candle close (overcrowded longs revert) |
| < −30 | **LONG** entry at the candle close (overcrowded shorts revert) |
| ∈ [−30, +30] | No entry |

The threshold of ±30 bps/day is committed before viewing any test result. It
is roughly mean + 2σ of the universe's funding-rate distribution per the
B3 features data (`mean_8h ≈ 0.0001`, `std_8h ≈ 0.0004`, so mean+2σ ≈ 27 bp/day,
rounded to 30). No threshold sweep is permitted in this test. If the threshold
is wrong it remains wrong; the verdict reflects that.

## Exit framework (locked, identical to deployed)

- Fixed 6:1 R:R take-profit
- Wick-based stop (`stop_buffer_pct=0.001` per the default config)
- Max-hold 504 hours
- Costs: `--fee-bps 10 --stop-slippage-bps 5 --funding-csv-dir data/funding`
  (fee = 10 bp round-trip, stop-slip = 5 bp, historical funding accrual)

These are identical to the deployed-candidate cost stack so the comparison is
clean.

## Universe (locked)

The deployed-16 set per `configs/symbols.yaml:deployed`. Tested on the same
symbols as the deployed strategy so the comparison reflects what we'd run in
production. NOT swept across the 57-symbol universe — that would consume
selection-bias degrees of freedom.

## Sample window (locked)

- **Aggregate test:** continuous 5y (2020-01 → 2025-04) per existing
  `realistic_sweep.sh` methodology (merged-CSV-once, no monthly forced closes).
- **Walk-forward:** identical 6-window scheme as the deployed candidate's
  walk-forward (per `scripts/walk_forward.sh`). No window redefinition.

## Decision rule (locked, committed before viewing any result)

Apply the FIRST matching tier:

| Tier | Conditions (ALL must hold) | Action |
|---|---|---|
| **ADOPT** | n/a — never for a 14th candidate | Forbidden by rigor frame |
| **SHADOW DEPLOY** | (a) ≥ 4 of 6 walk-forward windows positive AND (b) mean annual NET ≥ +$50k/yr AND (c) Pearson correlation with the deployed-candidate's per-window NET vector < 0.5 (orthogonality check) | Deploy as paper-live shadow alongside current; collect forward-paper data |
| **WALK-FORWARD CANDIDATE** | ≥ 3 of 6 windows positive AND mean annual NET > $0 | Eligible for re-test in the next milestone; not deployed |
| **REJECT** | otherwise | Closed |

Correlation cutoff of 0.5 chosen because below 0.5 ≈ < 25% shared variance,
which is the threshold below which ensembling delivers meaningful Sharpe
improvement under standard portfolio-theory math.

## Pre-registered probability estimates

These are my priors before running anything. Recording them so I can compare
against the actual outcome and learn from any large surprise.

| Outcome | Pre-registered probability |
|---|---:|
| REJECT | ~60% |
| WALK-FORWARD CANDIDATE | ~25% |
| SHADOW DEPLOY | ~12% |
| ADOPT-eligible | ~3% |

A result of "mean annual NET > $50k/yr AND ≥ 4 windows positive AND
correlation < 0.5" would meaningfully update my prior toward the existence of
a genuinely-orthogonal mechanism. That's the holy-grail-grade outcome.

A result of "mean annual NET < $0 AND ≥ 4 windows negative" decisively
falsifies the hypothesis at this threshold. **No threshold-sweep retry is
permitted under that outcome** — that's data mining.

## Implementation contract

The implementation must:

1. Add a new entry mode `FundingCrossMode` to `pkg/strategy/entry.go` (do
   NOT reuse existing modes).
2. Plumb funding-rate access into the EntryDetector via a setter, so the
   detector can call `RateAt(c.CloseTime)` at signal-evaluation time.
3. Emit signals exactly per the trigger table above, with `Reason` like
   `"funding_cross SHORT | rate_8h=0.000245 → +73.5bp/day | rr=6.0"`.
4. Include a Go unit test verifying the trigger fires at +30bp/day, +30.1
   bp/day; doesn't fire at +29.9bp/day; symmetric on negative side.
5. Wire `--funding-cross-mode` and `--funding-threshold-bps` flags in
   `cmd/backtest`. The threshold defaults to 30 (locked).

## Verdict

To be filled after the run, in
`results/cat_f1_funding_cross_verdict_2026-05-07.md`. The verdict will quote
this file, apply the locked decision rule mechanically, and record the
outcome — whether SHADOW DEPLOY, WALK-FORWARD CANDIDATE, or REJECT.

---

*Pre-registered 2026-05-07, committed before implementation begins.*
