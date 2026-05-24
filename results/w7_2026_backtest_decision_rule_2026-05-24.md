# W7 (2026) walk-forward extension — pre-registered decision rule

**Status:** LOCKED 2026-05-24, BEFORE running any 2026 backtest.
**Author:** Cristian Manoliu
**Context:** Prior 6-window validation (results/6window_decision_rule_2026-05-06.md) covered
2020-05 → 2026-04. W7 = Jan–Apr 2026, the first 4 months of 2026 — 100% OOS (no prior
analysis has ever run on 2026 price data). Forward-paper started 2026-05-05; W7 ends before
that date so there is zero overlap with live decisions.

---

## What is being tested

The **live candidate config** run against 2026 data:

- Signal TF: 4H
- EMA: 9/21
- Side filter: short only
- Target RR: 6.0
- Max hold: 504h
- Fee: 10bp
- Slip: 5bp
- Stake: $1,000
- Universe: 16 deployed symbols (configs/symbols.yaml `deployed` list)
- Flags: `--exact-fills --include-boundary --pessimistic-ambiguous`
- Period: 2026-01 → 2026-04 (4 months, Jan–Apr inclusive)

**Also run:** same config on the full 57-symbol universe to test whether the deployed-16
selection generalizes to the broader universe in this regime.

---

## Pre-registered predictions (written before running)

Based on known W3 (2025-05 → 2026-04) behavior from the 6-window sweep:

1. **W7 will show lower NET than the per-window average of W1–W6.** W3 was already showing
   regime pressure. If the strategy's weak point is 2025-2026, extending into 2026 should
   extend that pressure.

2. **WR will be below 20.6% backtest mean.** The forward-paper live config is already seeing
   WR ~7% at n=27, which is noise at low n, but directionally consistent with a regime that
   doesn't favor this strategy. W7 backtest should show WR suppression vs historical mean.

3. **NET will be negative for majority of 16 deployed symbols.** Not necessarily for all 57 —
   universe effects can mask per-symbol pressure.

4. **Funding will not rescue results.** W7 is a short-biased strategy; short funding rates in
   Jan–Apr 2026 have been roughly neutral (not strongly positive for shorts).

---

## Verdict criteria (mechanical, applied after results are seen)

| Result | Verdict | Implication |
|--------|---------|-------------|
| W7 NET > 0 AND WR ≥ 14.3% (breakeven) for deployed-16 | **SUPPORTIVE** | 2026 regime consistent with strategy viability; no config change warranted |
| W7 NET > 0 AND WR < 14.3% | **WEAK** | Marginal; proceed with forward-paper but note regime fragility |
| W7 NET ≤ 0 AND magnitude < $5k total | **BORDERLINE** | Within noise for 4 months; non-actionable, file as data point |
| W7 NET ≤ 0 AND magnitude ≥ $5k total (deployed-16) | **ADVERSE** | 2026 is hostile regime; strengthens prior that live forward-paper may kill; no config change during monitoring but inform post-kill analysis |
| W7 NET ≤ 0 for ≥ 12/16 deployed symbols | **REGIME-HOSTILE** | Per-symbol breakdown confirms systemic regime pressure, not symbol-specific noise |

**Non-actionable during monitoring:** regardless of verdict, this result does NOT trigger any
change to the live config. Forward-paper resolution is governed by `forward_paper_outcome_
resolution_decision_rule_2026-05-10.md` only. W7 result is **research context** for
post-resolution strategy design in milestone 2.

---

## What this does NOT test

- Whether any config change would fix the 2026 regime issue (that's milestone 2 scope)
- Whether the strategy should be killed (drift detector governs that)
- Whether the deployed-16 symbol selection was optimal (selection was pre-registered as non-actionable)

---

## Comparison baseline

W3 (2025-05 → 2026-04) from 6window sweep: used slip=15bp (not 5bp). W7 uses slip=5bp
(production cost model). Results are NOT directly comparable on absolute $ but ARE comparable
on WR and per-symbol pass/fail counts.

The per-window NET from the 6window sweep at slip=15bp:
- Referenced from results/6window_decision_rule_2026-05-06.md (read before applying verdict)

---

## Scope

Research-only. Non-actionable during forward-paper monitoring phase. Filed as calibration
artifact for milestone-2 regime analysis.
