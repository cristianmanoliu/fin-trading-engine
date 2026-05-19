# 1D bias confluence sweep — research pre-registration (2026-05-19, sweep #2)

**Status:** LOCKED 2026-05-19 BEFORE sweep execution. Pure research, NOT a deployment decision. Mirrors the discipline of `ema_tf_exploratory_grid_2026-05-19.md` (sweep #1 today).

## Context

Sweep #1 today mapped the (timeframe × EMA fast × EMA slow) landscape. It found LIVE config robust (3/3 windows positive, mean +$295k) and one variant (4H 5/21) beat it +34% — but as a research finding only, with locked decision rule barring promotion.

Operator authorized a second sweep with the same discipline framing. After reviewing the harness's available filter parameters and prior sweep history, the genuinely-unexplored cell at current cost model (slip=5 mh504 — different from 2026-05-06 sweep's slip=15 mh336) is:

**1D bias confluence as an ENTRY FILTER on top of the LIVE 4H strategy.**

The mechanism: at signal time, check whether the 1D EMA bias agrees with the 4H short signal (i.e., 1D EMA fast also < 1D EMA slow). If 1D bias disagrees (1D in uptrend), skip the trade. Concept is "multi-timeframe agreement" — a hugely popular pattern in professional discretionary trading; the harness has it as `CONFL_1D_MODE=1` but no production sweep has run it at the current cost model.

## What this sweep does

