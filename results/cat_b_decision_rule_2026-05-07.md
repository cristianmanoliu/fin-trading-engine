# Pre-registered decision rule for Cat B exit-framework alternatives
**Written: 2026-05-07, BEFORE wiring or running B1/B2 backtests.**
**Same shape as Cat A rule — applied uniformly across all Cat B candidates.**

## What we're testing

Each candidate replaces the FIXED 6R take-profit (and/or wick stop) with an
alternative exit logic. **SAME entry trigger, SAME signal TF, SAME side filter,
SAME universe, SAME max-hold cap, SAME fees/slip/funding** — isolates the EXIT
contribution.

| Candidate | Mechanism                                 | Mode flag             | Pre-registered parameters             |
|-----------|-------------------------------------------|-----------------------|---------------------------------------|
| **B1**    | Trailing stop (ratchet at 1R intervals)   | `--trailing-stop-mode` | `--trail-interval-r 1.0`              |
| **B2**    | Multi-level TP (50% at 3R, 50% at 6R, raise stop to BE on remainder after partial) | `--multi-level-tp-mode` | `--mid-r 3.0 --mid-frac 0.5`          |

**Trailing-stop algorithm (B1):**
- Track `maxFavorableR` = peak favorable price excursion in R-multiples since entry.
- When `maxFavorableR ≥ 1.0`: lock the stop at `entry + (floor(maxFavorableR) − 1) × originalStopDist` (LONG; mirror for SHORT).
- This means: at 1R favorable → stop at entry (BE). At 2R → stop at +1R locked. At 3R → +2R locked. Etc.
- Stop only ratchets in the favorable direction; never loosens.
- Take-profit (6R) remains as backstop in case price runs straight through without retracement.

**Multi-level TP algorithm (B2):**
- At entry: full position open, stop at wick + buffer (1R risk), target at 6R.
- When price reaches `entry ± 3.0 × originalStopDist` (mid-R): close 50% of position at mid-R price; emit a separate tradeResult with `outcome=PARTIAL`; raise stop on the remaining 50% to entry (BE).
- The remaining 50% then runs to either: (a) 6R target (full TARGET) or (b) BE-stop (gross ≈ 0, after fees slightly negative).
- Funding accrues per-leg: partial leg from open to mid-R hit; remainder leg from open to final exit.

## Baseline

`results/walk_forward_4H_short_6w_ema9-21_mh504_2026-05-06.txt`:
- Sum: **+$922,615**  Mean: **+$153,769**  Wins: **4/6**  Verdict: SUPPORTIVE
- Per-window NETs: W-2 −$275k, W-1 +$460k, W0 +$129k, W1 +$81k, W2 +$565k, W3 −$36k

All Cat B candidates run with: `--signal-tf 4H --side-filter short --target-rr 6.0
--max-hold-hours 504 --fee-bps 10 --stop-slippage-bps 15 --funding-csv-dir data/funding`,
universe-57.

## Decision rule (Stage 1: standalone viability)

Each candidate evaluated on its 6-window NET vs the baseline:

| Outcome                                                         | Verdict        | Action                                              |
|-----------------------------------------------------------------|----------------|-----------------------------------------------------|
| Wins ≥5/6 AND **sum > baseline (+$922k)**                       | **STRONG**     | Replace exit framework in live deploy               |
| Wins ≥4/6 AND sum > baseline                                    | **SUPPORTIVE** | Shadow deploy + 90 days forward observation         |
| Wins ≥4/6 AND sum > 0 but ≤ baseline                            | **NEUTRAL**    | No deploy change (no improvement over current)      |
| Wins 3/6 OR sum < 0                                             | **Inconclusive** | Reject                                            |
| Wins ≤2/6                                                       | **REJECTED**   | Reject                                              |

**Note on trade count (B2 only):** Multi-level TP emits a separate tradeResult
for each partial close. This roughly doubles `total_trades` for the leg that
hits 3R. The decision rule uses NET (sum), which aggregates correctly across
partial+remainder legs. Trade count differences vs baseline are an audit
artifact, NOT a deploy signal.

