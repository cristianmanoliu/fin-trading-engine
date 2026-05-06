# Pre-registered decision rule for Cat D/E filter alternatives
**Written: 2026-05-07, BEFORE wiring or running D1/E1 backtests.**
**Same shape as Cat A/B rules — applied uniformly across both candidates.**

## What we're testing

Each candidate is a **filter** on top of the deployed entry trigger (4H EMA 9/21
cross, shorts-only). Same trigger, same exit framework (fixed 6R + wick stop +
mh504), same fees/slip/funding, same universe-57. The filter REJECTS some signals
that would otherwise enter — never adds new ones. **Trade count strictly decreases.**

| Candidate | Mechanism                                      | Mode flag                  | Pre-registered parameters                    |
|-----------|------------------------------------------------|----------------------------|----------------------------------------------|
| **D1**    | Multi-TF confluence: 1D EMA bias must agree    | `--confluence-1d-mode`     | 1D EMA 9/21 sampled at 4H candle CloseTime hour=0 UTC |
| **E1**    | Volatility regime: skip top-extreme vol        | `--vol-filter-mode`        | `--max-vol-annualized 1.20` (120% annualized 30d realized vol)  |

**D1 algorithm (1D bias confluence):**
- Sample 4H candle close into a 1D EMA pair when `c.CloseTime.UTC().Hour() == 0`. This is the Binance canonical daily close (close of the 20-24 UTC 4H candle).
- Maintain 1D EMA(9) and 1D EMA(21) on those daily samples.
- When the 4H bearish EMA cross fires (would emit SHORT), gate: emit only if 1D EMA(9) < 1D EMA(21). When the 4H bullish cross fires, gate: emit only if 1D EMA(9) > 1D EMA(21). Disagreement → signal dropped silently.
- Mirror logic for LONG signals (currently filtered out by `--side-filter short` anyway, but the gate is symmetric).
- Implementation note: aggregating "daily" inside the entry detector (via the hour=0 sampling rule) avoids architectural changes to the runner / aggregator. The 1D EMA values are identical to those that would be computed on Binance native 1D candles.

**E1 algorithm (volatility regime filter):**
- Compute log returns on 4H closes: `r_t = log(close_t / close_{t-1})`.
- Maintain a rolling window of the last 180 returns (≈30 calendar days at 6 4H candles/day).
- When window has ≥30 returns: realized vol = stdev(returns) × sqrt(6 × 365) → annualized.
- When the 4H entry cross fires: gate: emit only if `realizedVol30d ≤ 1.20` (120% annualized).
- Disagreement → signal dropped.
- Rationale: panic/liquidation regimes where 4H momentum signals are too late to reversal. Excludes events like LUNA collapse, FTX week, COVID March 2020 — periods where the directional move is essentially over by the time a 4H cross confirms.

## Baseline

`results/walk_forward_4H_short_6w_ema9-21_mh504_2026-05-06.txt`:
- Sum: **+$922,615**  Mean: **+$153,769**  σ: **$313k**  Wins: **4/6**  Verdict: SUPPORTIVE
- Per-window NETs: W-2 −$275k, W-1 +$460k, W0 +$129k, W1 +$81k, W2 +$565k, W3 −$36k

All Cat D/E candidates run with identical framework: `--signal-tf 4H --side-filter short
--target-rr 6.0 --max-hold-hours 504 --fee-bps 10 --stop-slippage-bps 15 --funding-csv-dir
data/funding`, universe-57, EMA 9/21 hardcoded.

## Decision rule (Stage 1: standalone viability)

Each candidate evaluated on its 6-window NET vs baseline:

| Outcome                                                         | Verdict        | Action                                              |
|-----------------------------------------------------------------|----------------|-----------------------------------------------------|
| Wins ≥5/6 AND **sum > baseline (+$922k)**                       | **STRONG**     | Replace deploy (filter added to live engines)       |
| Wins ≥4/6 AND sum > baseline                                    | **SUPPORTIVE** | Shadow deploy + 90 days forward observation         |
| Wins ≥4/6 AND sum > 0 but ≤ baseline                            | **NEUTRAL**    | No deploy change (no improvement over current)      |
| Wins 3/6 OR sum < 0                                             | **Inconclusive** | Reject                                            |
| Wins ≤2/6                                                       | **REJECTED**   | Reject                                              |

