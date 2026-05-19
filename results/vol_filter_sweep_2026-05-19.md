# Volatility regime filter sweep — research pre-registration (2026-05-19, sweep #3)

**Status:** LOCKED 2026-05-19 BEFORE sweep execution. Pure research, NOT a deployment decision. Identical discipline framing as sweeps #1 and #2 today.

## Context

Sweep #1 explored EMA × timeframe variants (1 cell beats LIVE +34%, hypothesis-strength). Sweep #2 cleanly refuted 1D bias confluence as an entry filter (all 4 variants lose −23% to −59%). Sweep #3 closes the entry-filter arc with the second-most-popular discretionary filter: **realized-volatility regime gating.**

The harness has `VOL_FILTER_MODE=1` with `MAX_VOL` threshold (annualized, default 1.20). Implementation per `pkg/strategy/entry.go`: rolling 180 4H log returns, annualized via `sqrt(6×365)`. At signal time, if 30-day realized vol exceeds `MAX_VOL`, the entry is rejected.

### Prior testing — why this sweep is still novel

A single-point E1 test exists at `results/walk_forward_4H_short_6w_e1_volfilter120_2026-05-07.txt`:
- ONE threshold tested (vol=1.20)
- Cost model was slip=15bp (LIVE today is slip=5bp)
- 6-window walk-forward (different windows than my 3-window default)
- Verdict was SUPPORTIVE but with regime variance

This sweep is novel because:
1. Tests 5 threshold levels, not 1 — maps the threshold curve
2. Uses CURRENT cost model (slip=5)
3. Uses standard 3-window walk-forward for direct comparability with sweeps #1 + #2 today

## What this sweep does

6-cell walk-forward grid: baseline (no filter) + 5 vol thresholds.

## What this sweep does NOT do

