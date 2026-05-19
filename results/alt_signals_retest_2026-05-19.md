# Alternative-signals re-test sweep — research pre-registration (2026-05-19, sweep #7)

**Status:** LOCKED 2026-05-19 BEFORE sweep execution. Pure research. Distinct from today's prior 6 sweeps (which tested PERTURBATIONS of the LIVE strategy) — this sweep tests ALTERNATIVE SIGNAL MECHANISMS.

## Context

Sweeps #1-#6 today mapped the LIVE strategy's parameter landscape (EMA periods, entry filters, side, exit mechanisms). All used the same entry signal: EMA cross. Sweep #7 explores whether a different SIGNAL mechanism would have its 2026-05-07 REJECTED verdict flipped at the current cost model.

### Why this is principled and not scope creep

bb20 became a shadow runner via the Cat A 2026-05-07 protocol: Bollinger breakdown was WEAK on backtest → added as shadow. The operational pattern of "backtest a different signal mechanism, add the survivor as a shadow" is precedented.

This sweep mirrors that protocol exactly:
- Pre-registered before any backtest run
- Decision rule explicit
- Findings filed; any "would-be shadow candidate" requires FRESH pre-reg next session for actual shadow promotion

### Prior verdicts (2026-05-06/07)

| Signal | Test | Verdict | Cost model |
|---|---|---|---|
| MACD line/signal cross | 4H short rr=6 mh504 | **REJECTED** (3/6 windows, mean −$26k) | slip=15bp, 6 windows |
| RSI cross-50 | 4H short rr=6 mh504 | **REJECTED** (minority positive) | slip=15bp, 6 windows |
| Bollinger breakdown | 4H short rr=6 mh504 | **WEAK** (4/6, mean +$250k) → became shadow | slip=15bp, 6 windows |

### Why re-test the REJECTED signals

Today's sweeps revealed cost-model fragility: vol-1.20 was SUPPORTIVE at slip=15 → −25% drag at slip=5; trail-1R was REJECTED at slip=15 → also negative-relative at slip=5 (sign flipped but relative position preserved).

The question: do MACD and RSI REJECTED verdicts also "preserve direction but shift magnitude" — or could the lower cost model flip them to positive?

**Pre-registered prediction:** Both still REJECT or look marginal. Lower slip lifts everything, but if MACD/RSI fundamentally don't have edge on this universe, slip change doesn't create one.

## What this sweep does

3-cell walk-forward grid:
- Cell 0: BASELINE (LIVE: EMA 9/21 cross)
- Cell 1: MACD-mode (MACD line/signal cross, 12/26/9 default)
- Cell 2: RSI-mode (RSI cross-50, period 14 default)

All cells share: 4H signal TF, short-only, mh504, slip=5, fee=10, RR=6, wick stop, 57 symbols, 3 walk-forward windows.

## What this sweep does NOT do

- Does NOT promote any signal to shadow. Even a winning finding here is "WALK-FORWARD CANDIDATE" research; shadow promotion requires fresh pre-reg next session.
- Does NOT deploy to LIVE. LIVE remains EMA cross 9/21.
- Does NOT iterate (e.g., "MACD looked OK, let me also test 9/21/5 periods"). Fixed 3 cells.

## The 3-cell grid (LOCKED)

| # | Role | Signal | Params | Rationale |
|---|---|---|---|---|
| 0 | BASELINE | EMA cross | 9/21 | LIVE config — reference. |
| 1 | MACD | MACD line/signal cross | fast=12, slow=26, signal=9 | Classic MACD periods. Matches 2026-05-07 single-point test. Cross-comparison at new cost model. |
| 2 | RSI | RSI cross 50 | period=14 | Default RSI period. Matches 2026-05-06 single-point test. |

Held constants:
- SIGNAL_TF=4H, SIDE_FILTER=short, TARGET_RR=6.0, FEE_BPS=10, SLIP_BPS=5, MAX_HOLD_HOURS=504
- Universe: 57 symbols
- 3 walk-forward windows

EMA mode is OFF for cells 1+2 (they use their own signal). EMA params on cell 0 default to 9/21 via harness.

