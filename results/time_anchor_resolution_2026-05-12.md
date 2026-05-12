# TIME-ANCHOR resolution — first-close canonical

**Date locked**: 2026-05-12
**Resolves**: T12 operator decision from `docs/findings/2026-05-11-pm.md`
**Affects**: CLAUDE.md "## Forward-paper go/no-go criteria" elapsed-time
gates (criterion #1 ≥60d; criterion #4 pro-rated annual) + LIMBO rule
inputs in `results/forward_paper_outcome_resolution_decision_rule_2026-05-10.md`

## The question

Three documents disagreed on the elapsed-time anchor:

- `real_money_protocol_decision_rule_2026-05-08.md` line 134: prose
  reference to "forward-paper start" (deploy date 2026-05-05) when
  projecting the EARLIEST STAGE_4 reach (2027-03-09).
- `forward_paper_outcome_resolution_decision_rule_2026-05-10.md` line 47:
  formal input definition — `n_days_live` = "calendar days since live
  cohort's first close."
- Implementation (`stage_promotion_check.py`, `forward_paper_status.sh`):
  comments + labels said "first trade", though on inspection the actual
  data path loads only `event == "close"` records and the dashboard
  awk block populates `first_ts` inside `$1 == "close"`. The
  implementation was already first-close-anchored — only the labels
  drifted.

## Resolution

**Canonical anchor: first-close.**

Matches the only doc that formally defines the gate inputs
(`forward_paper_outcome_resolution_decision_rule_2026-05-10.md` line 47).
The other doc's "forward-paper start" reference is prose projection
(predicting calendar milestones for operator planning), not a gate
definition — and is not in conflict with the formal input definition
when read in context.

## What changes

Labels and comments only. The implementation was already correct.

- `stage_promotion_check.py::check_days`: criterion name "(since first
  trade)" → "(since first close)"; comment block rewritten to remove
  the AMBIGUITY framing.
- `stage_promotion_check.py::check_pro_rated_annual`: same.
- `forward_paper_status.sh`: header comment "days elapsed since first
  trade" → "since first close"; printf line label refreshed.
- `forward_paper_resolution.py`: pro-rated diagnostic message anchor
  text refreshed.
- `test_stage_promotion_check.py`: T12 pin tests
  `test_check_days_name_surfaces_first_trade_anchor` +
  `test_check_pro_rated_annual_name_surfaces_first_trade_anchor`
  renamed + docstrings rewritten, assertion strings updated.

New defensive pin: `test_load_journal_filters_to_close_events` —
source-scans `load_journal` to assert it filters on `event == "close"`.
Locks the IMPLEMENTATION anchor (not just the label) so a future
refactor that accidentally widens the filter to include open events
fails CI.

## Why this isn't a relaxation

- The gate semantics don't change. The threshold (≥60d) is the same.
  The numerator-vs-denominator alignment in `check_pro_rated_annual`
  doesn't change.
- Today (forward-paper day 7, first trade landed within hours of
  2026-05-05 deploy), the numerical difference between deploy /
  first-trade / first-close anchors is sub-day.
- The implementation was already at first-close. This amendment locks
  the documentation + the test pins to match the implementation
  reality.

## Resilience benefit

In a quiet-signal regime where the first close lands days or weeks
after deploy, first-close anchoring is strictly more honest than
deploy-anchoring:
- A "no-data" cohort would burn `days_since_deploy` credit toward the
  60-day gate under deploy-anchoring, despite having no realized PnL
  to evaluate.
- First-close anchoring matches the test semantics: the 60-day window
  is "60 days of data," not "60 days of waiting."

## Forward-paper data continuity

No change to any historical value. Current gate evaluations on the
forward-paper-day-7 cohort produce identical numbers before and after
this amendment — the labels just say "first close" instead of "first
trade."

## Lineage

- 2026-05-08: real_money_protocol pre-reg locked
- 2026-05-10: forward_paper_outcome_resolution pre-reg locked, defines
  `n_days_live` formally as first-close-anchored.
- 2026-05-11 PM: T12 surfaces label-vs-impl drift (labels said "first
  trade", implementation was first-close).
- 2026-05-12: this amendment aligns labels + tests + comments to the
  formal input definition.
