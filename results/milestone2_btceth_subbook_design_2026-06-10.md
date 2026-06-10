# Milestone-2 design: BTC/ETH-only short sub-book with #15+#16 regime gate (2026-06-10)

**Status: DESIGN ONLY — non-actionable until milestone-2 launch.** This document fixes
the architecture, evidence base, experimental design, and decision frame. It does NOT
lock numeric verdict bars (those are pre-registered fresh at milestone-2 launch per
`milestone2_runbook_decision_rule_2026-05-10.md` migration triggers) and authorizes no
build, no shadow, no live change. Charter freeze (~2026-08-06) applies.

Design decisions ratified by operator 2026-06-10: **both strategy variants** specced for
milestone-2; validation path **backtest gates → shadow → own forward-paper**; sizing
intent **same staged $100→$1k ladder, run as an independent protocol instance**.

---

## 1. Evidence base (data of record: the two verdict docs)

| signal | definition | short-favorable regime | strength | robustness |
|---|---|---|---|---|
| #15 DVOL VRP | VRP-z = z(DVOL − realized vol) | **complacency: VRP-z < 0** | ETH Welch t=2.04; BTC t=0.79 (n.s.) | ETH 5/6 years |
| #16 Coinbase premium | premium-z = z(Coinbase spot − Binance) | **US not bidding: premium-z < 0** | BTC t=2.39, ETH t=2.29 | 6/7 years each |

Independence: corr(VRP-z, premium-z) = −0.12 over 1,789 common days — two distinct bets.

> ⚠️ **SIGN WARNING.** The orthogonal-search synthesis and the 2026-06-10 handoff both
> shorthand the combined gate as "short when **fear-high** AND US-not-bidding". The #15
> half of that is BACKWARDS. The verdict doc (data of record,
> `dvol_vrp_verdict_2026-06-10.md`) measured: ETH short next-day return = **+19.6bp under
> complacency (z<0)** vs **−16.5bp under fear (z>0)** — after fear extremes the market
> mean-reverts UP and shorts get hurt. The correct combined gate is
> **VRP-z < 0 AND premium-z < 0**. Any milestone-2 implementation must cite the verdict
> docs, not the synthesis shorthand.

**Honest multiple-testing context (carried into the design as prior, stated now):** these
two gates are the maxima of a 20-candidate search. Expected max-|t| across ~20 roughly
independent null candidates is ≈2.3–2.5 — raw t=2.0–2.4 is therefore approximately AT
the null-expectation boundary. What elevates #15/#16 above noise is the conjunction of
multi-year robustness (5/6, 6/7, 6/7), mutual independence (−0.12), and economically
correct signs measured on out-of-band data sources. The milestone-2 pre-reg bars must
price this in (e.g., demand the combined gate beat EITHER single gate, not just beat
no-gate). Also carried in: the price-based regime-gate prior is bad
(`project_regime_gate_shortonly_degenerate`: F=0/9) — these gates are exogenous, a
different class, but the burden of proof stays on the gate.

## 2. The sub-book

Two NEW paper engines, BTCUSDT + ETHUSDT (neither is in the deployed-16). Rate-limit:
18 symbols × 10s poll × 20 weight = 2,160/min vs 2,400 cap — fits, but leaves only one
more engine of headroom; documented as a constraint on any future symbol additions.

### Gate computation (shared by both variants)

- Inputs, daily at 00:30 UTC on VPS: Deribit DVOL (BTC+ETH) via `fetch_dvol.py`
  lineage; Coinbase + Binance spot closes via `fetch_flow_data.py` lineage; realized
  vol from engine's own kline history.
- Output: one JSON state file per symbol (`/etc/paper-live/gate/{SYM}.json`):
  `{date, vrp_z, premium_z, gate_green, computed_at}`. z-scores: **90d rolling**, the
  exact form both studies validated (`coinbase_premium_study.py` line 14,
  `dvol_vrp_study.py` line 15) — no train/live mismatch, no new window DoF.
- **Measured gate behavior** (computed 2026-06-10 from the study data, 1,806 common
  days): P(premium-z<0) ≈ 0.50, P(vrp-z<0) ≈ 0.46-0.47, **P(dual green) ≈ 0.24-0.25**,
  ~48 state flips/yr, median green run 1-2 days. The dual gate is CHOPPY at daily
  granularity — both variants must be designed for that, not for slow regimes.