## Pre-registered predictions

**Strong prior (both REJECT or marginal):** MACD and RSI fundamentally don't have edge on this universe at any cost model. The 2026-05-07 REJECT verdicts reflect mechanism, not cost — lower slip won't create a real signal.

**Weak prior on sign-flip:** lower slip lifts everything, so cells 1+2 may show POSITIVE mean (sign flipped from negative) but still WEAK or marginal relative to baseline.

**Falsifiable claim:** if MACD or RSI beats baseline by ≥10% mean across ≥2/3 windows, the prior is wrong and we have a candidate-for-shadow finding (still requires fresh pre-reg to promote).

**Falsifiable counter-claim:** if MACD or RSI show 3/3 windows positive AND mean within 30% of baseline, that's a DIVERSIFIER candidate (low correlation with baseline could justify shadow even if mean is lower).

## Decision rule

| Result | Action |
|---|---|
| Both cells lose to baseline by ≥10% AND fail walk-forward (1/3 windows or negative) | File as confirmation of prior REJECT verdicts. No action. |
| Either cell beats baseline ≥10% across ≥2/3 windows | File as WALK-FORWARD CANDIDATE. **Shadow promotion requires fresh pre-reg next session** with explicit "promote to shadow" rule + walk-forward stability check. No auto-promotion. |
| Either cell within 30% of baseline AND 3/3 windows | File as diversifier candidate. Same shadow-promotion rule. No auto-promotion. |
| Inconclusive (mixed signal) | File as research finding. No action. |

**No deployment from this sweep regardless of result.** This matches today's discipline pattern for all 6 prior sweeps.

## Statistical caveats

2 variants × 3 windows = 6 measurements. FWER under null ≈ 1 - 0.95^2 ≈ 10% — the lowest of today's sweeps (smaller grid = cleaner stats).

Note: today's cumulative measurement count is now ~117 across 7 sweeps. Multiple-comparisons across sweeps DOES accumulate — but each sweep's pre-reg was separate with its own decision rule, and any finding here that triggers shadow consideration would be subject to independent walk-forward validation in a future session before deployment.

## Output

- CSV: `results/alt_signals_retest_2026-05-19.csv`
- Markdown table appended after sweep
- Telegram notification

---

## Results — completed 2026-05-19 14:27 UTC

Sweep wall-clock: ~1.5 minutes. 9 measurements (3 cells × 3 windows).

### Per-cell results

| Cell | Role | Signal | W1 NET | W2 NET | W3 NET | Mean NET | Wins | Trades | vs Baseline |
|---:|:---|:---|---:|---:|---:|---:|:---:|---:|:---:|
| **0** | **BASELINE (LIVE)** | **EMA 9/21 cross** | **+175,021** | **+645,812** | **+65,044** | **+$295,292** | **3/3** | **5,572** | **📍 reference** |
| 2 | RSI-mode | RSI-14 cross-50 | −238,884 | +800,940 | +3,224 | +$188,427 | 2/3 | 11,122 | −36% |
| 1 | MACD-mode | MACD 12/26/9 cross | −212,546 | +266,900 | +137,964 | +$64,106 | 2/3 | 12,557 | −78% |

### Verdict — REJECT preserved in relative terms

Pre-reg predictions:
| Prediction | Result |
|---|---|
| Both REJECT or marginal | ✓ Confirmed (both lose to baseline) |
| Sign may flip negative→positive due to lower slip | ✓ Confirmed (May 7: REJECTED with negative mean; Today: POSITIVE walk-forward but baseline-loss) |
| Any beats baseline ≥10% | ✗ Refuted (closest is RSI at −36%) |
| 3/3 + within 30% diversifier | ✗ Both 2/3, not 3/3 |

Both signals now walk-forward-PASS at slip=5 (2/3 windows, mean > 0) but lose meaningfully to baseline. The May 7 REJECTED verdicts hold in RELATIVE terms — direction-of-edge vs baseline preserved despite sign change.

### Three notable findings

**1. Cost-model fragility — third example today.** Today found 3 examples of how slip change shifts backtest verdicts:

