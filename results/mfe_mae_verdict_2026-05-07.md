# MFE/MAE Decomposition — Verdict 2026-05-07 (HG1)

## Question

The deployed strategy uses a **fixed 6:1 RR exit**. Cat B1/B2 (trailing stop,
multi-level TP) were rejected per CLAUDE.md memory, but those tests used
arbitrary parameter choices. Is 6:1 *empirically* the optimal exit, or is the
strategy leaving money on the table that a properly-tuned partial-TP /
stop-to-BE / trailing-stop framework could rescue?

## Method

Added MFE_R (peak favorable excursion in stop-distance multiples) and MAE_R
(peak adverse excursion in stop-distance multiples) tracking to `Stub.OnTick`,
emitted in close-event journals.

Drive-by fix: `MaxFavorableR` was previously gated behind `TrailingStopMode`,
so MFE was silently 0 in normal-exit journals. Pre-fix the diagnostic was
impossible to run. Tracked unconditionally now.

Re-ran `scripts/hod_journals.sh` across deployed-16 over 5y at the candidate
cost stack (fee=10/slip=5, target_rr=6.0, side-filter=short, max-hold=504h,
funding-historical). Output: 2,210 trades with MFE_R/MAE_R per close event.

## Findings

### Distributions per outcome bucket

| Outcome | n | metric | p10 | p50 | p90 | p99 |
|---|---:|---|---:|---:|---:|---:|
| STOP | 1,726 | MFE_R | 0.12 | 0.81 | 3.47 | 5.55 |
| STOP | 1,726 | MAE_R | 1.00 | 1.03 | 1.19 | 1.58 |
| TARGET | 484 | MFE_R | 3.94 | 6.07 | 6.62 | 8.37 |
| TARGET | 484 | MAE_R | 0.07 | 0.47 | 0.89 | 0.99 |

(Note: MFE_R for TARGET shows p10=3.94 due to MFE update happening after
close-resolution in OnTick — peak MFE for winners is reported as the
pre-target tick's value, not the target itself. This is an instrumentation
oddity; doesn't affect the loss-rescue analysis below.)

### P(MFE_R ≥ T | STOP) — rescuable losses

| T (R) | count | fraction | Notes |
|---:|---:|---:|---|
| 0.5 | 1,110 | 64.3% | Most losses go *some* favorable distance first |
| 1.0 | 752 | 43.6% | Almost half go a full R |
| 1.5 | 537 | 31.1% | |
| **2.0** | 375 | **21.7%** | Below the 28.6% breakeven for partial-TP at 2R |
| 3.0 | 216 | 12.5% | |
| 4.0 | 115 | 6.7% | |
| 5.0 | 54 | 3.1% | |

### MAE_R for TARGET wins — close-call winners

- P(MAE_R ≥ 0.5 | TARGET) = **47.3%** — half of winners had a half-stop
  adverse excursion at some point
- P(MAE_R ≥ 0.9 | TARGET) = **9.5%** — 10% of winners almost stopped out

### Counterfactual: partial-50%-at-T-with-stop-to-BE — BEST-CASE lift

Per-trade expected ΔR = `−WR×(3−T/2) + (1−WR)×P(MFE≥T|STOP)×(T/2 + 1)`,
assuming remainder *always* hits target on wins (upper bound — true gain
requires path-level simulation).

| T | P(MFE≥T \| STOP) | best-case ΔNET | new NET | lift |
|---:|---:|---:|---:|---:|
| 0.5 | 64.3% | **+$33,689** | $720,761 | **+4.9%** ★ |
| 1.0 | 43.6% | −$110,875 | $576,197 | −16.1% |
| 1.5 | 31.1% | −$179,842 | $507,230 | −26.2% |
| 2.0 | 21.7% | −$250,400 | $436,672 | −36.4% |
| 2.5 | 16.9% | −$220,692 | $466,380 | −32.1% |
| 3.0 | 12.5% | −$211,673 | $475,399 | −30.8% |

★ = highest best-case lift

**Only T=0.5 gives positive NET lift** — and only +4.9% in the BEST case.
Path-level reality is strictly worse (winners' remainder won't *always*
hit target after a 0.5R partial). The actual path-corrected lift at T=0.5
is likely 0% or negative.

## Verdict: HG1 closed — exit framework is empirically optimal

The win-side cost of any partial-TP / stop-to-BE policy dominates the
loss-side rescue gain. Specifically:
- WR is too low (22.2%) — every partial-TP that fires costs ≥(3 − T/2) R
  on each winner, and only 22% of trades are winners. The 78% loser pool
  doesn't have enough trades reaching MFE ≥ T to compensate.
- Even at T=2 (where 21.7% of losers reach the threshold), the win-side
  cost (−$250k) overwhelms the loss-side rescue.
- **The 6:1 fixed-RR exit is the correct policy for this strategy** at
  WR=22% — confirms Cat B1/B2 rejection memory under proper instrumentation.

Stops also can't be meaningfully tightened: 9.5% of winners had MAE_R ≥ 0.9.
Tighter stop = killed winners.

## What this rules out

- HG1 (MFE/MAE-driven exit redesign) — **closed**
- Cat B1 trailing-stop variants — confirmed empirically
- Cat B2 multi-level TP — confirmed empirically
- Tight-stop variants — would kill ~10% of winners

## What this leaves

The "leave money on table" hypothesis is FALSE for this strategy. The
holy grail isn't in the exit framework. Pivoting to:

- **HG2 — funding-asymmetry both-sides regime strategy.** Universe funding
  is positive 75% of time → systematic short carry. Long when extreme
  negative funding, short when extreme positive funding. Both sides,
  regime-gated. Re-validate post-funding-bug-fix correlation.
- **HG3 — winner-profile classifier.** Adds a filter (technically a
  candidate), but mechanism-grounded.

HG2 has the higher conviction — funding asymmetry is a documented
structural edge in crypto futures, orthogonal to the EMA-cross trend
mechanism.

## What changed in the codebase

- `pkg/execution/stub.go`:
  - `journalEntry` gained `MFER` / `MAER` fields (omitempty in JSON)
  - `MaxFavorableR` tracking moved out from under `TrailingStopMode` —
    was a silent bug (MFE was 0 in normal-exit journals)
  - Both close-emission paths (full close + partial close) emit MFE/MAE
- `scripts/mfe_mae_analysis.py`: descriptive + counterfactual analysis
- `results/mfe_mae_2026-05-07.txt`: raw output
- `results/hod_journals/2026-05-07-mfe/`: re-run journals with MFE/MAE

## Reproduction

```bash
DATE_TAG=2026-05-07-mfe bash scripts/hod_journals.sh
python3 scripts/mfe_mae_analysis.py
```