Run a 5-cell walk-forward grid:
- **Cell 0**: BASELINE — no 1D confluence filter (= today's LIVE config)
- **Cells 1-4**: confluence filter ON, with 4 different 1D EMA period pairs

The hypothesis is testable: does requiring 1D bias agreement reduce false 4H entries without removing too many winners?

## What this sweep does NOT do

(All five clauses copied verbatim from sweep #1 pre-reg — the discipline framing is identical.)

- Does NOT deploy any new strategy to live or shadow.
- Does NOT inform any STAGE_1 promotion decision.
- Does NOT iterate. 5 cells, fixed. "Interesting findings" do not unlock follow-up sweeps without fresh pre-reg.
- Does NOT introduce new symbols or any operational change.
- Does NOT use deployed-16-only (full 57-symbol universe).

## The 5-cell grid (LOCKED)

| # | Role | 1D Confl | 1D Fast | 1D Slow | Rationale |
|---|---|:---:|---:|---:|---|
| 0 | BASELINE | OFF | — | — | LIVE config (= no filter). Anchors the grid. |
| 1 | match-live | ON | 9 | 21 | Same EMA periods as the 4H signal — direct cross-TF consistency. |
| 2 | faster-bias | ON | 5 | 15 | Faster 1D bias = more responsive but noisier filter. |
| 3 | slower-bias | ON | 21 | 50 | Slower 1D bias = more conservative, fewer entries. |
| 4 | fib-bias | ON | 13 | 34 | Fibonacci pair, mid-tempo 1D filter. |

Constants:
- 4H signal timeframe (matches LIVE)
- EMA 9/21 on 4H (matches LIVE)
- target-RR 6.0, side-filter short, wick stop, mh504
- fee_bps 10, slip_bps 5 (matches LIVE current cost model — distinct from prior sweep's 15bp)
- Universe: 57 symbols

## Walk-forward windows (LOCKED)

3 non-overlapping 12-month chunks (matches harness default):
- W1 2023-05 → 2024-04
- W2 2024-05 → 2025-04
- W3 2025-05 → 2026-04

## Statistical caveats (LOCKED)

5 cells × 3 windows = 15 measurements. With 4 variants tested, false-discovery odds under null ≈ 1 - (0.95)^4 ≈ 19% — much lower than sweep #1's 40%. Smaller grid = cleaner statistics. Still hypothesis-strength only.

## Decision rule (LOCKED)

Per-cell verdict (walk_forward.sh semantics):
- POSITIVE if NET > 0 in ≥2/3 windows AND mean NET > 0

Grid-level interpretation:
- All POSITIVE cells filed as research findings.
- Cells that beat baseline NET noted as "interesting."
- **No finding triggers any action.** No shadow creation, no live change, no follow-up sweeps without fresh pre-reg.

## Pre-registration of expected outcomes

To prevent post-hoc rationalization, pre-stating predictions:

- **If confluence helps significantly** (≥2 cells beat baseline by ≥30% across 3/3 windows): would be evidence that multi-timeframe agreement filters real noise. Still doesn't unlock deployment — would inform future hypothesis design.
- **If confluence hurts significantly** (multiple cells lose to baseline): suggests the 4H short signal is already capturing what 1D would catch + the filter is removing real winners during regime transitions.
- **If results are mixed / cluster around baseline**: confluence is parameter-sensitive but offers no robust edge — falsifies "multi-timeframe is always better" intuition.

## Output

1. CSV at `results/confl_1d_bias_sweep_2026-05-19.csv`
2. Markdown table appended to this doc after sweep completes
3. Telegram notification on completion

## Audit note

This is sweep #2 of the day. Discipline test: can we run a second focused sweep without scope creep into "let's also test funding filter" or "what about confluence + cell 4 EMAs together"? The answer must be YES — each sweep is its own pre-reg, fixed scope, no on-the-fly combinations. The previous-sweep findings (cell 4 from sweep #1) are explicitly NOT carried in as a "now combine with confluence" — that would be a fresh pre-reg.

---

## Results — completed 2026-05-19 13:11 UTC

Sweep wall-clock: ~3 minutes. 15 measurements (5 cells × 3 windows).

### Ranked by mean NET (baseline anchored)

| Cell | Role | Confl | 1D EMA | W1 NET | W2 NET | W3 NET | Mean NET | Wins | Trades | vs Baseline |
|---:|:---|:---:|:---:|---:|---:|---:|---:|:---:|---:|:---:|
| **0** | **BASELINE (LIVE)** | **OFF** | **—** | **+175,021** | **+645,812** | **+65,044** | **+295,292** | **3/3** | **5,572** | **📍 reference** |
| 4 | fib-bias | ON | 13/34 | +274,858 | +513,436 | −102,867 | +228,476 | 2/3 | 3,056 | −23% |
| 3 | slower-bias | ON | 21/50 | +172,315 | +556,327 | −148,533 | +193,370 | 2/3 | 3,026 | −35% |
| 1 | match-live | ON | 9/21 | +295,138 | +296,379 | −34,450 | +185,689 | 2/3 | 2,925 | −37% |
| 2 | faster-bias | ON | 5/15 | +234,078 | +155,814 | −25,484 | +121,469 | 2/3 | 2,762 | −59% |

### Findings — clean negative result

**Verdict: hypothesis REFUTED.** Every confluence variant loses mean NET vs baseline. Three converging signals:

1. **Direction uniformity.** All 4 variants worse than baseline by −23% to −59%. Under null, expected variance would produce a mix of better/worse — uniform direction is mechanism, not noise.

2. **W3 collapse pattern.** Baseline W3 (2025-05 → 2026-04) is +$65k. All 4 variants turn W3 negative (−$25k to −$148k). The filter systematically removes winners in the most-recent year. Plausible mechanism: 2025-2026 had regime transitions where 1D bias lagged 4H reality; the filter killed legitimate short signals during those transitions.

3. **Trade-count halves.** ~5,572 → ~2,800-3,000 trades. The filter rejects ~45-50% of entries. For NET to drop this much, those rejected entries must contain MORE net winners than losers — i.e., the filter is removing alpha, not noise.

### Mechanism interpretation

The 4H EMA 9/21 short signal on a crypto-altcoin universe ALREADY captures the regime alignment it needs. The bias is essentially already present in the 4H signal's structure — fast-EMA crossing below slow-EMA in a 4H candle context means recent momentum has turned bearish. Adding a 1D bias filter doesn't strengthen this — it just blocks valid signals fired during regime transitions when 1D EMAs haven't caught up to the new reality.

**Falsifies a popular discretionary-trading heuristic** on this specific strategy + universe + timeframe: "multi-timeframe agreement is always better." On 4H short crypto with 6:1 RR, MTF agreement is a NET drag.

### Statistical robustness

4 variants × 3 windows = 12 variant-measurements. Family-wise false-discovery rate under null ≈ 19%. Said plainly: if every variant truly had the same edge as baseline, we'd expect roughly 1-in-5 chance that ALL 4 would look worse by pure chance — but the magnitudes here (−23% to −59%) are far larger than noise. The negative result is robust.

There is no "cherry pick to dismiss" — when nothing looks good, every variant being worse is itself the evidence.

### Cross-sweep contrast (sweep #1 vs sweep #2)

| Sweep | Hypothesis | Result | Magnitude |
|---|---|---|---|
| #1 (EMA × TF) | "Some EMA/TF combo beats LIVE" | 1 cell (4H 5/21) beats by +34% | hypothesis-strength positive |
| #2 (1D confluence) | "1D filter improves entry quality" | All 4 variants lose | clean refutation |

The pair is intellectually complementary: parameter exploration sometimes finds bigger numbers (sweep #1 cell 4); structural filter stacking on this strategy makes things worse (sweep #2). Both filed as research findings only — neither unlocks deployment per the locked decision rule.

### What this sweep does NOT change

- LIVE cohort: unchanged (the negative confluence result confirms the no-filter baseline)
- 3 shadows: unchanged
- Layer 3 wrap: unchanged
- All dashboards / crons / pre-flight tools: unchanged

### What this sweep DOES change

- **Falsifies** the "1D bias confluence helps 4H short" hypothesis on this universe + strategy.
- Adds a clean negative-result artifact to the research record. (Negative results are valuable: they prevent future operators from re-running this experiment.)

### What this sweep does NOT authorize

- Tweaking the confluence filter further (e.g., MACD bias instead of EMA bias). Would require fresh pre-reg.
- Combining confluence with cell 4 from sweep #1. Same — fresh pre-reg.
- Any change to LIVE or shadows.

### Future-session reading note

When a future operator (or future-me) considers "wouldn't multi-timeframe filtering improve our backtest?" — refer here. The mechanism is documented. The hypothesis was tested at 4 different parameter levels. The answer is consistently no. Don't re-run unless the strategy mechanics themselves change (e.g., different entry signal, different timeframe primary, different universe).

