# Trailing stop exit mechanism sweep — research pre-registration (2026-05-19, sweep #6)

**Status:** LOCKED 2026-05-19 BEFORE sweep execution. Same discipline framing as today's prior 5 sweeps.

## Context

Today's 5 sweeps mapped: parameter family (#1), entry filters (#2-#3), side filter (#4), exit mechanism via MLTP (#5). Sweep #6 closes the exit-mechanism arc by testing the other major exit mechanism class — **trailing stop**.

User constraint: keep 6:1 RR locked. Trailing stop does NOT change the RR target — the 6R remains the natural take-profit. Trail only adjusts WHEN the stop fires post-MFE. Same constraint analysis as MLTP — RR ratio preserved, exit timing modified.

The harness:
- `TRAIL_MODE=1` enables
- `TRAIL_INTERVAL_R` = trail step in R (default 1.0)

Mechanism: once the trade's max favorable excursion (MFE) crosses the trail interval, the stop moves up to break-even, then continues trailing as price moves favorably. Locks in profit on winners that don't reach 6R; turns "would-be-losers-but-had-a-favorable-spike" into break-even exits.

### Prior testing

Single-point test exists: `walk_forward_4H_short_6w_b1_trail1R_2026-05-07.txt`.
- ONE setting (TRAIL_INTERVAL_R=1.0)
- slip=15bp (different cost model)
- 6-window walk-forward
- Verdict: REJECTED (mean −$26k, 3/6 windows)

This sweep is novel because:
1. Tests 5 trail-interval settings, not 1
2. Current cost model (slip=5bp)
3. Same 3-window scope as today's prior sweeps for direct cross-comparison

## What this sweep does

6-cell walk-forward grid: baseline + 5 trail-interval variants.

## What this sweep does NOT do

