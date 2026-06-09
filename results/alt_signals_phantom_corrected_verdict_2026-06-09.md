# Alt-signals (MACD/RSI) re-verification on the phantom-fixed binary — verdict (2026-06-09)

**Status:** Research re-verification. Triggered by the discovery that the side-filter
phantom-long class bug (fixed in `d1d0fae`, 2026-06-09) contaminated every non-EMA
short-only backtest, including the MACD/RSI "REJECT" verdict on file
(`alt_signals_retest_2026-05-19.md`) and two columns of the overfit matrix
(`overfit_returns_matrix_2026-05-29.csv`). Those verdicts were produced ~3 weeks
**before** the fix and are invalid for the affected modes.

This doc records the corrected numbers, the verification that the fix actually works,
and the disposition. **No deployment. No shadow promotion.** It corrects the ledger; it
does not authorize anything (charter freeze to ~2026-08-06 + FRAGILE overfit verdict both
still bind).

---

## 1. Root-cause verification — the fix eliminates phantom longs (PASS)

`tasks/verify_phantom_fix.sh` built the **pre-fix** binary (`d1d0fae^`, via temp git
worktree) and the **post-fix** binary (HEAD), ran RSI-mode + MACD-mode under
`--side-filter short` on 5 liquid symbols (BTC/ETH/SOL/BNB/XRP, 5y), counted journal
sides:

| Mode | Binary | open LONG | open SHORT |
|---|---|---:|---:|
| RSI | **pre-fix** `d1d0fae^` | **752** | 965 |
| RSI | **post-fix** HEAD | **0** | 1516 |
| MACD | **pre-fix** `d1d0fae^` | **1033** | 1032 |
| MACD | **post-fix** HEAD | **0** | 1500 |

Pre-fix RSI was ~44% phantom longs (752/1717). Post-fix: **zero LONG opens** under a
short-only filter; short count rises because the long-block no longer steals candle
events. Bug mechanism directly observed; fix confirmed. (Baseline EMA cross was never
contaminated — it always had the inline filter; the overfit matrix `LIVE` column is
byte-identical pre/post, Sharpe 0.2273 both runs.)

---

## 2. Corrected walk-forward numbers — both flip REJECT → CANDIDATE

`scripts/alt_signals_retest_sweep.sh` re-run on the fixed binary (fresh build per
`p4_fresh_oos.sh`). Production cost model: slip=5 / fee=10 / mh504 / 4H / short / 57 sym /
3 OOS walk-forward windows (W1 2023-05→2024-04, W2 2024-05→2025-04, W3 2025-05→2026-04).

| Cell | W1 | W2 | W3 | Mean | Wins | Trades | vs LIVE |
|---|---:|---:|---:|---:|:---:|---:|:---:|
| **BASELINE EMA 9/21** | +175,021 | +645,812 | +65,044 | **+$295,292** | 3/3 | 5,572 | reference |
| **MACD 12/26/9** (post-fix) | −101,442 | +378,385 | +767,806 | **+$348,250** | 2/3 | 8,531 | **+18%** |
| **RSI-14 cross-50** (post-fix) | +17,766 | +903,662 | +667,491 | **+$529,640** | 3/3 | 9,416 | **+79%** |

Contaminated (pre-fix) numbers for contrast: MACD +$64,106 (−78%, 12,557 trades), RSI
+$188,427 (−36%, 11,122 trades). The fix removed **4,026 MACD** and **1,706 RSI** phantom
trades (counts dropped toward baseline) and *raised* mean NET — because in the bear-ish
W2/W3 the phantom longs had been getting **stopped out for real losses** that dragged the
reported short-strategy mean down. (Inverse of bb20, whose phantom longs booked fake
*wins* in the 2020-21 bull.)