**Trade-count caveat:** filters strictly reduce trade count. Expected reductions:
- D1: ~40-60% drop (1D bias agrees with 4H signal direction in roughly half of all moments).
- E1: ~10-25% drop (high-vol regimes are minority of trade time but concentrated in tail events).

If trade count drops below ~150 over the 6-window aggregate for any single candidate
(power floor for the deployed-16), flag the verdict as **STATISTICALLY UNDERPOWERED**
and treat any positive verdict with extra skepticism. This is a labeling step,
not a separate gate — Stage 1 verdict still applies.

## Stage 2 (only for STRONG / SUPPORTIVE)

Standard deviation comparison:
- `σ_candidate < 0.7 × σ_baseline` → **VARIANCE-REDUCING** — preferred
- `0.7 × σ_baseline ≤ σ_candidate ≤ 1.3 × σ_baseline` → **NEUTRAL band**
- `σ_candidate > 1.3 × σ_baseline` → **VARIANCE-INCREASING** — flagged but not auto-rejected

Stage 2 is observational, not gating. Stage 1 verdict is the deploy signal.

## Pre-registered hypotheses

**D1 (1D confluence):**
The 1D both-sides walk-forward (3/3 windows positive, +$63k/yr, slip-robust)
suggests 1D carries directional information. Filtering 4H signals through
1D bias agreement should:
- Reduce trade count by ~50%
- Raise per-trade win rate (filtered trades are more aligned with the dominant trend)
- May or may not raise sum: improvement depends on whether dropped trades had negative expectancy
- σ direction unclear (fewer trades + more aligned regime exposure could go either way)

PRIOR: most likely **SUPPORTIVE or NEUTRAL.** STRONG would require 1D filter to
add ≥50% to per-trade EV, which is implausible given the 4H signal already
captures most of the structural shorts asymmetry. REJECT outcome would mean
counter-trend 4H trades have NON-NEGATIVE expectancy that we'd be filtering
out.

**E1 (vol regime filter):**
Mechanism analysis (2026-05-06) found mid-vol altcoins were the sweet spot.
Hypothesis: high-vol periods (>120% annualized) are panic/liquidation cascades
where:
- Move is mostly priced in by the time 4H cross confirms
- Slippage tail is fatter
- Time-stop hits are more common (long range-bound moves after the vol burst)

PRIOR: most likely **NEUTRAL or marginally SUPPORTIVE.** A handful of removed
high-vol periods (e.g., LUNA week, FTX week, COVID March) might have been
costly trades. But baseline already has 4/6 positive windows including W2
(+$565k) which contained several high-vol regimes — so the filter could
remove WINNERS too. REJECT outcome would mean high-vol periods are
disproportionately profitable (counter-intuitive but possible).

## Commitments NOT to do regardless of results

1. NOT change the threshold parameters (D1: EMA 9/21 specifically; E1: 120% specifically) after seeing results.
2. NOT redefine "win" beyond per-window NET > 0.
3. NOT switch to Sharpe / max-DD / Sortino as a tiebreaker — sum is the gate.
4. NOT exclude windows for being atypical.
5. NOT combine D1+E1 as a third candidate after seeing results — separate test for a future session.
6. NOT add asymmetric vol thresholds (e.g., also filtering bottom-extreme vol) after seeing E1 results.
7. Will deploy as live / shadow ONLY by the rule above.

## What this leaves

If both REJECT or NEUTRAL: cumulative search exhaustion across 9 candidates
(5 entry triggers + 2 exit frameworks + 2 filters) all rejected. Strong
evidence that the deployed configuration is genuinely robust at this
trigger/TF/cost stack. No more sweep variants warranted; remaining work
is operational (forward-paper monitoring tools) and possibly architectural
(Cat F portfolio-level sizing requires multi-symbol backtest mode).

If one or both SUPPORTIVE/STRONG: the candidate gets a shadow slot. Forward-
paper observation over 90 days informs deploy decision.

## Multiple-comparison caveat

This is the **8th and 9th candidates** tested against the same baseline.
At a nominal α=0.05, family-wise error control across 9 tests would require
each individual test to be α≈0.0056. Pre-registered rules with high bars
partially mitigate, but a SUPPORTIVE result on the 9th candidate should be
treated more skeptically than a SUPPORTIVE result on the 1st. Mentally weight
these as ~3× more likely than baseline to be a false positive.
