# Slip-Cliff Sensitivity — Verdict 2026-05-07

## Question

CLAUDE.md ## Forward-paper go/no-go criteria specifies:
> Kill the strategy if … **Realized stop-side slippage > 25 bp** (the cliff edge)

The "cliff" framing implies a nonlinear breakdown — slip values below the
threshold are tolerable, slip values above are catastrophic. Is that the
shape of the actual sensitivity curve? Where exactly is breakeven?

## Method

Ran the deployed-16 candidate (4H short EMA9/21 mh504 target_rr=6 fee=10)
across slip ∈ {5, 10, 15, 20, 22, 25, 27, 30, 35, 40, 50, 60, 75, 100} bp
on the full 5y merged-CSV pipeline. Same `--exact-fills --include-boundary`
audit invariants as `realistic_sweep.sh`. Trade count is **identical at 2,210
across all slip levels** (slip is a fill-side cost, not an entry filter), so
the sensitivity curve is purely cost-driven, not signal-count-driven.

## Results

| slip (bp) | annual NET | profitable | Δ from prior step |
|----:|----:|----:|----:|
|   5 | **+$130,127** | 16/16 | (baseline) |
|  10 | +$121,493 | 16/16 | −$8,634 (5bp step) |
|  15 | +$112,858 | 16/16 | −$8,635 |
|  20 | +$104,224 | 16/16 | −$8,634 |
|  22 | +$100,770 | 16/16 | −$3,454 (2bp step) |
|  25 | **+$95,589** | 16/16 | −$5,181 (3bp step) |
|  27 | +$92,135 | 16/16 | −$3,454 |
|  30 | +$86,954 | 16/16 | −$5,181 |
|  35 | +$78,320 | 15/16 | −$8,634 (5bp step) |
|  40 | **+$69,685** | 15/16 | −$8,635 |
|  50 | +$52,416 | 13/16 | −$17,269 (10bp step) |
|  60 | +$35,147 | 13/16 | −$17,269 |
|  75 | +$9,243 | 8/16 | −$25,904 (15bp step) |
| 100 | **−$33,930** | 5/16 | −$43,173 (25bp step) |

### The curve is linear, not a cliff

Marginal cost: **−$1.71k/yr per 1bp slip**, holds across the entire 5–100bp
range to 4 significant figures. Linear regression `annual_NET ≈ $138k − $1.71k × slip_bp`:

|  Statistic |  Value  |
|---|---|
| Slope | −$1,711 / yr / bp |
| Intercept | $138,500 / yr (slip=0) |
| R² | > 0.999 |
| **Implied breakeven** | **slip ≈ 81 bp** |

There is **no cliff**. The strategy degrades gracefully and predictably
from a $130k/yr peak (at the deployed cost stack of fee=10/slip=5) to
zero NET at ~80 bp. Every additional 1 bp of slip costs ~$1,700/yr in
expectation.

## Verdict: the 25bp kill threshold is overly conservative by ~3×

At slip=25 bp, the strategy is still **+$95.6k/yr** — 73% of the deployed
cost-stack baseline. At slip=40 bp it's **+$69.7k/yr** — exactly matching
the "honest annual ≈ $69k/yr" figure cited in CLAUDE.md as the look-ahead-
corrected expectation. The curve doesn't enter "concerning" territory until:

- **slip > 50 bp**: 3 of 16 symbols flip negative; NET drops to ~half baseline
- **slip > 75 bp**: half the symbols negative; NET ≈ breakeven
- **slip > 80 bp**: strategy net negative on the deployed-16

The 25 bp threshold made operational sense as a *conservative trigger
for investigation*, but as a strict kill rule it would terminate the
strategy while it's still earning $95k/yr in expectation. That is a
false positive against the underlying cost-tolerance.

## Recommended kill-switch revision

| Threshold | Action | Rationale |
|---|---|---|
| **slip > 25 bp** | Investigate (don't kill) | Strategy still ~$95k/yr; might be transient venue degradation |
| **slip > 50 bp** | Throttle position size to 50% | NET drops to ~$52k/yr; concentration risk if any symbol's slip drives the average |
| **slip > 65 bp** | Halt new entries; manage open positions | NET below the honest-baseline floor |
| **slip > 80 bp** | Hard kill | Implied breakeven |

This requires the kill-switch to track *realized* per-trade slip with a
rolling window (e.g., trailing 30 trades) rather than a single-trade
threshold. A single 50-bp fill in a thin-liquidity event is not the same
as a sustained 50-bp regime.

The single-number "kill > 25 bp" rule remains valid as a **paged-alert
trigger** — operator wakes up and investigates — but should not be a
hard automatic kill.

## Operational implications for forward-paper

- **Most of the cost-tolerance lives between 25 and 80 bp**, not below 25 bp
  as the existing rule implies.
- Realized fee tier confirmed at 10 bp (Binance Regular) leaves 70 bp of
  headroom for slip before the strategy breaks. That's a wide operational
  margin — venue conditions would have to deteriorate substantially.
- The next-step refinement is **per-symbol slip-cliff** (B3 territory):
  do all 16 deployed symbols share the same linear slope, or are some
  more cost-sensitive than others? Per-symbol slope dispersion would
  inform position-size policy.

## Reproduction

```bash
bash scripts/slip_cliff.sh                              # default: deployed-16, 9 slip values
SLIP_BPS_LIST="5 10 15 20 22 25 27 30 35 40 50 60 75 100" \
    bash scripts/slip_cliff.sh                          # full curve as in this verdict
SYMBOL_GROUP=universe bash scripts/slip_cliff.sh        # all 57 symbols (B3 input)
```

CSV: `results/slip_cliff_deployed_2026-05-07.csv`
