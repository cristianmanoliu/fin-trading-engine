# Drift-detector censoring blind-spot + N-floor gap — finding & proposal (2026-05-30)

**Status:** FINDING + PROPOSAL. Pre-registered **before** the next ≥7d-apart drift firing
(~2026-06-07), which would be the first that mechanically trips the two-firings auto-kill (wrapper
exit 4). Proposals are non-actionable until ratified; ratification is itself a pre-registration move —
**decide the rule before the data arrives.**

**Resolution (2026-05-30):** Proposal 1 (censoring cross-check 9) **RATIFIED** into
`drift_firing_investigation_decision_rule_2026-05-08.md` (sanctioned by its migration-trigger-1).
Proposal 2 (n≥50 auto-kill arming) **DEFERRED to milestone-2** — it is *re-tuning*, which
`drift_detector_time_to_detection_verdict_2026-05-08.md` explicitly forbids within this milestone. The
06-07 risk is therefore defused by cross-check 9 + the operator-in-loop exit-4 gate, **not** by changing
the kill bar.

**Trigger:** Migration-trigger-1 of `drift_firing_investigation_decision_rule_2026-05-08.md` ("first
firing actually occurs — review whether the playbook held up"). The first real drift firing
(2026-05-27, corroborated 2026-05-30) was investigated in
`results/drift_firings/2026-05-27-B-general-lown-censoring.md`. This doc is the mandated review plus two
methodological findings it surfaced.

**Bottom line:** Neither finding changes the live strategy, and **the 2026-05-27 auto-kill anchor stays
armed.** Both findings refine how an exit-1/exit-4 firing is *interpreted* during forward-paper's
fill-in window. Without them, the mechanical two-firings rule could fire a kill on a *healthy* strategy.

---

## Finding 1 — Winner-censoring blind spot (decision-grade)

### Mechanism

The strategy is **6:1 R:R with 336–504h max-hold**. Outcome resolution is therefore strongly
**asymmetric in time**:
- Losers hit the wick stop fast (hours–days).
- Winners ride toward +6R over up to 14–21 days.

At any early forward-paper snapshot, the **closed-trade sample is loser-biased** — fast outcomes have
resolved, slow winners are still open. The drift detector
(`scripts/live_vs_backtest_drift.py`) emits a `Trade` **only on a `close` event** (loader lines 88–102)
and the `Trade` dataclass carries **no holding-time field** (lines 65–72). So:

- **Live sample = closed trades only.** Open positions are invisible.
- **Backtest reference = a completed 5y run** (`hod_journals/2026-05-07-mfe`, 2,210 trades) where every
  position eventually resolved → **~0% censoring.**
- The detector compares a **censoring-biased partial** live distribution against a **fully-resolved**
  backtest distribution, **with no survival correction.** Under a long-hold high-RR strategy this
  *guarantees* apparent drift in mix-sensitive metrics during the fill-in period, independent of true
  performance.

### Evidence (the 2026-05-27 / 05-30 firings)

At investigation: **8 open positions, all favorable** (+0.85R … +4.16R, none near stop) — i.e. the
winner-mass is sitting in the open set, excluded from the comparison. The two persistent Bonferroni
firers are exactly the censoring-distorted metrics:

| metric | censoring-sensitivity | observed | reads as |
|---|---|---|---|
| `mfe_r` | **sensitive** — high-MFE winners still open | ★ DRIFT (−1.3R) | censoring |
| `win_pnl` | **sensitive** — only ~2 closed winners (WR 5.7%×35) | ★ DRIFT (n=2 noise) | censoring |
| `WR` | sensitive — winners haven't closed | 5.7% (abstains, high var) | censoring |
| `mae_r` | **insensitive** — adverse excursion is early | flat (Δ+0.02R, p=0.50) | **losers normal** |
| `pnl_per_trade` | sensitive (loser-heavy closed set) | −$706 (abstains) | censoring |

`mae_r` flat is the tell: losers are behaving exactly as backtest predicts. Nothing is *degrading* —
the winners simply haven't finished riding.

### The discriminator (censoring-benign vs real degradation)

This is the safety bound that prevents the finding from becoming a "dismiss every firing" loophole.
A firing on `mfe_r` / `win_pnl` / `WR` is **censoring-benign** only if **ALL** hold:

1. **`mae_r` flat** (live ≈ backtest; not Bonferroni-significant). MAE is censoring-insensitive — if it
   drifts adverse, that is **real degradation**, caveat void.
2. **Open positions predominantly favorable** (majority riding toward target, not clustered toward
   stop). If open positions are adverse, the winner-mass is *not* merely deferred — real signal.
3. **Few closed winners** (`n_closed_winners` below the power threshold, ≈15). Once enough winners have
   completed their long rides, `mfe_r`/`win_pnl` are adequately powered and trustworthy.

If **any** fails → treat as real, proceed per the firing playbook (intensify / toward kill).
This is **falsifiable and time-limited**, not a blanket excuse.

### Expiry (the caveat must lift)

The censoring caveat is void as soon as **any** of:
- `n_closed_winners ≥ 15` (the sensitive metrics are now powered), OR
- forward-paper elapsed ≥ 2× max-hold past first fills (~6 weeks; censoring largely worked through), OR
- a censoring-**insensitive** drift appears (mae_r drift, open-position adverse cluster, or
  pnl_per_trade drift driven by *widening losers* rather than *missing winners*).

---

## Finding 2 — N_MIN(30) vs calibrated-N(50) under-powered band

The detector's `N_MIN = 30` is described in its own docstring (lines 16–17) as "the original safety
floor; **we ramp to N=50 in practice where the calibration was anchored**." The locked FP=12.1% /
TP=100% operating point (`drift_detector_calibration_verdict_2026-05-07.md`) holds **at N=50**. Below
50 the detector is under-powered and noisier than its headline FP.

- The **2026-05-27 anchor fired at n=30** (the absolute floor); the 05-30 corroboration at n=35. **Both
  in the under-powered [30,50) band.**
- Meanwhile `forward_paper_status.sh`'s LIMBO resolution rule **already** treats `n_live < 50` as
  **CONTINUE-only** ("below this nothing but CONTINUE applies"). 
- **Inconsistency:** the detector can emit exit-1/exit-4 (→ arm the two-firings auto-kill) at an `n` the
  resolution rule deems too thin to act on. A 06-07 firing at, say, n=45 would return exit 4 on two
  firings *both* below the calibrated N.

---

## Migration-trigger-1 review — did the playbook hold up?

- **Tier classification (deterministic):** worked. 3 metrics → TRIAGE-B by count; the 48h-budget-expiry
  escalation to C-depth fired mechanically. ✓
- **Directional-inconsistency branch:** worked — `mfe_r`↓ vs `win_pnl`↑ routed to "ambiguous → continue
  per cadence," the correct call. ✓
- **Gap found:** the playbook's 8 cross-checks have **no censoring check.** Had the firing been
  *coherent-low* (e.g., `mfe_r` low **and** `win_pnl` low — which occurs once a few winners close below
  backtest R), the playbook's directional-consistency branch would have read "real signal → intensify
  cadence," escalating toward auto-kill on what is still censoring. **The playbook can misclassify
  coherent-censoring as degradation.** This is the substantive playbook-hardening output.

---

## Proposals (PROPOSED — ratify before ~2026-06-07; none auto-applied)

1. **Playbook cross-check 9 (censoring).** Add to
   `drift_firing_investigation_decision_rule_2026-05-08.md`: before treating any `mfe_r`/`win_pnl`/`WR`
   firing as degradation, run the Finding-1 discriminator (mae_r flat + open positions favorable +
   n_closed_winners < 15). All hold → censoring-benign, continue per cadence **regardless of
   direction-coherence**. Any fail → real, proceed to kill path.
   → **RATIFIED 2026-05-30** as the dated Amendment in that rule (additive; migration-trigger-1).

2. **Auto-kill requires n≥50 (align detector with calibration + resolution rule).** A drift firing
   counts toward the two-firings auto-kill (wrapper exit 4 arming) **only at n_live ≥ 50**. Firings in
   `n∈[30,50)` remain **investigation-grade** (exit 1, Telegram WARN, history-logged) but do **not** arm
   the auto-kill anchor. Consequence: the **2026-05-27 anchor (n=30) would not be a valid auto-kill
   anchor** — the clock would start only at the first firing with n≥50. This is not loosening the kill
   bar; it makes the detector consistent with two **already-locked** decisions (calibration N=50,
   resolution n<50→CONTINUE).
   → **DEFERRED to milestone-2.** Even though it aligns with locked anchors, changing the auto-kill
   arming-N is *re-tuning* the detector, which `drift_detector_time_to_detection_verdict_2026-05-08.md`
   forbids within this milestone ("❌ Reframing the bands… fresh pre-registration in a future milestone").
   Logged as an m2 candidate, NOT applied now. (Mechanical enforcement is also a code change to the
   decision-grade wrapper/detector + tests — a tracked follow-up, not a blind edit.)

3. **Mark-to-market un-censoring (optional, rigorous).** Add `held_hours` + an unrealized-R snapshot for
   open positions so the detector can compare on a censoring-matched basis. Heavier; tracked, not
   required for 1–2.

### DoF / goalpost-moving guard

Changing detector behavior mid-forward-paper is a researcher degree of freedom and must be handled with
care. These proposals are defensible **because**: (a) they are justified by a structural argument
(time-censoring + power) **independent of the current firing's direction or PnL sign**; (b) they make
the system *more* consistent with thresholds locked *before* forward-paper began (N=50, n<50→CONTINUE),
rather than inventing a new bar; (c) they are pre-registered **before** the data event they govern
(~06-07). They must be **ratified/locked before that date** or they become post-hoc and inadmissible.
**The auto-kill mechanism is preserved, not defeated** — a censoring-insensitive degradation
(mae_r drift, adverse opens) is decision-grade immediately, at any n.

---

## Actionable for the ~2026-06-07 watch

When the drift wrapper next fires ≥7d after 2026-05-27 (→ exit 4 candidate under current rules):
1. **Do not treat exit 4 as decision-grade until the Finding-1 discriminator is run.** If mae_r is flat
   and open positions are favorable and closed winners are still few → censoring-benign, continue.
2. Proposal 2 is **deferred to m2** (re-tuning locked this milestone), so the n<50 caution is enforced
   **manually**: at exit-4 the operator-in-loop gate runs cross-check 9 (now ratified in the playbook)
   before executing, and notes whether both firings were n≥50.
3. A kill proceeds only if the discriminator shows **real** degradation (mae_r drift / adverse opens /
   loser-widening pnl drift).

## Cross-references

- `results/drift_firings/2026-05-27-B-general-lown-censoring.md` — the firing this finding reviews
- `results/drift_firing_investigation_decision_rule_2026-05-08.md` — playbook (proposal 1 target;
  migration-trigger-1 + 3)
- `results/drift_detector_calibration_verdict_2026-05-07.md` — N=50 anchor, FP/TP operating point
- `results/drift_detector_time_to_detection_verdict_2026-05-08.md` — two-firings ≥7d auto-kill rule
- `results/auto_kill_execution_decision_rule_2026-05-08.md` — exit-4 execution (operator-in-loop gate)
- `scripts/live_vs_backtest_drift.py` — detector (N_MIN=30; closed-only loader; no holding-time field)
- `CLAUDE.md ## Forward-paper go/no-go criteria` — power floor (n<60d uninterpretable)