- Per-symbol gate forms (both are pre-reg cells, choice made by backtest):
  - **dual** (primary): green iff vrp_z < 0 AND premium_z < 0 — both symbols.
  - **asymmetric** (secondary): ETH dual; BTC premium-only (since #15 is n.s. on BTC).
- Staleness: `computed_at` older than 48h → Telegram WARN. Shadow phase: **fail-open**
  (trade ungated, log gate state — preserves the paired experiment below). Any future
  real-money phase: **fail-closed for new entries** (no fresh gate, no fresh risk).

### Variant A — live EMA config + gate as entry filter

Identical to the validated live engine (EMA 9/21, 4H, short-only, 6:1 RR, wick stop,
mh504, funding CSV, fee10/slip5) on BTC+ETH; the gate suppresses NEW entries while red;
open positions are NEVER touched by the gate (exits unchanged). Implementation surface:
one entry-filter hook in the Runner (config: `--regime-gate-dir`), reading the state
file at signal time; absent/disabled file = ungated behavior, so backtest byte-parity
is preserved when off.

**Power problem, stated honestly (corrected on review):** BTC live-config journals
show **184 trades/6.4y (0.078/day)**; ETH has never been journaled (assumed similar) →
two symbols ≈ 0.16/day of signals. With the dual gate green only ~25% of days, the
gated arm trades **~14/yr** — a sequential 150-trade floor would take ~a decade. The
paired design in §3 is therefore not an optimization but the only feasible evaluation;
even its blocked-set n=60 takes ~17 months. Variant B, with ~365 daily observations/yr
per symbol (the same granularity the t=2.0-2.4 splits were measured on), resolves far
sooner — so **B is the primary evaluable variant; A is the deployment-form rider**
whose case rests on B's result plus A's backtest.

### Variant B — gate-as-strategy

Short $-fixed notional while gate green, flat while red; evaluated once daily at the
gate timestamp; exits/entries at next 4H candle close after state change. No stop/target
geometry; costs per flip (10bp fee + 5bp slip per side) and funding accrued while short.
This is closer to what the t=2.3 splits actually measured (unconditional daily short
returns) but has NEVER been cost-validated — that is precisely what its backtest cell
exists to decide. **Measured flip rate: ~48/yr with median green run 1-2 days** — B is
a 1-2-day holding-period strategy. Cost drag ≈ 24 round-trips/yr × 30bp ≈ 0.7%/yr per
symbol (modest); the real question its backtest answers is whether the split survives
the chop (the studies measured per-DAY conditional returns, which a 1-2d holder
captures almost directly). B is also the fast-resolving variant (§2A).

## 3. Experimental design — the paired-arms shadow (the key idea)

A gated-vs-ungated comparison run as two SEPARATE engines would need years (§2A power
problem). Instead each engine runs BOTH arms on the same tick stream, same signals:

- **Arm U (ungated):** every EMA signal becomes a (paper) trade — this is the baseline.
- **Arm G (gated):** same signals; trades only when gate green at signal time.
- The arms differ ONLY on signals fired while the gate is red — the **blocked set**.
  The gate's entire value claim lives there: `sum(PnL of blocked trades)` should be
  **negative** (the gate skipped losers) if #15/#16 are real.
- Statistical test at evaluation: sign/median test + bootstrap on the blocked set
  directly, NOT a two-sample comparison of full arms. With the dual gate red ~75% of
  days (measured), the blocked set captures ~75% of signals (~43/yr across both
  symbols) — n=60 in ~17 months, vs ~decade for a gated-arm-only trade count. Power
  scales with the blocked set, and the blocked set is the larger fraction.

Variant B runs as a third journal label on the same engines (its own arm, daily-driven).
All journals via the existing shadow mechanism (`--shadow`-style labels, lazy-write,
`journal_fetch.sh`/dashboard pickup) — no new monitoring surface.

## 4. Validation pipeline (locked path; numeric bars locked at milestone-2 pre-reg)

1. **Stage 0 — data audit BEFORE any study run** (batch-3 lesson, mandatory): verify
   span + row count + alignment convention of every input (DVOL history from 2021-03
   only — the backtest window must match; Coinbase premium from 2019+; right-edge
   labels everywhere; `.values` on Series construction). Pre-reg the universe claim
   only after the spans are verified.
2. **Stage 1 — backtest both variants × both gate forms** (4 cells + ungated baseline)
   with the standing 5-gate battery (walk-forward, family DSR, corr-to-live < 0.5,
   by-year, realistic costs) + full honesty block (median, drop-top-5%, correct sign,
   selection-corrected across all cells). Required extra: combined gate must beat the
   better SINGLE gate, per §1. Bar values locked in the milestone-2 pre-reg doc before
   the first run.
3. **Stage 2 — shadow** (only cells that survive Stage 1): paired-arms engines per §3
   on VPS; `post_deploy_check` extended with a gate-staleness section; zero changes to
   the existing 16 engines.
4. **Stage 3 — own forward-paper floor**: evaluation on the blocked set per §3, floor
   sized by blocked-set count (pre-reg'd at milestone-2; indicatively ≥60 blocked
   signals). Promotion beyond shadow follows the staged real-money protocol
   ($100→$300→$500→$1k) as an INDEPENDENT instance — main-book stage status neither
   accelerates nor blocks the sub-book, but a main-book locked kill freezes everything
   project-wide as today.

## 5. Risks / open questions for the milestone-2 pre-reg

- **z-window**: 90d rolling is inherited from the studies as-validated — it is NOT a
  free parameter at milestone-2. A single optional sensitivity column (180d) may be
  reported but cannot drive selection.
- **Gate chop**: median green run 1-2 days means variant A's entry filter samples the
  gate at 4H-signal times against a state that flips ~weekly-to-daily; the studies'
  evidence is daily-granularity, so A's application is a mild extrapolation —
  documented, and resolved empirically by the paired arms.
- **#15 is ETH-only significant** — if the asymmetric form wins the backtest, the
  sub-book is really "ETH dual-gated + BTC premium-gated", and the doc's headline
  should say so plainly.
- **External data dependency** (Deribit + Coinbase public APIs) is a new operational
  failure class; fail-open/fail-closed split per §2 is the mitigation, plus weekly
  staleness audit.
- **Regime count**: ~5y of gate data ≈ a handful of regime cycles; the by-year
  robustness is the strongest counter, but Stage 3's blocked-set test is the only
  forward, out-of-sample evidence that will exist. Nothing real-money before it.

## 6. Trigger & non-goals

- **Trigger:** milestone-2 launch per the runbook's migration triggers (earliest at
  charter-freeze expiry ~2026-08-06, and only after the main forward-paper question is
  resolved per its own locked criteria).
- **Non-goals:** no alt-book gating (data doesn't cover it), no third-party paid data,
  no new strategy mechanics beyond the two variants, no change to the deployed 16
  engines, no INDEX/backlog reshuffle ahead of the milestone.
