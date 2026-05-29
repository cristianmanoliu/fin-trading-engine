# Milestone-2 Addendum (PROPOSED) — overfit-gate + 3 new mechanisms

**Status:** PROPOSED 2026-05-30. **Non-actionable.** This does NOT modify the locked milestone-2
artifacts. It proposes additions to be ratified into them via their own migration governance when
milestone-2 approaches:
- `strategy_backlog_milestone2_2026-05-08.md` (LOCKED — 8 ideas, top-5 A2/A1/C1/B2/D1)
- `milestone2_runbook_decision_rule_2026-05-10.md` (LOCKED — execution sequence + Bonferroni α=0.01; migration triggers at its §"Migration triggers")
**Author:** Cristian Manoliu
**Born from:** the FRAGILE overfit verdict (`backtest_overfit_pbo_dsr_verdict_2026-05-29.md`) + the
reusable PBO/DSR tooling built the same day.

## Why an addendum, not a new plan

The milestone-2 plan already exists and is strong (pre-registration, falsification tests,
prioritization rubric, family-wise α, locked execution order). Two things post-date it and are
genuinely additive; everything else I considered is already covered (see §3). Adding them follows
the runbook's locked migration triggers — this doc is the proposal, ratification happens at
milestone-2 launch.

---

## 1. PROPOSED methodology addition — batch overfit-gate (PBO + Deflated Sharpe)

**The gap it fills.** The runbook's family-wise Bonferroni (α_family=0.05, N=5 → α=0.01) corrects
for the **5 ideas** tested. But each idea is itself a parameter SEARCH — A1 sweeps vol quintiles,
B2 sweeps the p10 squeeze threshold, C1 sweeps p5/p95 funding cutoffs, A2 sweeps ATR multipliers.
That **within-idea search consumes degrees of freedom the N=5 Bonferroni does not see.** The
2026-05-29 analysis showed this matters: a 34-config search produced a FRAGILE verdict (DSR 0.64)
even though each config was individually plausible.

**The gate.** When a milestone-2 idea is run as a param grid, additionally evaluate the FULL set of
param-cells actually run through `scripts/backtest_overfit_analysis.py` with **N = total distinct
cells in that idea's frozen grid**:
- Build the months×cells NET matrix (`scripts/overfit_matrix_gen.sh` pattern, entry-month pairing).
- Compute PBO (CSCV) + Deflated Sharpe at N = grid size.

**Composes with the existing per-phase verdict** (it does not replace the backlog's ADOPT/REJECT
bootstrap-CI criteria — it adds a gate):

| Overfit-gate outcome | Effect on the phase verdict |
|---|---|
| PBO < 0.50 AND DSR(N_grid) ≥ 0.95 AND parameter plateau | gate PASS — phase verdict stands as per backlog criterion |
| DSR(N_grid) ∈ [0.90, 0.95) or no plateau | gate WARN — ADOPT downgraded to HOLD (shadow-only, no deploy) |
| PBO ≥ 0.50 OR DSR(N_grid) < 0.90 | gate FAIL — REJECT regardless of the raw bootstrap-CI result |

(Slope criterion per the 2026-05-29 caveat: excluded when IS-best concentration > 0.5.)

**Ratification:** this is a verdict-methodology change → per runbook §"Migration triggers" line
~140 it requires an explicit `results/migration_<date>.md` at milestone-2 launch. This doc is the
advance proposal; the migration doc formalizes it then.

---

## 2. PROPOSED new backlog ideas (H-series) — in the backlog's locked format

Each is genuinely NOT among the existing 8 (see §3 for the distinction). Adding any is a "backlog
update" → runbook migration trigger #1 (execution sequence + α budget recompute mechanically; N
grows from 5).

### H1. Delta-neutral funding carry  — distinct from C1

- **Hypothesis:** holding spot long + perp short (market-neutral) harvests positive funding as a
  carry/risk-premium, independent of price direction.
- **Mechanism:** crowded leveraged longs pay funding to shorts; you're paid to provide that
  liquidity. Persists because retail leverage demand is structural. **C1 trades funding as a
  directional REVERSAL signal; H1 is a market-NEUTRAL carry book — different risk profile entirely.**
- **Impl sketch:** paired spot+perp legs per symbol; accrue funding net of fees on both legs;
  measure carry + the distribution of funding-flip / basis-blowout drawdowns.
- **Verdict criterion:** ADOPT (as independent market-neutral sleeve) if 5y net carry > 0 at
  realistic costs AND worst 30d carry drawdown ≤ a pre-set tail bound AND Cat-X (Bybit) sign
  agreement. REJECT if net carry ≤ 0 after costs. HOLD if positive but tail-fragile.
