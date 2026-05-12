# PARTIAL canon resolution — terminals-only

**Date locked**: 2026-05-12
**Resolves**: T13b operator decision from `docs/findings/2026-05-11-pm.md`
**Affects**: CLAUDE.md "## Forward-paper go/no-go criteria" criterion #1
("≥150 live trades") + the dashboard / formal-gate trade-count
semantics

## The question

Two tools counted PARTIAL closes differently:

- `scripts/stage_promotion_check.py::load_journal` (the formal gate)
  SKIPS PARTIAL closes — counts only terminal positions toward the
  150-trades criterion.
- `scripts/forward_paper_status.sh` (the operator dashboard) counted
  every close event toward `total` AND counted PARTIAL as a `win`
  contributing to WR.

Today the divergence is latent: live config is single 6:1 RR target
with no B2 / multi-TP, so zero PARTIAL emission. With B2 or multi-TP
re-enabled, the dashboard's "≥150 trades" view would fire BEFORE the
formal gate fired, and the operator's mental model would diverge from
the formal verdict.

## Resolution

**Canonical: terminals only (the gate's interpretation).**

Reasoning:

- The 150-trades criterion exists to bound CI width on WR. CI math
  assumes INDEPENDENT decisions. Multiple PARTIAL exits from one entry
  share the same entry signal and alpha source — they are not
  independent decisions, so multi-counting them inflates the
  trade-count CI without bounding it.
- PARTIAL closes are scale-out events, not separate decisions. A
  position that scales out across 3 PARTIAL exits and one terminal
  STOP is ONE entry decision with multiple realized PnL events. The
  trade-count gate is testing decision-quality, which lives at the
  entry-decision level.
- The dollar accounting (PnL, fees, slip) IS event-level, not
  decision-level — every close event realizes real dollars. The
  resolution preserves PnL/fee/slip aggregation across PARTIALs; only
  the trade COUNT changes.

## What changes

- `scripts/forward_paper_status.sh` awk block: `total` and `wins`
  increment only on non-PARTIAL closes. New `partials` counter tracks
  PARTIAL events separately. PnL/fee/slip/notional still accumulate
  from all closes (including PARTIAL).
- STRATEGY|... line gains a trailing `partials` field. Internal-only;
  the only parser of this format is forward_paper_status.sh itself.
- "Trades closed: N / 150" surface now shows TERMINAL N. When
  `partials > 0`, an extra line surfaces: "(+ K partial closes; not
  counted in trades/wins per D3 canon)" — the operator sees both
  views without conflation.
- Comment block in `stage_promotion_check.py::load_journal` rewritten:
  removes the "drift with the dashboard" framing because the two sites
  now agree.
- "NODATA" emit-condition relaxed: previously fired when total==0,
  now fires only when both total==0 AND partials==0. A mid-flight
  partial-only cohort (B2 enabled, one PARTIAL realized, no terminal
  yet) still surfaces a STRATEGY line with first_ts + PnL.

## What stays locked

- The 150-trade threshold itself (criterion #1).
- PARTIAL events still appear in the journal, drift detector, and HODL
  comparator — those tools see per-close events independent of the
  display semantics.
- CLAUDE.md text doesn't need updating because it only says "≥150
  trades" — which was always terminal-count semantically; the
  dashboard was the outlier.

## Forward-paper data continuity

Zero behavioral change today: live config emits no PARTIAL events.
Every cohort's "Trades closed" number is identical before and after
D3. Activates differently only when B2 / multi-TP is re-enabled.

## Test pins

`scripts/test_forward_paper_status.py` gains a fixture-driven test
that emits a mix of TARGET / STOP / PARTIAL close events and asserts:
- `trades` counts only TARGET + STOP (terminal outcomes)
- `wins` counts only TARGET
- `partials` shows the PARTIAL count
- The "(+ N partial closes ...)" line renders only when partials > 0

## Lineage

- (pre-2026): Stub executor's B2 trail-stop mechanic emitted PARTIAL
  closes when partial-take-profit fired; dashboard's awk was written
  to count those as wins.
- 2026-05-04: P4-Combined live config locked single 6:1 RR target, no
  multi-leg. PARTIAL emission went to zero.
- 2026-05-11 PM: T13b surfaces the latent dashboard/gate divergence.
- 2026-05-12: this resolution makes the dashboard match the gate.
