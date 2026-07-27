# ATR 1.0 Stop Sizing — VERDICT: STRONGEST CANDIDATE FOUND (held-out significant)

**Status:** Research finding, 2026-07-27. **Held-out significant — the first result in this
project's history to clear a genuine out-of-sample significance test.** Still research-only:
no live change, no promotion, no capital.
**Harness:** `scripts/cost_geometry_sweep.sh` + `scripts/overnight_arc_2026-07-27.sh`
**Split:** TRAIN 2020-01 → 2023-12 (selection). TEST 2024-01 → 2025-04 (held out, opened after
the shortlist was fixed).

## Result

`--atr-stop-mult 1.0` (stop = entry ± ATR(14) × 1.0, replacing the wick stop).

### Held-out TEST 2024-01 → 2025-04

| Arm | Trades | WR | Gross | Fees | NET | NET/trade |
|---|---:|---:|---:|---:|---:|---:|
| baseline (wick) | 2,433 | 21.7% | $945,756 | $129,957 | $794,069 | $326.4 |
| **atr1.0** | **2,433** | 22.1% | **$1,214,636** | **$88,829** | **$1,129,924** | **$464.4** |
| atr1.25 | 2,176 | 23.6% | $994,118 | $62,856 | $948,274 | $435.8 |
| atr1.5 (last night) | 1,948 | 26.4% | $909,648 | $46,646 | $887,278 | $455.5 |
| atr1.5-mh336 | 2,130 | 28.0% | $859,906 | $51,824 | $826,273 | $387.9 |

| Arm | Δ NET | t | perm p | beats baseline | median symbol | top-3 share |
|---|---:|---:|---:|---:|---:|---:|
| **atr1.0** | **+$335,855** | **+3.83** | **<0.001** | **36/57** | **+$4,448** | **26%** |
| atr1.25 | +$154,205 | +1.75 | 0.085 | 34/57 | +$2,867 | 44% |
| atr1.5 | +$93,209 | +1.00 | 0.319 | 28/57 | −$808 | 79% |
| atr1.5-mh336 | +$32,204 | +0.35 | 0.722 | 26/57 | −$1,159 | 204% |

**atr1.0 passes every diagnostic that atr1.5 failed.**

## Why this is not the usual noise

Last night's atr1.5 was rejected as noise-grade (p=0.32, median symbol negative, top-3 = 79%).
atr1.0 is structurally different:

| Robustness check | Result |
|---|---|
| Total Δ | +$335,855 |
| Drop top-1 winner | +$299,124 |
| Drop top-3 winners | +$249,680 |
| **Drop top-5 winners** | **+$205,631** |
| Median symbol | **+$4,448** |
| Sign test (36/57) | **p = 0.031** |
| Paired t | **+3.83** |

Not concentration-driven; the *typical* symbol improves.

### Decomposition — this one improves the EDGE, not just cost

| Component | TEST baseline → atr1.0 | Symbols improved |
|---|---|---|
| Fees | $129,957 → $88,829 (**−32%**) | 55/57 |
| **Gross** | $945,756 → **$1,214,636** (**+28%**) | **36/57** |

This is the key difference from atr1.5, where gross was a coin flip (46%). Here gross rises on
a clear majority. **Trade count is identical (2,433 → 2,433)** — the same signals, the same
entries, only the stop placement differs. So the gross uplift is *not* a different strategy
being selected; it is the same strategy with stops that stop getting wicked out.

Mechanism: the wick stop is placed at a recent extreme, which is precisely where noise
clusters. An ATR(14)×1.0 stop is volatility-normalized, so it sits outside routine noise while
staying tight enough to preserve R geometry. It both (a) avoids premature stop-outs → gross up,
and (b) widens the stop modestly → notional down → fees down.

### The optimum is interior, not a boundary artifact

| Mult | Trades | Gross | Fees | NET |
|---|---:|---:|---:|---:|
| 0.5 | 5,765 | $321,648 | $418,426 | **−$221,735** |
| 0.75 | 5,388 | $628,944 | $256,431 | $339,242 |
| **1.0** | 4,904 | $1,004,398 | $171,916 | **$843,341** |
| 1.25 | 4,409 | $799,876 | $121,618 | $708,562 |
| 1.5 | 3,991 | $570,655 | $90,580 | $518,167 |
| 2.0 | 3,461 | $254,094 | $58,499 | $233,347 |
| 3.5 | 2,765 | −$32,109 | $26,191 | −$26,504 |
| 5.0 | 2,458 | −$91,960 | $16,274 | −$79,910 |
| wick (baseline) | 5,014 | $783,785 | $270,278 | $463,687 |

