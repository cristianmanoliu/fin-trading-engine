# BTC-Anchored Regime Gate — Backtest Harness (Design)

- **Date:** 2026-06-07
- **Status:** Design approved; pre-registered before any run.
- **Scope:** Research-only backtest. **No live / shadow / VPS change** is in scope. Any
  promotion is a separate decision after the result is read.
- **Author:** Cristian Manoliu (w/ Claude, model claude-opus-4-8)

## 1. Problem & Motivation

Today's strategy is `--side-filter short` (shorts only), hardcoded. The 2026-06-07
analysis (`results/side_filter_short_long_both_2026-06-07.txt`,
`results/regime_analysis_2026-06-07.txt`) established two facts:

1. The EMA9×21 cross has **no entry-selection skill** — SHORT-only and LONG-only are
   near-perfect mirrors year by year, so the indicator carries no win/lose information
   about individual trades. The only "alpha" is the *direction flag*.
2. P&L correlates with **alt-market breadth** (`r(%alts-down, profit) = +0.72`): the
   strategy is a *short on the altcoin market*, winning in bear/crash years (2022, 2025,
   choppy 2024) and losing every alt-bull year (2020, 2021, 2023). It structurally
   produces **consecutive losing years** (2020+2021), failing the operator's
   "no consecutive losing years" bar.

If the only alpha is direction, and direction is a slow market-regime property, then the
intervention worth testing is **making the direction flag time-varying, driven by the
market regime** — rather than tuning the (skill-less) entry. This spec defines a
backtest harness to test exactly that, with the overfit guards the repo's history
demands (Option C fee-illusion, the 2026-05-19 perturbation arc, `RESEARCH_BACKLOG.md`).

## 2. Hypothesis (pre-registered)

> Conditioning trade direction on a BTC momentum anchor — SHORT when BTC has fallen,
> LONG when BTC has risen, FLAT otherwise — beats today's hardcoded always-short
> **out-of-sample**, and produces an OOS year sequence with **no two adjacent losing
> years**.

The null (expected by default, given the findings cast doubt on per-trade skill): the
gate adds nothing over always-short OOS, and/or its parameter picks are unstable across
folds — i.e. another beta trade dressed up as timing.

