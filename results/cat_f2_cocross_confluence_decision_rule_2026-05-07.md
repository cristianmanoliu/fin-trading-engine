# Cat F2 — Co-Cross Confluence Filter — PRE-REGISTERED Decision Rule

**Status:** PRE-REGISTERED — committed before any backtest is run.

This file is the contract. The implementation, walk-forward sweep, and verdict
that follow MUST honor the rule below as written. Any post-hoc adjustment to
threshold K, window definition, time tolerance, or decision criteria invalidates
the test.

## Hypothesis

Bearish EMA9/21 crosses on the 4H signal timeframe produce **higher expected
NET-per-trade** when ≥ K other universe-57 symbols also cross bearishly at the
same 4H close. Mechanism: co-clustered crosses indicate market-wide regime
shift; individual crosses during such moments capture systematic moves rather
than idiosyncratic single-symbol noise.

This is a **filter** on the deployed strategy — not a new entry signal. The
test asks: of the EMA crosses we're already taking, which ones occur during
regime shifts?

This directly probes the "structural trend asymmetry" hypothesis from the
holy-grail synthesis — one of three remaining unknowns about why the deployed
shorts work.

## Filter shape (locked)

At each 4H candle close, define:

```
n_cocross = number of OTHER 56 universe symbols with bearish EMA9/21 cross
            at the same 4H close (within ±60s tolerance)
```

A deployed-16 SHORT trade is RETAINED if and only if `n_cocross >= 5`.
Otherwise it's filtered out.

Locked parameters:
- **K = 5** (≈9% of the 56-symbol cross-count universe)
- **Time tolerance = ±60s** at the 4H boundary
- **Universe for cross-counting = 57** (per `configs/symbols.yaml: universe`)
- **Trading universe = deployed-16** (per `configs/symbols.yaml: deployed`)

**No threshold sweep permitted.** K=5 is principled rather than optimized,
in the spirit of Cat F1's mean+2σ funding threshold (30 bp/day).

## Implementation approach (locked)

**Two-pass post-hoc analysis** — no engine code changes.

1. **Pass 1 — capture all bearish crosses on universe-57:** run the existing
   EMA backtest on all 57 symbols with `--journal-dir` (the flag added in B1).
   Output: 57 per-symbol JSONLs, each containing every bearish EMA9/21 cross
   event with its 4H boundary timestamp.

2. **Pass 2 — count cocrosses, filter:** for each deployed-16 trade in
   `results/hod_journals/2026-05-07-mfe/` (already cached), look up how many
   of the OTHER 56 universe symbols had a cross at the same 4H boundary
   (±60s match). Retain only trades with `n_cocross >= 5`.

3. **Aggregate** — compute on the filtered subset:
   - Total NET, WR, trades, NET-per-trade
   - Compare against the unfiltered baseline (deployed-16, slip=5, identical
     parameters — already cached as 2,210 trades / +$687k NET / 22.2% WR)

4. **Walk-forward** — repeat per-window on the same 6 windows as Cat F1
   (W-2 → W3, locked). For each window, compute filtered vs unfiltered.

This avoids any code changes to `engine.go` / `entry.go` / `stub.go`. Pure
Python analysis on existing journal data. ~1 hour total.

## Exit framework (locked, identical to deployed)

- Fixed 6:1 R:R take-profit
- Wick-based stop (`stop_buffer_pct=0.001`)
- Max-hold 504 hours
- Costs: `--fee-bps 10 --stop-slippage-bps 5 --funding-csv-dir data/funding`

The filter touches ENTRY admission only. Exit framework, costs, max-hold
are all unchanged from the deployed candidate.

## Sample window (locked)

- **Aggregate test:** continuous 5y (2020-01 → 2025-04) per the existing
  cached journals.
- **Walk-forward:** 6 windows identical to Cat F1.

## Decision rule (locked, committed before viewing any result)