(Verbatim discipline framing from sweeps #1 + #2.)

- Does NOT deploy. Does NOT inform STAGE_1. Does NOT iterate. Does NOT change deployed symbols, shadows, Layer 3, or anything operational.

## The 6-cell grid (LOCKED)

| # | Role | Vol filter | MAX_VOL | Rationale |
|---|---|:---:|---:|---|
| 0 | BASELINE | OFF | — | LIVE config (no filter). Anchors grid. |
| 1 | vol-tight | ON | 0.80 | Aggressive filter — only "calm" 80%-annualized entries. Tests "low vol = cleaner trends" hypothesis. |
| 2 | vol-1.00 | ON | 1.00 | 100% annualized = typical crypto baseline. Mid threshold. |
| 3 | vol-1.20 | ON | 1.20 | Harness default + matches prior E1 test for cross-comparison. |
| 4 | vol-1.50 | ON | 1.50 | Loose — only filters notably-high vol periods (regime crashes). |
| 5 | vol-2.00 | ON | 2.00 | Very loose — only filters extreme outliers. Tests "tail-cut" hypothesis without removing typical regime moves. |

Constants — match LIVE production:
- Signal TF 4H, EMA 9/21, side-filter short, target-RR 6.0
- fee_bps 10, slip_bps 5, mh504, funding=CSV
- Universe: 57 symbols
- 3 walk-forward windows (W1/W2/W3 2023-05 → 2026-04)

## Pre-registered predictions

To prevent post-hoc rationalization, stating predictions before seeing results:

**Strong prior**: I expect the result to mirror sweep #2 — every vol filter LOSES vs baseline. Reason: shorts on crypto altcoins specifically capture liquidation cascades + sustained drawdowns, which are by definition HIGH-vol events. Filtering out high vol filters out the strategy's main alpha source.

**Weak prior**: there's a possibility tight thresholds (cell 1, vol=0.80) might survive by capturing only "controlled descents" without whipsaws. Unlikely on crypto but not impossible.

**Falsifiable claim**: if ANY cell beats baseline by >20% across ≥2/3 windows, my prior is wrong and vol filtering captures real signal.

## Statistical caveats

5 variants × 3 windows = 15 measurements. FWER under null ≈ 1 − 0.95^5 ≈ 23%. Smaller than sweep #1 but similar to sweep #2. Hypothesis-strength only.

## Decision rule

Per-cell verdict (walk_forward.sh semantics): POSITIVE if NET > 0 in ≥2/3 AND mean > 0.

Grid-level: No finding triggers any action. No new shadows. No follow-up sweeps without fresh pre-reg.

## Output

- CSV: `results/vol_filter_sweep_2026-05-19.csv`
- Markdown table appended to this doc after sweep
- Telegram notification

---

## Results — completed 2026-05-19 13:22 UTC

Sweep wall-clock: ~3.5 minutes. 18 measurements (6 cells × 3 windows).

### Ranked by mean NET

| Cell | Role | Vol filter | MAX_VOL | W1 NET | W2 NET | W3 NET | Mean NET | Wins | Trades | vs Baseline |
|---:|:---|:---:|---:|---:|---:|---:|---:|:---:|---:|:---:|
| 4 | vol-1.50 | ON | 1.50 | +196,339 | +638,516 | +68,985 | **+$301,280** | 3/3 | 5,348 | **+2%** ★ |
| 5 | vol-2.00 | ON | 2.00 | +184,616 | +638,173 | +65,584 | +$296,124 | 3/3 | 5,530 | −0.3% |
| **0** | **BASELINE (LIVE)** | **OFF** | **—** | **+175,021** | **+645,812** | **+65,044** | **+$295,292** | **3/3** | **5,572** | **📍 reference** |
| 3 | vol-1.20 | ON | 1.20 | +155,300 | +466,374 | +38,576 | +$220,083 | 3/3 | 4,702 | −25% |
| 2 | vol-1.00 | ON | 1.00 | +110,551 | +239,392 | −26,739 | +$107,735 | 2/3 | 3,883 | −63% |
| 1 | vol-tight-0.80 | ON | 0.80 | +127,922 | +120,106 | −24,590 | +$74,479 | 2/3 | 2,456 | −75% |

### Verdict — partial refutation of pre-registered prediction

The pre-reg locked the prediction: "Every vol filter LOSES vs baseline." That was **half wrong**. Tight thresholds (0.80, 1.00, 1.20) lose −25% to −75%, confirming the prior. Loose thresholds (1.50, 2.00) statistically tie baseline (+2% / −0.3% — both noise at these magnitudes).

### Four findings

1. **Clean monotonic relationship.** Cleanest signal of today's three sweeps. As MAX_VOL loosens from 0.80 → 2.00, mean NET moves monotonically: −75% → −63% → −25% → +2% → −0.3% (with cell 5 essentially tied with cell 4 because both barely filter anything). The relationship is mechanical, not noise.

2. **Loose filters are operationally null.** vol=1.50 rejects ~4% of trades (5,572 → 5,348); vol=2.00 rejects ~1% (5,572 → 5,530). Both are essentially "no filter" with extra CPU. Cell 4's nominal +2% is well within window-to-window noise variance.

3. **The 1.20 May-7 SUPPORTIVE finding does NOT replicate at current cost model.** Critical robustness lesson.
   - May 7 E1 test: slip=15bp, 6 walk-forward windows → vol=1.20 was SUPPORTIVE (mean +$88k/window, 4/6 positive)
   - Today (sweep #3 cell 3): slip=5bp, 3 walk-forward windows → vol=1.20 is −25% vs baseline (mean +$220k vs baseline +$295k)
   - **Same parameter, different cost+window assumptions, opposite verdict.** This is exactly the fee-illusion class of trap that Option C represented at 2026-05-05. Backtest "findings" are extremely sensitive to cost-model assumptions.

4. **W3 collapse pattern returns.** Tight filters (cells 1, 2) turn W3 negative. Same as the 1D confluence sweep #2 showed. The 2025-2026 year contains regime transitions where structural entry filters systematically disagree with reality and remove valid alpha. This W3-collapse pattern is now observed across TWO independent filter mechanisms (1D bias + vol regime) — strong evidence the 2025-2026 year is filter-hostile.

### Mechanism interpretation

Crypto altcoin shorts capture liquidation cascades, which are by definition high-vol events. The math is direct:

- Tight vol filter (vol < 1.20) REJECTS most cascade-triggered entries. PnL collapses because cascades are the strategy's main alpha source.
- Loose vol filter (vol > 1.50) only rejects extreme outliers (~1-4% of trades). These extreme entries are a mixed bag — some are explosive winners (continued cascades), some are extreme reversals (top-of-vol regime turns). The tail is a wash; filtering it changes nothing.

The strategy is BROADLY INSENSITIVE to LIGHT filtering but ALLERGIC TO AGGRESSIVE filtering.

### Cross-sweep summary (all three of 2026-05-19)

| Sweep | Hypothesis | Cells | Verdict | Magnitude |
|---|---|---:|---|---|
| #1 (EMA × TF) | Some param combo beats LIVE | 11 | 1/10 variants nominally beats | +34% (1 cell) |
| #2 (1D confluence) | Multi-TF agreement filter helps | 5 | All 4 variants lose | −23% to −59% |
| #3 (vol regime) | Skip high-vol entries helps | 6 | Tight loses, loose ties | −75% to +2% |

**Pattern**: parameter exploration occasionally finds nominally-bigger numbers (sweep #1 cell 4); structural filters CONSISTENTLY fail or barely-help on this strategy + universe (sweeps #2 + #3). The strategy is structurally robust to small parameter changes but fragile to filter stacking.

### What this sweep does NOT change

(Same as sweeps #1 + #2.)

- LIVE cohort unchanged
- 3 shadows unchanged
- Layer 3 wrap unchanged
- All dashboards / crons / pre-flight unchanged

### What this sweep DOES change

- **Falsifies** the "vol regime filter improves entries" hypothesis at every threshold except the trivially-null ones (vol≥1.50).
- **Refutes the May 7 E1 SUPPORTIVE finding** at current cost model. Documents the fragility lesson: backtest verdicts can flip when cost model changes.
- Adds a third clean research artifact to the day's record.

### Future-session note

When a future operator considers "wouldn't filtering out high-vol entries reduce drawdowns?" — refer here. The mechanism is documented. The hypothesis was tested at 5 threshold levels. Loose filters do nothing; tight filters destroy alpha. Don't re-run unless the strategy mechanics themselves change.

**The W3-collapse pattern across both filter sweeps (#2 + #3) is a signal worth flagging**: any future entry filter tested against this universe at current cost model should be expected to underperform in the 2025-2026 walk-forward window unless explicitly designed for regime transitions.