- **Why it might NOT work:** funding flips negative in bear regimes (you pay); basis/de-peg/
  liquidation tail. Falsification: if net carry is positive only in the 2020-21 bull funding
  regime and negative 2022-25, the "premium" is regime-conditional, not structural.
- **Sub-scores:** mechanism=4, independence=5, impl=2, data=4. **Composite ≈ 3.6.** Priority HIGH
  (market-neutral → strongest diversifier; impl cost is the only drag).

### H2. Cross-sectional momentum rotation

- **Hypothesis:** rank the universe by trailing N-day return, hold the top-K, rotate periodically;
  relative strength persists short-term.
- **Mechanism:** *cross-sectional* momentum (symbols vs each other), documented in equities +
  crypto. **Distinct from the deployed strategy's time-series momentum (each symbol vs its own
  trend) and from F1's multi-TF ensemble (same symbol, multiple TFs).**
- **Impl sketch:** weekly/monthly rank by 30/60/90d return (pre-commit lookback grid), hold top-K,
  optional BTC-trend gate; NET after turnover.
- **Verdict criterion:** ADOPT if 5y NET > 0 at slip=5bp AND Sharpe ≥ 0.8 AND plateau over
  lookback × K. REJECT if NET ≤ 0. HOLD if marginal.
- **Why it might NOT work:** rotation crashes (top-K reverse together); turnover cost; whipsaw at
  the rank boundary. Falsification: if a random-K portfolio matches top-K Sharpe, the ranking adds
  nothing.
- **Sub-scores:** mechanism=4, independence=4, impl=3, data=5. **Composite ≈ 3.9.** Priority HIGH.

### H3. Failed-pump / liquidation-cascade short  — operator idea

- **Hypothesis:** a fast pump that fails fast → late leveraged longs' stops + liquidations become
  forced selling → ride the dump short.
- **Mechanism:** forced-seller cascade + panic. Persists because leverage + FOMO are structural.
  Needs a LOW-TF trigger (4H too slow).
- **Impl sketch:** define pump (+X% in N 5m bars) + failure (no new high within M bars / sharp
  reversal); short on breakdown; pre-commit X/N/M grid.
- **Verdict criterion:** ADOPT if 5y NET > 0 at slip=5bp AND **PnL correlation to the deployed
  trend-follower < 0.5** (must be ADDITIVE, not a re-skin) AND plateau. REJECT if NET ≤ 0 OR
  correlation ≥ 0.7. HOLD between.
- **Why it might NOT work:** ADJACENT to LIVE (both capture forced-downside flow) → may be the same
  edge in a different costume; V-recovery; low-TF execution latency. Falsification: correlation to
  LIVE ≥ 0.7 means no new mechanism.
- **Sub-scores:** mechanism=3, independence=2, impl=2, data=4. **Composite ≈ 2.6.** Priority
  MEDIUM-LOW (adjacency risk; validate additivity first).

**Proposed ranking among new ideas:** H2 (3.9) > H1 (3.6) > H3 (2.6). If ratified, re-run the
backlog's composite ranking over all 11 ideas and re-pick the milestone-2 subset; the runbook's
α budget recomputes for the new N.

---

## 3. Already covered — explicitly NOT re-added (no duplication)

| Considered | Already in plan as | Verdict |
|---|---|---|
| Mean-reversion / grid / Bollinger fade | B1 (RSI extremum), B2 (BB squeeze), live `bb20` shadow | covered |
| Funding as a directional signal | C1 (funding-extremum reversal) | covered (H1 ≠ C1: carry vs signal) |
| Trend variants (EMA/MACD/RSI periods, TFs) | milestone-1 05-19 arc; FRAGILE 2026-05-29 | rejected — re-skin |
| Vol-regime filter / ATR sizing / session / multi-TF ensemble / tick-imbalance | A1 / A2 / D1 / F1 / G1 | covered |

## Cross-references

- `results/strategy_backlog_milestone2_2026-05-08.md` — the locked 8-idea backlog (H-series extends it)
- `results/milestone2_runbook_decision_rule_2026-05-10.md` — locked execution + migration triggers
- `results/backtest_overfit_pbo_dsr_verdict_2026-05-29.md` — the FRAGILE finding motivating the gate
- `scripts/backtest_overfit_analysis.py` / `overfit_matrix_gen.sh` — the gate's tooling
- Memory: `user_broad_backtest_explore_then_shadow`

## Scope

PROPOSED, non-actionable, research-banking. Ratified only at milestone-2 launch, via the runbook's
migration process (backlog update for H-series; `migration_<date>.md` for the overfit-gate). Until
then it changes nothing.