| Prior test | May 7 verdict (slip=15) | Today's verdict (slip=5) |
|---|---|---|
| vol-1.20 (E1) | SUPPORTIVE | −25% drag vs baseline (lose) |
| trail-1R (B1) | REJECTED | walk-forward POSITIVE but −74% drag (relative REJECT preserved) |
| MACD/RSI (Cat A) | REJECTED | walk-forward POSITIVE but −36% and −78% drag |

Pattern: lower slip lifts EVERYTHING. Sign-flips happen at the margin. Direction-of-edge vs baseline is preserved. **Don't trust ANY backtest finding without re-verification at production cost model.**

**2. Alt signals fire ~2× more often than EMA.**
- MACD: 12,557 trades (vs EMA's 5,572)
- RSI: 11,122 trades

Per-trade NET collapses dramatically:
- EMA: $53/trade ($295k / 5,572)
- RSI: $17/trade ($188k / 11,122)
- MACD: $5/trade ($64k / 12,557)

The mechanism overproduces signals. Edge dilutes across many marginal entries. The strategy structure (6:1 RR + wick stop) doesn't fit the overproductive signal pattern.

**3. W1 is the alt-signals breaking point — NOT W3.**

This is the FIRST sweep today where W3 is NOT the regime-hostile window. Cross-check:
- Baseline W1: +$175k, W3: +$65k
- MACD W1: −$213k, W3: +$138k (HIGHER than baseline)
- RSI W1: −$239k, W3: +$3k

The 2023-2024 (W1) year was hostile to non-EMA signals. The 2025-2026 year (W3) was actually MACD-friendly (it captures the bear regime through MACD line/signal divergence). **Different mechanisms have different regime sensitivities.** EMA is regime-agnostic on this universe; MACD/RSI are 2023-hostile.

This INVERTS the W3-collapse pattern observed in sweeps #2-#6. **The "filter-hostile W3" pattern is specific to ENTRY-FILTER modifications of EMA strategy.** When the entry mechanism is fundamentally different (MACD/RSI), W3 is no longer the breaking point.

### Counter-intuitive observation

Typical TA wisdom puts MACD above RSI for trend-following strategies. Here it's reversed:
- RSI mean: +$188k (−36% vs baseline)
- MACD mean: +$64k (−78% vs baseline)

The strategy structure (6:1 RR + wick stop + short-only crypto altcoins) favors RSI's threshold-crossing over MACD's line/signal logic. Possible mechanism: RSI cross-50 is a SHARP threshold; MACD line/signal cross is a SMOOTHER transition that lags actual momentum reversal. With wick-stops calibrated to recent extreme, sharp transitions fire CLOSER to the actual stop boundary, reducing initial drawdown distance.

### Decision rule applied

Pre-reg outcomes:
1. Lose ≥10% AND fail walk-forward → confirmation, no action
2. Beat baseline ≥10% → CANDIDATE, fresh pre-reg for shadow
3. Within 30% AND 3/3 → diversifier candidate, fresh pre-reg
4. Inconclusive → research finding, no action

Today's result: BOTH cells lose ≥10% BUT pass walk-forward (not in outcome #1 explicitly). Closest match is outcome #4 (inconclusive). **Action: file as research finding. No deployment. No shadow promotion.**

### What this sweep does NOT change

LIVE / shadows / Layer 3 unchanged. Same as all prior sweeps today.

### What this sweep DOES change

- **Confirms** Cat A REJECT direction at new cost model (relative position preserved despite sign flip).
- **Adds** a third cost-model-fragility data point.
- **Documents** the inverted W3 pattern for non-EMA signals (W1 hostile, W3 friendly — opposite of EMA-modification sweeps).
- **Falsifies** the typical TA-wisdom MACD-over-RSI ranking on this strategy.

### Future-session reading note

When a future operator considers "should we add an alternative signal like MACD or RSI?" — refer here. The Cat A 2026-05-07 REJECT verdicts hold in relative terms at current cost. Bb20 (Bollinger) is the only Cat A signal that survived; it's already a shadow. MACD and RSI are not shadow candidates absent fundamental mechanism change.