**Note on win classification (B1 only):** Under TrailingStopMode, when the
trailed stop fires above entry (LONG) or below entry (SHORT), the trade is
classified as a WIN (matches PnL sign). Under baseline (no trailing), all
stop-hits are classified as losses regardless of sign (irrelevant in baseline
since stop is always below entry for LONG). This is an intentional semantic
change confined to B1 mode.

## Stage 2 (only for STRONG / SUPPORTIVE)

Compute the standard deviation of per-window NET vs baseline σ:

| σ ratio                          | Tag                  | Effect on action |
|----------------------------------|----------------------|------------------|
| `σ_candidate < 0.7 × σ_baseline` | **VARIANCE-REDUCING** | Preferred — Sharpe improvement |
| `0.7 × σ_baseline ≤ σ_candidate ≤ 1.3 × σ_baseline` | **NEUTRAL** | Sum delta is the deploy signal |
| `σ_candidate > 1.3 × σ_baseline` | **VARIANCE-INCREASING** | Flagged but not auto-rejected (deploy decision still on sum) |

Variance is observational. The deploy decision is sum + win-rate from Stage 1.

## Pre-registered hypothesis

**B1 (trailing stop):**
The validated shorts-asymmetry (5y aggregate +$1.08M shorts vs longs) suggests
crypto-perp downtrends extend further than uptrends. Fixed 6R may cut tail
captures short. A 1R trail captures more of that tail at the cost of:
- Fewer "clean" 6R wins (some get stopped out at +2R, +3R locked instead)
- Longer trade durations on average → more funding cost (shorts: net benefit; longs: net cost)
- More signals exiting at small profits → wider WR distribution

**Prior expectation:** small-to-moderate sum improvement (+5% to +30%); higher
WR (~25-30%); modest funding-cost increase. Most likely outcome: **NEUTRAL or
SUPPORTIVE.** STRONG would be surprising (would require trail to dominate
fixed-RR by ≥1.0% per window — large effect).

**B2 (multi-level TP):**
Variance reduction without changing edge sign. PRIOR EXPECTATION:
- Sum slightly lower (~−5% to flat) due to extra partial-close fees and capped tail captures.
- Win rate higher (40-50% from 17.5% baseline; partial-close legs always win).
- σ across windows lower — banking partial wins shaves off losing-window magnitude.

Most likely outcome: **NEUTRAL on sum but VARIANCE-REDUCING on σ.** If sum
is preserved AND σ shrinks, that's a Sharpe win even at NEUTRAL Stage 1.

## Commitments NOT to do regardless of results

1. NOT test more parameter values (different trail intervals like 0.5R or 2R; different mid-R splits like 2R/4R or 60/40 fractions) after seeing results.
2. NOT redefine "win" beyond per-window NET > baseline.
3. NOT switch to Sharpe / max-DD / equity-curve metrics to break ties — sum is the gating signal at Stage 1.
4. NOT exclude windows for being atypical (W-2 COVID rally, W1 BTC consolidation, etc.).
5. NOT combine B1+B2 into a third candidate after seeing results — that's a separate test for a future session with its own pre-registered rule.
6. Will deploy as live / shadow ONLY by the rule above.

## What this leaves

If both REJECTED: exit framework is robust at fixed 6R + wick + mh504. The
search has now exhausted 5 entry-trigger variants AND 2 exit-framework
variants. Strong evidence that current configuration is near-optimal for
this cost stack.

If one or both SUPPORTIVE: candidate gets a shadow slot. Forward-paper
observation of live vs shadow over 90 days informs a future deploy decision.

If one STRONG: rare outcome. Would warrant replacing live exit, but
require an additional confirmation run (different random seed if seed
exists; or partial-window robustness check) before deploy. Pre-register
that confirmation step too if reached.