## 3. Locked Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Validation | **Expanding-window walk-forward, OOS** | Only honest read of a 3-knob sweep on ~6 episodes; the OOS column is the only number believed. |
| Gate action | **Flip side: SHORT ↔ LONG ↔ FLAT** | Faithful to "activate short mode / long mode"; tests the time-varying direction flag directly. |
| Anchor | **BTC first, breadth-swappable** | BTC is one clean series matching the mental model; labeler consumes a generic `anchor.csv` so an alt-breadth index is a drop-in anchor #2. |
| Implementation | **Orchestration layer (Approach A)** | Zero `pkg/`/`cmd/` changes; reuses the trusted `bin/backtest` + live cost stack, like `per_year_cohort_backtest.sh`. Rejected: in-engine time-varying filter (Approach B — production-code risk, deferred to a *follow-up if green*); pure-Python vectorized P&L (Approach C — a second P&L model that won't match the engine; rejected per the `--exact-fills` divergence scars). |
| Window | **Expanding** (anchor at 2020), not rolling | More fit data per fold; user-confirmed. |

## 4. Architecture

Three independently testable units + a walk-forward wrapper, all in `tasks/`
(working-tree, not committed source). No changes to `pkg/` or `cmd/`.

```
btc_anchor_build.sh   →  data/anchor/BTCUSDT-1d.csv   (date,open,high,low,close)
                          PRIMARY: aggregate local BTCUSDT-1m-*.csv → daily (offline,
                          reproducible; BTC 1m IS present in the symlinked data store).
                          FALLBACK: fapi /v1/klines 1d if a month is missing locally.
        │
regime_label.py       →  date,label   label ∈ {SHORT, LONG, FLAT}
  pure fn: (X,Y,Z, anchor.csv) → causal timeline
        │
regime_gate_backtest.sh
  per symbol: split 1m CSV at episode boundaries; run bin/backtest
  --side-filter <label> per episode (FLAT skipped); sum P&L
        │
walk_forward.py       →  tasks/regime_gate_results/{grid_<fold>.csv, walk_forward.csv, summary.txt}
  expanding-window fit/score; OOS vs always-short baseline
```

### Unit contracts

- **`btc_anchor_build.sh`** — aggregates local `BTCUSDT-1m-*.csv` into daily OHLC
  (`data/anchor/BTCUSDT-1d.csv`); API fetch only as fallback for a missing month.
  Idempotent.
- **`regime_label.py`** — input `(X, Y, Z, anchor.csv)`; output `date,label` timeline.
  No engine, no I/O beyond the anchor file. Unit-testable on a synthetic BTC series.
- **`regime_gate_backtest.sh`** — input: a label timeline + symbol universe; output:
  per-symbol per-episode P&L rows. Reuses the exact binary + cost flags as
  `per_year_cohort_backtest.sh`.
- **`walk_forward.py`** — input: the grid of per-(X,Y,Z) annual P&L; output: fit-on-early
  / score-on-unseen folds with in-sample vs OOS columns + baseline + stability.
- **Anchor abstraction** — realized as a *file-format contract* (`anchor.csv` =
  `date,close`), not code branching. Swapping BTC → alt-breadth = produce a different
  `anchor.csv`; labeler + driver unchanged.

## 5. Data Flow & Look-Ahead Discipline

**Causal labeling (no look-ahead in the gate).** Label for day *t* uses only BTC closes
through *t*:
- `SHORT` if BTC trailing return over **Y** days ≤ **−X%**
- `LONG`  if BTC trailing return over **Z** days ≥ **+X%**
- `FLAT`  otherwise
- Tie-break (both fire): **SHORT wins** (documented; rare, only when Y≠Z windows
  disagree on a whipsaw). A live engine knows exactly this much on day *t* → timeline is
  reproducible in real time.

Trades within an episode are filtered by the label **already active when the episode
began**. Episodes are never relabeled by outcome.

**Walk-forward (no look-ahead in parameter choice):**

| Fold | Fit (X,Y,Z) on | Score on (unseen) |
|---|---|---|
| 1 | 2020–2022 | **2023** |
| 2 | 2020–2023 | **2024** |
| 3 | 2020–2024 | **2025** |
| 4 | 2020–2025 | **2026 (Jan–Apr)** |

Per fold: grid-search (X,Y,Z) to maximize P&L **on the fit years only**, freeze it,
record what the frozen combo earns on the one unseen year. The concatenation of the OOS
column (2023+2024+2025+2026) is the believed number. In-sample reported alongside to
show the inflation gap. (Note: 2022 — the +$640k carry year — is in the *fit* window of
every fold, so it cannot inflate the OOS total. Happy accident of the timeline.)

**Baseline.** Every fold scored against today's strategy: `--side-filter short`
always-on, same years, universe, costs. The gate must **beat hardcoded-short OOS**, not
merely be positive.

**Boundary force-close.** Regime flip mid-position → episode split force-closes at the
boundary price. Faithful (the live gate would flatten on flip too) and applied
identically to all combos + baseline, so it cannot bias the comparison — same reasoning
as the existing per-year bucketing.

**Cost stack frozen to live:** `--fee-bps 10 --stop-slippage-bps 5 --target-rr 6.0
--max-hold-hours 504 --funding-csv-dir data/funding --exact-fills --include-boundary`,
**full-57 universe** (no selection look-ahead). Identical to registered sweeps.

## 6. Parameter Grid

Deliberately coarse — fewer cells = fewer degrees of freedom; episodes are few.

- **X (threshold %):** 5, 10, 15, 20
- **Y (short lookback, days):** 7, 14, 30
- **Z (long lookback, days):** 7, 14, 30

4 × 3 × 3 = **36 combos/fold × 4 folds = 144 full-57 engine sweeps**. Each full-57
year-run is what `per_year_cohort_backtest.sh` already does in minutes → tractable
(< ~1h expected). Refine a sub-region **only** if walk-forward shows a stable plateau
there (explore → overfit-filter → confirm loop).

## 7. Outputs

All to `tasks/regime_gate_results/` (working-tree; not committed until reviewed):

- `grid_<fold>.csv` — every (X,Y,Z) combo's fit-year P&L per fold
- `walk_forward.csv` — frozen pick per fold + its OOS P&L + always-short baseline OOS
- `summary.txt` — verdict table: OOS-gate vs OOS-baseline per fold, concatenated OOS
  total, parameter stability across folds, per-year win/loss direction

## 8. Success Criteria (pre-registered)

All three must pass for a POSITIVE verdict:

1. **Beat baseline OOS:** concatenated OOS gate P&L **>** concatenated OOS always-short
   P&L, reported per-fold so no single year carries it.
2. **No consecutive losing years (the real bar):** the OOS sequence
   (2023, 2024, 2025, 2026) has **no two adjacent losing years**. Binary, unfakeable.
3. **Parameter stability:** the frozen (X,Y,Z) picks across the 4 folds are near each
   other. Wildly different picks per fold ⇒ the edge is noise ⇒ **reject**, regardless of
   #1 and #2.

## 9. Verdict Logic

- **All three pass** → write a registered finding (add to `results/INDEX.md`), *then*
  separately discuss building Approach B (in-engine time-varying filter) toward a shadow.
  **No live/shadow/VPS change in this work.**
- **Any fail** → write the **negative finding** (closes the regime-gate question the way
  `RESEARCH_BACKLOG.md` closed the strategy-class question) and stop. A negative result is
  a real result.

## 10. Honesty Notes (written before running)

- Only **4 OOS folds** with **3 knobs** — even walk-forward can flatter. Hence the
  hard **stability gate** (#3): parameter instability across folds is itself evidence of
  noise, independent of the OOS sum.
- BTC is a **noisy proxy** for the actual driver (alt breadth, r=+0.72). 2025 is the
  cautionary case: BTC ≈ flat (−6%) while alts crashed (−61%) — a +$469k short year a BTC
  trigger may partly miss. The breadth-swappable anchor exists to measure this cost as
  anchor #2 **if** BTC v1 is promising.
- This harness is **descriptive research**, not a registered edge claim until the
  positive verdict + finding are written.

## 11. Non-Goals (YAGNI)

- No per-trade entry classifier (the findings say the per-trade signal is information-
  less; the regime layer is where the signal lives).
- No production/engine code changes (Approach B deferred; C rejected).
- No new symbols, no forward-paper change, no drift-protocol interaction. The 06-07
  drift KILL was already adjudicated HOLD via the cross-check 9 censoring gate
  (`results/drift_firings/2026-05-31-crosscheck9-precheck.md`); this work is orthogonal.