A FILTER decision rule differs from a standalone-signal rule (Cat F1). The
right test is "does filtering improve outcome?" — measured across multiple
metrics. Apply the FIRST matching tier:

| Tier | Conditions (ALL must hold) | Action |
|---|---|---|
| **DEPLOY-CANDIDATE** | (a) filtered NET ≥ unfiltered NET (filter doesn't shrink total) AND (b) ≥5/6 walk-forward windows where filtered NET ≥ unfiltered AND (c) filtered NET-per-trade > unfiltered | Pre-register for forward-paper validation post-current-window |
| **SHADOW DEPLOY** | (a) filtered NET-per-trade ≥ 1.3× unfiltered AND (b) filtered total NET ≥ 60% of unfiltered AND (c) ≥4/6 windows where filtered NET-per-trade beats unfiltered | Run as paper-live shadow alongside deployed |
| **WALK-FORWARD CANDIDATE** | (a) filtered WR > unfiltered WR by ≥ 2pp on aggregate AND (b) positive NET in ≥3/6 windows | Eligible for re-test in next milestone |
| **REJECT** | otherwise | Closed |

Note: orthogonality (r < 0.5) does NOT apply here — a filter is by definition
a subset of the same signal, so per-window NET correlation with deployed will
be high. The right test is improvement-over-baseline, not orthogonality.

## Pre-registered probability estimates

These are my priors before running. Recording them so I can compare against
the actual outcome.

| Outcome | Pre-registered probability |
|---|---:|
| REJECT | 50% |
| WALK-FORWARD CANDIDATE | 25% |
| SHADOW DEPLOY | 18% |
| DEPLOY-CANDIDATE | 7% |

Higher success-prior than Cat F1 (which had 60% REJECT) because:
- This is a filter on a known-positive signal, not a standalone signal
- Regime clustering is a documented market phenomenon
- The lowest tier (WALK-FORWARD CANDIDATE) requires only +2pp WR
  improvement, easier to clear than Cat F1's standalone bar

## Falsification (locked)

If REJECT verdict, the filter is closed for this milestone. **Explicitly
forbidden under this pre-registration:**
- Sweeping K — only K=5 tested
- Sweeping universe size (57 locked)
- Sweeping time tolerance (±60s locked)
- Changing exit framework (6:1 RR, mh504, fee=10/slip=5 all locked)
- Re-running with different cocross direction definitions (bearish-only locked)

A WALK-FORWARD CANDIDATE result (lowest tier) means re-test in the *next*
milestone after the current forward-paper window resolves — NOT immediate
deployment. SHADOW DEPLOY and DEPLOY-CANDIDATE outcomes both require forward-
paper validation before any real-money allocation per the existing
go/no-go criteria.

## Implementation contract

The implementation MUST:

1. Reuse `scripts/hod_journals.sh` for Pass 1 (set `SYMBOLS` env to the
   universe-57 list).
2. Write a Python analyzer that:
   - Loads universe-57 cross events into a `(boundary_ts → set of symbols)` index
   - For each deployed-16 trade, computes `n_cocross` at its `ts` (excluding
     the trade's own symbol)
   - Splits the deployed trade pool into FILTERED (n_cocross ≥ 5) and EXCLUDED
   - Aggregates per-window and aggregate stats with the locked decision rule
3. Output `results/cat_f2_cocross_aggregate_2026-05-07.csv` (per-trade or
   per-symbol filtered/unfiltered comparison) and `results/cat_f2_cocross_walk_forward_2026-05-07.csv`
   (per-window comparison).
4. Apply the decision rule mechanically and emit a verdict.

## Verdict

To be filled after the run, in
`results/cat_f2_cocross_confluence_verdict_2026-05-07.md`. The verdict will
quote this file, apply the locked decision rule mechanically, and record the
outcome — DEPLOY-CANDIDATE / SHADOW DEPLOY / WALK-FORWARD CANDIDATE / REJECT.

---

*Pre-registered 2026-05-07, committed before implementation begins.*