Per the **LOCKED** decision rule in `alt_signals_retest_2026-05-19.md` (line 79: "either
cell beats baseline ≥10% across ≥2/3 windows → WALK-FORWARD CANDIDATE; shadow promotion
requires fresh pre-reg"), **both now qualify as candidates**, and RSI also trips the
diversifier branch (3/3 within/above baseline).

---

## 3. Overfit gate on the clean matrix — removing the bug makes the family WORSE, not better

Regenerated the 34-config × 57-sym × 64-month returns matrix on the fixed binary
(`results/overfit_returns_matrix_2026-06-09_postfix.csv`) and re-ran
`scripts/backtest_overfit_analysis.py`. The old matrix had 2 contaminated columns
(`rsi_14`, `macd_12_26_9`); the other 32 are EMA-signal (filter/exit variants) and were
clean.

| Metric | Contaminated (05-29) | **Clean (06-09)** | Direction |
|---|---:|---:|---|
| LIVE monthly Sharpe | 0.2273 | 0.2273 | identical (LIVE always clean) |
| PBO (CSCV) | 0.4698 | **0.5192** | **worse** (crossed 0.50) |
| DSR (N=34) | 0.6420 | **0.6578** | ~same, still ≪0.95 |
| PSR | 0.9690 | 0.9690 | identical |
| degradation slope | −0.8737 | **−0.9363** | worse |
| IS-best modal config | **rsi_14** (40.8% of folds) | **confl_13_34** (24.5%) | de-concentrated |

Two consequences:

1. **The contamination had been flattering the in-sample picture.** Phantom-inflated
   `rsi_14` was so strong in-sample it was the modal IS-best config in 41% of CSCV folds.
   Fixing it de-concentrates the search — but PBO *rises* to 0.52 (in-sample ranking now
   has ≈ zero out-of-sample predictive power) and the degradation slope steepens.

2. **DSR(34) is still 0.658 ≪ 0.95 on clean data.** RSI being a *legitimately* strong
   config does not rescue the strategy family — it is one more trial in the 34-config
   haircut. The live edge (PSR 0.97 in isolation) still does **not** survive multiple
   testing. The FRAGILE verdict (`backtest_overfit_pbo_dsr_verdict_2026-05-29.md`) holds
   and is, if anything, reinforced on clean data.

---

## 4. Is RSI's +79% robust or an artifact? — broad across symbols, but SAME crash-dependence as LIVE

`tasks/verify_rsi_concentration.sh` ran post-fix RSI on the full 57-sym universe, 5y,
decomposed by symbol and calendar year. Total NET $1,677,183 / 14,881 trades.

**Symbol breadth — robust (not one-symbol-driven):**
- Max single-symbol share **7.9%** (GRTUSDT) — far below the >40% kill-flag.
- 47 symbols positive / 10 negative. Edge is distributed across the universe.

**Time concentration — same structural flaw as the live class:**

| Year | NET | % of total |
|---|---:|---:|
| 2020 | −204,783 | −12.2% |
| 2021 | −214,016 | −12.8% |
| 2022 | +816,074 | +48.7% |
| 2023 | −449,484 | −26.8% |
| 2024 | +709,359 | +42.3% |
| 2025 | +1,020,031 | +60.8% |

RSI makes all its money in 2022 (crash) + 2024-25 and **bleeds in 2020-21 (bull) and
2023.** This is the *same* short-only crash-dependent profile as the live EMA config (which
makes ~52% of 5y P&L in 2022 and is back-to-back-red in bulls). RSI is **not a
diversifier** — it is the identical regime bet with a different trigger, and it adds a
*new* losing year (2023) the live config handles better. The 3/3 walk-forward headline
hides this: the May→April windows (W1-W3) happen to straddle the calendar-2023 bleed
rather than land on it.

The regime-timing thread (`results/regime_switch_fullstop_verdict_2026-06-08.md` +
gate/breadth verdicts) already proved this back-to-back-red-in-bulls flaw is **not
fixable** by any overlay on the always-short class. RSI inherits that unsolved flaw; it
does not escape it.

---

## 5. Disposition

| Question | Answer |
|---|---|
| Was the search validly "closed" per `RESEARCH_BACKLOG.md`? | **No** — it cited contaminated MACD/RSI REJECTs as settled fact. |
| Do MACD/RSI beat baseline post-fix? | **Yes** — MACD +18% (2/3), RSI +79% (3/3). Both are WALK-FORWARD CANDIDATES per the locked rule. |
| Does RSI survive the overfit gate? | **No.** Clean DSR(34)=0.658 ≪ 0.95; clean PBO 0.52. Adding RSI widens the haircut the live family already fails. |
| Is RSI's edge robust across symbols? | **Yes** (max 7.9%, 47/10 breadth). |
| Is RSI a *diversifier* vs LIVE? | **No** — same 2022-crash-dependent, bull-year-bleeding regime profile; adds a new bad year (2023). |
| Does this authorize a shadow / deploy? | **No.** Charter freeze (~2026-08-06) + FRAGILE overfit + locked "fresh pre-reg required" rule all bind. |

**Action taken:** correct the contaminated docs (this verdict + corrections appended to
`alt_signals_retest_2026-05-19.md`, `RESEARCH_BACKLOG.md`, and a contamination note on
`backtest_overfit_pbo_dsr_verdict_2026-05-29.md`). **No deployment. No promotion.** MACD/RSI
move from "REJECTED" to "candidate-pending-fresh-pre-reg" in the ledger — but the overfit
gate and the shared crash-dependence mean they are weak candidates at best, to be
considered (if at all) only in the next research window alongside the pre-committed
EMA-5/15 shadow promotion.

## Artifacts (all reproducible; gitignored journals regenerable)
- `tasks/verify_phantom_fix.sh` — pre/post-binary phantom-long count (PASS)
- `tasks/verify_rsi_concentration.sh` — per-symbol + per-year decomposition
- `scripts/alt_signals_retest_sweep.sh` — corrected walk-forward (re-run on fixed binary)
- `results/overfit_returns_matrix_2026-06-09_postfix.csv` — clean overfit matrix
- `results/alt_signals_retest_2026-05-19.csv` — overwritten with corrected numbers