(TRAIN.) A clean single-peaked curve with a well-separated maximum at 1.0 — falling off hard in
both directions. This is what a real parameter optimum looks like, as opposed to the monotone
ramp that would indicate a boundary artifact.

## Other arms tested (TRAIN, all rejected or dominated)

| Arm | NET | vs atr1.5 |
|---|---:|---:|
| atr1.5 + vol-filter | $476,344 | −$41,823 |
| atr1.5 + ATR period 21 | $464,578 | −$53,589 |
| atr1.5 + ATR period 7 | $528,785 | +$10,618 |
| atr1.5 + max-hold 720 | $382,477 | −$135,690 |
| atr1.5 + trailing stop | $329,110 | −$189,057 |

None promoted. Vol-filter is redundant once stops are volatility-normalized (as hypothesized).

## Application to the live book — still not profitable

Applying the measured TEST ratios (gross ×1.28, fees ×0.68) to live's 118 realized trades:

| Scenario | Gross | Fees | Slippage | NET |
|---|---:|---:|---:|---:|
| LIVE actual | +$3,489 | −$7,909 | −$3,329 | **−$7,749** |
| atr1.0, fee effect only | +$3,489 | −$5,406 | −$3,329 | −$5,246 |
| atr1.0, full effect | +$4,481 | −$5,406 | −$2,275 | **−$3,201** |

**0.58× of breakeven** (vs 0.31× today). A large improvement that still does not cross zero on
the live book. The live sample is 118 trades over 3 months on 16 symbols; the backtest ratio is
measured over 2,433 trades on 57 symbols and is not guaranteed to transfer.

## Verdict

**STRONGEST CANDIDATE THE PROJECT HAS PRODUCED — and still not a promotion.**

What is genuinely established:
- Out-of-sample significant (t=3.83, p<0.001) on a holdout fixed before it was opened.
- Robust to dropping the top 5 symbols; the median symbol improves.
- Mechanism is explicable and matches the measured decomposition.
- Same signal, same trade count — a stop-placement change, not a new strategy.

What is NOT established:
- That it rescues the live book (projection: −$3,201, still negative).
- That the +28% gross uplift transfers to the 16-symbol deployed set.
- Anything about live execution: no drift detector, no Layer 2, no Layer 3.

**This arc spent ~13 more trials against the DSR budget** (N ≈ 45 → ~58), raising the
luck-bar to ~1.22 annualized. atr1.0's TEST Sharpe should be computed against that bar before
any promotion argument is made — that calculation is NOT yet done and is the first task for
the next session.

## Recommended next step (not taken)

Deploy `--atr-stop-mult 1.0` as a **9th shadow engine**. Zero incremental REST weight, zero
forward-paper degrees of freedom, and it starts accruing genuine out-of-sample live evidence
immediately. This matches the operator's stated plan: shadow anything promising for months
before any capital.

**Do not alter the live config. The ~2026-08-25 forward-paper verdict is unaffected** and must
resolve on the config that has been running since 2026-05-05.

## Reproduction

```bash
bash scripts/overnight_arc_2026-07-27.sh          # TRAIN arms
VARIANT=atr1.0 EXTRA="--atr-stop-mult 1.0" START_YEAR=2024 END_YEAR=2025 END_YEAR_MONTH=04 \
  bash scripts/cost_geometry_sweep.sh results/oa_test/atr1.0.txt
```

Raw per-symbol outputs: `results/oa_train/`, `results/oa_test/`.

## Cross-references

- `results/atr_stop_cost_geometry_verdict_2026-07-26.md` — the atr1.5 PARTIAL that led here.
- `results/stop_distance_cost_filter_verdict_2026-07-26.md` — live cost decomposition.
- `results/backtest_overfit_pbo_dsr_verdict_2026-05-29.md` — the DSR budget this spends.
