# Research Backlog — Closed Items Ledger

**Purpose:** Durable answer to the recurring question *"do we have anything solid in
backtest that we never managed to test live?"* — i.e., is there a research backlog of
promising-but-undeployed strategies waiting for an engine?

**Answer (as of 2026-06-03): No. The strategy-class search is formally closed.** Every
backtest finding that scored well is already on a live or shadow engine. Everything not
on an engine was tested and rejected (or judged a trade-off, not added edge). This is the
intended end-state, not a gap — `results/research_synthesis_2026-05-19.md` closes the
strategy-class search. Forward-paper now carries the burden of proof; adding more
candidates would only widen the multiple-testing haircut that already flagged the live
edge FRAGILE (`results/backtest_overfit_pbo_dsr_verdict_2026-05-29.md`).

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

### Alternative entry signals — REJECTED at production cost
| Signal | Mean NET vs baseline | Walk-forward | Verdict | Doc |
|---|---|---|---|---|
| MACD 12/26/9 cross | **−78%** (+$64k) | 2/3 windows | **REJECT** | `results/alt_signals_retest_2026-05-19.md` |
| RSI-14 cross-50 | **−36%** (+$188k) | 2/3 windows | **REJECT** | `results/alt_signals_retest_2026-05-19.md` |

Re-tested specifically to check whether the lower cost model would flip their 2026-05-07
REJECT. The sign flipped negative→positive (lower slip lifts everything) but the relative
position held: both lose to baseline, neither clears the ≥10%-beat bar, neither is 3/3 so
neither qualifies as a low-correlation diversifier. **The shadow harness only wires
EMA-cross and Bollinger — adding MACD/RSI would require harness work to validate an
inferior signal.** Not worth it.

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