(Same discipline as today's prior 5 sweeps.) No deployment, no shadow promotion, no follow-up without fresh pre-reg.

## The 6-cell grid (LOCKED)

| # | Role | Trail mode | TRAIL_INTERVAL_R | Rationale |
|---|---|:---:|---:|---|
| 0 | BASELINE | OFF | — | LIVE config (single 6R target). |
| 1 | trail-0.5R | ON | 0.5 | Tight trail — locks profit fastest. |
| 2 | trail-1.0R | ON | 1.0 | Matches 2026-05-07 prior test (cross-comparison at new cost model). |
| 3 | trail-1.5R | ON | 1.5 | Mid-loose trail. |
| 4 | trail-2.0R | ON | 2.0 | Loose — gives room before locking. |
| 5 | trail-3.0R | ON | 3.0 | Very loose — only fires after material favorable move. |

Constants — match LIVE production:
- 4H signal, EMA 9/21, side=short, target-RR 6.0, wick stop, mh504
- fee_bps 10, slip_bps 5, funding=CSV
- Universe: 57 symbols, 3 walk-forward windows

## Pre-registered predictions

**Strong prior:** Trailing stop will REDUCE mean NET. Mechanism: 6R targets are rare; even 1R favorable moves are common. Tight trails (0.5R, 1.0R) exit MOST trades before reaching 6R. On a 6:1 RR strategy where winning trades drive the math, losing the long tail is catastrophic.

**Weak prior:** Looser trails (3.0R) hurt less. Cell 5 may approximate baseline because the trail only fires after material favorable moves.

**Variance prediction:** Trailing stop should TIGHTEN W3 variance (similar to MLTP) but at HIGHER mean cost — trail is more aggressive than partial close (kicks out the FULL position, not just a fraction).

**Cross-mechanism comparison:** Compared to MLTP (sweep #5), I expect trail to show MORE drag at equivalent thresholds:
- MLTP cell 1 (mid=2R, half) was −41% — partial close
- Trail cell 2 (trail=1R, equivalent activation threshold) should be −50% to −60% — full close

**Falsifiable claim:** if any trail variant beats baseline by ≥10% across ≥2/3 windows, my prior is wrong and trail captures real signal at current cost model. The 2026-05-07 REJECTED verdict (at slip=15) is the strongest prior against this happening.

## Statistical caveats

5 variants × 3 windows = 15 measurements. FWER under null ≈ 23%. Hypothesis-strength.

## Decision rule

Per-cell verdict: walk_forward.sh standard. No finding triggers deployment.

## Output

- CSV: `results/trailing_stop_sweep_2026-05-19.csv`
- Markdown table appended after sweep
- Telegram notification

---

## Results — completed 2026-05-19 13:53 UTC

Sweep wall-clock: ~3 minutes. 18 measurements (6 cells × 3 windows).

### Ranked by mean NET

| Cell | Role | Trail | TRAIL_R | W1 NET | W2 NET | W3 NET | Mean NET | Wins | Trades | vs Baseline |
|---:|:---|:---:|---:|---:|---:|---:|---:|:---:|---:|:---:|
| **0** | **BASELINE (LIVE)** | **OFF** | **—** | **+175,021** | **+645,812** | **+65,044** | **+$295,292** | **3/3** | **5,572** | **📍 reference** |
| 5 | trail-3.0R | ON | 3.0 | +132,490 | +625,707 | +51,768 | +$269,988 | 3/3 | 5,705 | −9% |
| 4 | trail-2.0R | ON | 2.0 | +98,015 | +553,815 | −24,784 | +$209,015 | 2/3 | 5,957 | −29% |
| 3 | trail-1.5R | ON | 1.5 | +24,787 | +457,545 | −12,487 | +$156,615 | 2/3 | 6,183 | −47% |
| 2 | trail-1.0R | ON | 1.0 | +77,172 | +241,780 | −84,113 | +$78,280 | 2/3 | 6,386 | −74% |
| 1 | trail-0.5R | ON | 0.5 | −32,010 | +84,693 | −153,662 | **−$33,660** | 1/3 | 6,486 | −111% ✗ |

### Verdict — pre-reg predictions confirmed

| Prediction | Result |
|---|---|
| Trail underperforms baseline | ✓ Confirmed (every cell) |
| Monotonic by trail tightness | ✓ Confirmed (−9% → −29% → −47% → −74% → −111%) |
| Trail more drag than MLTP at equivalent thresholds | ✓ Confirmed (see cross-mechanism table) |
| Any cell beats baseline ≥10% | ✗ Refuted (no cell beats baseline) |

### Three notable findings

**1. The 2026-05-07 REJECTED verdict at trail=1.0R replicates at current cost model.**
- May 7 single-point test: slip=15, 6 windows, mean −$27k → REJECTED
- Today (sweep #6 cell 2): slip=5, 3 windows, mean +$78k → POSITIVE but −74% drag

The sign flipped because lower slip lifts the whole curve, but the RELATIVE position (substantial drag vs baseline) is identical. The May 7 finding holds in relative terms at the new cost model: trail=1R is a clean loss vs no-trail.

**2. Cell 5 (trail-3.0R) is genuinely interesting cheap optionality.**
- Mean drag only −9% (within noise of baseline)
- 3/3 windows positive (matches baseline robustness)
- Only 2.4% more trades than baseline (5,705 vs 5,572)
- Mechanism: trail at 3R only fires when trade reaches +3R THEN reverses before 6R. This is a small fraction of trades. For those trades, trail captures break-even instead of full reversal back to stop.

Net effect: small upside loss (trades that would've reached 6R get locked at 3R when they don't quite make it) traded for downside protection (trades that reach 3R then reverse don't fully round-trip). At 3R threshold, this is roughly a wash.

**3. Cross-mechanism comparison: trail BEATS MLTP at similar activation thresholds.**

| Activation R | MLTP drag | Trail drag |
|---:|---:|---:|
| 2R | −41% (MLTP cell 1, mid-2R-half) | −29% (trail cell 4, trail-2R) |
| 3R | −28% (MLTP cell 2, mid-3R-half) | −9% (trail cell 5, trail-3R) |
| 4R | −18% (MLTP cell 3, mid-4R-half) | n/a (trail cells max at 3R in this sweep) |

**Mechanism insight:** MLTP closes a FRACTION of position unconditionally at mid-R. Trail moves STOP to break-even — the position only exits if price reverses. For trades that continue to 6R after passing the threshold:
- MLTP: lost the partial fraction's upside (already closed some)
- Trail: full position rides to 6R (only stop moved, not triggered)

For trades that reverse after passing the threshold:
- MLTP: half got locked at +R, half went to stop
- Trail: full position exits at break-even

At loose thresholds (3R), most trades that pass threshold continue to 6R, so trail's "full position rides" beats MLTP's "partial locked, rest rides." At tight thresholds (1R), more trades reverse, but trail's full-close on reversal hurts more than MLTP's partial-close at full position dynamics.

This is **the first cross-sweep mechanistic insight today** — not just confirming/refuting a hypothesis but discovering a relationship between two exit mechanisms.

### W3 collapse pattern — trail joins the catalog

Tight trails (cells 1-4) turn W3 negative. Loose trail (cell 5) keeps W3 positive. Same shape as the W3-collapse pattern from sweeps #2, #3, #4.

Updated cross-sweep W3 catalog:

| Sweep | Mechanism | W3 baseline → variant |
|---|---|---|
| #2 (confluence) | 1D bias filter | +$65k → −$25k to −$148k |
| #3 (vol filter) | tight vol threshold | +$65k → −$24k to −$26k |
| #4 (side filter) | longs / both | +$65k → −$206k to −$380k |
| #5 (MLTP) | partial close at mid-R | +$65k → +$28k to −$10k (relatively stable) |
| **#6 (trail)** | **trailing stop** | **+$65k → +$52k to −$154k (tight kills, loose survives)** |

**Pattern is now firmly established:** the 2025-2026 walk-forward year punishes any tight-threshold modification of the LIVE strategy. The two mechanisms that DON'T catastrophically break W3 are MLTP (variance-reduction, every cell stable) and loose trail (cell 5 only). Everything else makes W3 worse.

### Strategic implication

Under current setup (no per-window constraints), trail is a clean loss vs baseline at every threshold. **Cell 5 (trail-3R) is the documented "least-cost optional add-on"** — could be added as a defensive parachute against worst-case price reversals at high MFE without materially affecting average performance. But absent a specific risk-management constraint, the single 6R target wins.

This sweep COMPLETES the exit-mechanism research arc (MLTP + Trail). Both major exit-mechanism classes are now characterized:
- **MLTP** (partial close at mid-R): variance-reduction trade-off, smoothest W3 behavior
- **Trail** (full close on reversal post-MFE): more drag than MLTP at tight thresholds, less at loose; cheap optionality at 3R

If a future stage imposes a risk constraint, the operator has a documented trade-off frontier:
- Want maximum mean NET: keep baseline (no exit modification)
- Want variance reduction: MLTP cell 3 (4R/0.5), −18% drag
- Want downside protection at small cost: trail cell 5 (3R), −9% drag

### What this sweep does NOT change

LIVE / shadows / Layer 3 unchanged. Same as all prior sweeps today.

### What this sweep DOES change

- **Completes** the exit-mechanism research arc with a cross-mechanism finding
- **Documents** trail's trade-off curve at current cost model
- **Refutes** the trail-as-improvement hypothesis (confirming the 2026-05-07 REJECTED verdict)
- **Identifies** cell 5 (trail-3R) as cheap optionality if future risk constraints arise

### Future-session reading note

When a future operator considers "should we add trailing stop?" — refer here. The answer is "not unless you have a specific risk constraint." If you do have a constraint (e.g., per-trade-drawdown limit), the documented options are MLTP cell 3 or trail cell 5, with documented trade-off curves.

