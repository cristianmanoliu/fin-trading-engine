# Research Backlog — Closed Items Ledger

**Purpose:** Durable answer to the recurring question *"do we have anything solid in
backtest that we never managed to test live?"* — i.e., is there a research backlog of
promising-but-undeployed strategies waiting for an engine?

**Answer (updated 2026-06-09): Effectively yes, but with a correction.** Every backtest
finding that scored well is already on a live or shadow engine; everything else was tested
and rejected or judged a trade-off. **Correction (2026-06-09):** the MACD/RSI "REJECT"
verdicts were contaminated by the phantom-long bug (`d1d0fae`) and, re-run on the fixed
binary, **flip to CANDIDATE** (MACD +18%, RSI +79% 3/3). So the search was not *validly*
closed — two cells were mis-filed. **However, the closure conclusion still holds for a
deeper reason:** on the clean overfit matrix the family still fails the multiple-testing
haircut (DSR(34)=0.658 ≪ 0.95, PBO 0.52), and the strongest new candidate (RSI) shares the
live config's crash-dependent / bull-year-bleeding regime profile rather than diversifying
it. Adding candidates *widens* the haircut the live edge already fails. Forward-paper
carries the burden of proof. See `results/alt_signals_phantom_corrected_verdict_2026-06-09.md`
and `results/backtest_overfit_pbo_dsr_verdict_2026-05-29.md`.

This ledger is a **navigation aid**, not a decision document. The data of record is the
per-sweep verdict files in `results/`. It does not authorize any deployment, alter any
locked decision rule, or propose follow-up sweeps (those require fresh pre-registration).

---

## What is currently on an engine (the deployed surface)

Source of truth: `deploy/systemd/paper-live@.service` (`--shadow` flag).

| Config | Role |
|---|---|
| EMA 9/21, 4H, shorts, 6:1 RR, wick stop, mh504 | **LIVE** |
| EMA 5/15 mh336 / mh504 | Shadow |
| EMA 5/21, 7/14, 10/30, 12/26, 21/50 (all mh504) | Shadow |
| Bollinger 20/2.0σ mh504 (only non-EMA) | Shadow |

All EMA-family + Bollinger cells that any sweep liked are **already** being forward-tested
as shadows. That is why there are 8 shadows. See `docs/OPERATOR_HANDBOOK.md` for the
promotion-readiness status of each (all promotion-LOCKED under the charter freeze until
~2026-08-06).

---

## Closed backlog: tested, scored well enough to consider, NOT deployed — and why

Everything below was a plausible "put it live" candidate at some point. Each was tested at
the **production cost model** (slip=5bp / fee=10bp / mh504 / funding=CSV) and resolved.
None is an open candidate.

### Alternative entry signals — CANDIDATE post-phantom-fix (was contaminated REJECT)
| Signal | Mean NET vs baseline (CORRECTED) | Walk-forward | Status | Doc |
|---|---|---|---|---|
| MACD 12/26/9 cross | **+18%** (+$348k) | 2/3 windows | **CANDIDATE (pending fresh pre-reg)** | `results/alt_signals_phantom_corrected_verdict_2026-06-09.md` |
| RSI-14 cross-50 | **+79%** (+$530k) | 3/3 windows | **CANDIDATE (pending fresh pre-reg)** | same |

> ⚠️ **Corrected 2026-06-09.** The prior −78% / −36% REJECT numbers were produced on the
> phantom-long-buggy binary (`d1d0fae` fix): MACD/RSI ignored `--side-filter short` and
> booked LONG fills (~44% of RSI trades). On the fixed binary both **beat baseline** and
> trip the locked "WALK-FORWARD CANDIDATE" rule (RSI also 3/3 → diversifier branch). They
> are **no longer settled REJECTs** — they are candidates awaiting fresh pre-registration.
>
> **But this does NOT re-open deployment**, for two independent reasons proven the same day:
> (1) on the clean overfit matrix (`overfit_returns_matrix_2026-06-09_postfix.csv`) the
> family still fails — DSR(34)=0.658 ≪ 0.95, PBO 0.52; adding RSI/MACD only widens the
> haircut the live config already fails. (2) RSI's edge, though broad across symbols (max
> 7.9%), has the **same crash-dependent / bull-year-bleeding** regime profile as the live
> class (2022 +49%, 2020-21 + 2023 negative) — it is not a diversifier, and the
> regime-timing thread already proved that flaw is unfixable by overlay. The shadow harness
> still only wires EMA-cross + Bollinger; promotion would require harness work + a fresh
> pre-reg + an independent walk-forward/overfit pass on out-of-sample data. Deferred to the
> next research window (post-2026-08-06 freeze), behind the pre-committed EMA-5/15 shadow.

### Exit-mechanism changes — TRADE-OFFS, not added edge (structurally untestable by current shadows)
| Mechanism | Best cell | vs baseline | What it buys | Doc |
|---|---|---|---|---|
| Multi-level TP (partial profit) | 4R / 0.5 frac | −18% mean (3/3 positive) | ~70% W2 variance reduction; W3 stays positive | `results/mltp_exit_sweep_2026-05-19.md` |
| Trailing stop | loose 3R | −9% mean | cheap downside parachute at high MFE | `results/trailing_stop_sweep_2026-05-19.md` |

Both are clean **losses vs baseline** under the current setup (no per-window-variance
constraint). MLTP doesn't break the strategy — it gives up 18–41% of mean NET for tighter
variance; tighter trails (≤1R) catastrophically break the 2025–26 window. **These are the
one class the current shadows cannot express** (the harness uses a fixed single-TP
wick-stop exit). They are filed as documented options *if a future stage imposes a
monthly-loss or drawdown constraint* — not as edge to deploy now.

### Entry filters — REJECTED (degrade or break W3)
| Filter | Result | Doc |
|---|---|---|
| 1D-bias confluence (4 EMA pairs) | All 4 variants −23% to −59%; clean refutation | `results/confl_1d_bias_sweep_2026-05-19.md` |
| Volatility-regime filter (6 cells) | Tight loses, loose ties; 05-07 SUPPORTIVE did NOT replicate at slip=5 | `results/vol_filter_sweep_2026-05-19.md` |

### Side filter — shorts-only DECISIVELY correct
| Cell | Mean NET | Verdict | Doc |
|---|---|---|---|
| shorts (LIVE) | **+$295k** (3/3) | reference | `results/side_filter_validation_2026-05-19.md` |
| both-sides | +$194k (2/3) | dilutes −34% | same |
| longs-only | **−$135k** (1/3) | catastrophic, W3 −$380k | same |

The ~$430k/window short-vs-long asymmetry is mechanism, not noise. Both-sides and
longs-only are firmly rejected.

### EMA × timeframe — the positive cell IS already a shadow
| Finding | Disposition | Doc |
|---|---|---|
| 4H 5/21 nominally beats LIVE +34% | **Deployed as shadow `alt5-21-504`** | `results/ema_tf_exploratory_grid_2026-05-19.md` |
| 1D timeframe weaker than 4H; MACD-period EMAs mid-pack | Mid-pack cells also deployed as shadows | same |

The only nominal EMA winner is on an engine. Its +34% could be real edge or a
multi-comparison artifact (it carries 28% more trades = more cost surface) — forward-paper
as a shadow is exactly how that gets resolved.

### Composite / funding / cross-exchange
| Item | Verdict | Note | Doc |
|---|---|---|---|
| Funding-cross (Cat F1), co-cross (F2), F1×F2 composite (G), universe-57 (G') | **REJECT** | no edge | `results/cat_f1_*`, `cat_f2_*`, `cat_g_*` (2026-05-07/08) |
| Bybit cross-exchange replication (Cat X) | **ROBUST** | *robustness check* — confirms the existing edge replicates 1.05× on Bybit; NOT a new strategy | `results/cat_x_bybit_replication_verdict_2026-05-08.md` |

---

## The standing caveat

The overfit gate (PBO/DSR, `results/backtest_overfit_pbo_dsr_verdict_2026-05-29.md`)
returned **FRAGILE** on the live config: PBO 0.47, DSR(34)=0.64 (<0.95), degradation
slope −0.87. The live edge is significant *in isolation* (PSR 0.97) but does **not** clearly
survive the multiple-testing haircut of the 34-config search. The correct response is not
"find more candidates" — it is the opposite: **the search is done; forward-paper is the
arbiter.** Each additional candidate widens the haircut.

## When a new candidate is legitimate

Not from this backlog (it is closed). The next legitimate candidate is the **promotion of an
existing shadow** — the EMA 5/15 pair (`alt5-15-504` / `-336`), the pre-committed first
candidate for the next research arc — after (a) the charter freeze lifts (~2026-08-06) and
(b) live forward-paper resolves (≥150 trades AND ≥60 days). Promotion then runs the same
gauntlet the live config did: its own walk-forward + overfit gate on out-of-sample data,
not these forward numbers.

---

*Last reviewed: 2026-06-03. If you are re-asking this question, re-scan `results/INDEX.md`
for any sweep dated after this review before trusting the ledger.*
