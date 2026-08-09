# Close-out addendum — 2026-08-09

**Status: POST-HOC RE-ANALYSIS. Not a verdict. Not a pre-registered result.**

This document was produced *after* the run terminated (2026-08-04) and *after*
both v2 routes closed (2026-08-05), on data those decisions already used. It is
therefore exempt from none of the charter's warnings and carries none of a
verdict's authority. It is filed because it (a) corrects one factual claim in
the record, (b) adds one structural finding the record never stated, and
(c) independently reproduces the record's central number by a different route.

**It does not reopen the project.** Its conclusions are *more* negative than the
existing record on the question that matters, not less.

Occasion: the operator questioned whether the closure was correct, on the
grounds that the book "is profitable, even a little, live/shadows." That premise
is false for live and misleading for shadows (§1). Testing it produced §2–§4.

---

## 0. What was run

All numbers below are recomputed from raw journals, not carried over from prior
documents.

| | |
|---|---|
| Live book | `results/journal_cache/*.jsonl`, flat glob, 134 closes |
| Shadows | `results/journal_cache/shadow/<algo>/*.jsonl`, 8 cohorts |
| Backtest | `cmd/backtest`, LIVE config, deployed-16, merged 1m CSVs 2020-01 → 2026-07 |
| Backtest config | `--signal-tf 4H --side-filter short --max-hold-hours 504 --funding-csv-dir data/funding --fee-bps 10 --stop-slippage-bps 5`, `target_rr: 6.0`, `ema_mode: true`, stake $1,000 |
| Backtest output | **2,887 trades**, 2020-01-20 → 2026-07-31, NET **+$715,942** |
| Regime classification | **BTC daily closes only** — never strategy P&L, so classification cannot be circular |

Scratch artifacts (journals, merged CSVs, per-symbol configs) are in the session
scratchpad and are **not** committed; the commands above regenerate them in ~7s
per symbol.

---

## 1. The premise: live was not profitable; the shadows' profit is 2-3 days

**Live: 134 trades, WR 17.9%, gross +$11,396, costs $13,047, NET −$1,626.58.**
Costs were 114% of gross. There is no reading of the live book as profitable.

**Shadows: all 8 positive, +$1,950 to +$77,722 — and all 8 fail the
pre-registered anti-cluster gate.** Applying Gate 9 of
`shadow_promotion_decision_rule_2026-05-27.md` (drop the 2 best calendar days;
require robust > 0 AND robust > 0.4 × raw) to the *final* data:

| cohort | n | raw | robust (drop top-2 days) | G9a | G9b |
|---|---:|---:|---:|:--:|:--:|
| alt5-15-504 | 213 | $77,722 | $12,236 | PASS | **FAIL** |
| alt5-15-336 | 232 | $69,987 | $4,588 | PASS | **FAIL** |
| alt12-26-504 | 80 | $31,030 | $7,121 | PASS | **FAIL** |
| alt7-14-504 | 136 | $30,952 | −$8,009 | **FAIL** | **FAIL** |
| alt10-30-504 | 70 | $25,668 | $8,767 | PASS | **FAIL** |
| alt5-21-504 | 127 | $20,190 | −$15,676 | **FAIL** | **FAIL** |
| bb20 | 157 | $5,001 | −$34,804 | **FAIL** | **FAIL** |
| alt21-50-504 | 17 | $1,950 | −$13,383 | **FAIL** | **FAIL** |
| *live* | 134 | −$1,627 | −$40,610 | FAIL | FAIL |

**0 of 9 pass.** Every cohort's profit is June 2026, and within June, 2-3 days.
July was negative for 6 of 9. The cohorts that looked best on June data
(alt5-15-336 −$31,753; bb20 −$28,183) were the **worst** in the final 30 days —
the regression the 05-27 rule was written to wait for, arriving on schedule.

The mechanism is frequency, not edge: alt5-15-504 fires 213 trades to live's 134
and catches more of the same few crash days at the same ~14bp cost.

**`bb20` is contaminated and must not be cited in either direction:** 29 LONG
fills in a short-only book — the side-filter phantom bug
([[project_sidefilter_phantom_bug]], fixed d1d0fae, after this data was written).

---

## 2. NEW FINDING — the class is a short-volatility product (never previously stated)

The record nowhere characterises the class's regime dependence. It should have.

**Non-overlapping calendar quarters, 2020-2026 (26 independent observations),
classified by BTC quarterly return alone:**

| BTC regime | quarters | positive | mean NET | median NET |
|---|---:|---:|---:|---:|
| **DOWN** (< −5%) | 9 | **9/9 (100%)** | **+$72,071** | +$74,160 |
| FLAT (−5% … +15%) | 7 | 4/7 (57%) | +$17,863 | +$4,579 |
| **UP** (> +15%) | 10 | 4/10 (40%) | **−$7,761** | −$14,295 |

**correlation(BTC quarterly return, strategy quarterly NET) = −0.588.**

Rolling 3-month windows agree: CRASH (BTC < −10%) 95.5% positive, median
+$72,639; BULL (BTC > +15%) 56.2% positive, median +$4,135.

### 2a. The operator's question, answered directly

*Does the class make money in non-crash windows?* **Yes — weakly.**
Pooled non-crash (CALM + BULL) rolling 3-month windows: **34 of 53 positive,
median +$8,794, mean +$10,139, sign-test one-sided p = 0.027.**

So the class is *not* purely a tail bet. But its central tendency outside
crashes is ~$9k per quarter in $1k-risk backtest units, against a cost stack
that consumed 114% of live gross — and 6.5-year concentration is severe:

| | share of total 6.5y NET |
|---|---:|
| top 1 day | 7.0% |
| top 5 days | 30.8% |
| top 10 days | 53.9% |
| **top 20 days** | **93.8%** |

1,324 trading days produced $715,942; 20 of them produced 94% of it. This is
structural to 6:1 RR at ~21% WR, not a forward-paper artifact.

---

## 3. Cheaper venue: flips the sign, does not make the edge signable

Recomputing the **live** book at alternative cost stacks (fee round-trip on
notional; slip on losers only, matching engine semantics):

| fee/slip | live NET | edge = grossR − costR | t | bootstrap P(mean ≤ 0) |
|---|---:|---:|---:|---:|
| 10/5 (as run) | −$1,564 | −0.0117 R | — | — |
| 8/5 | +$271 | +0.0020 R | — | — |
| 6/4 | +$2,863 | +0.0214 R | — | — |
| 4/3 | +$5,455 | +0.0407 R | +0.19 | 0.431 |
| 2/2 (maker-equiv) | +$8,047 | +0.0601 R | +0.28 | 0.396 |
| **0/0 (free)** | **+$11,396** | **+0.0850 R** | **+0.40** | **0.345** |

Break-even lands between 8 and 10 bp — **independently reproducing §1 of
`v2_lessons_and_design_2026-08-04.md` by a different route.** That document's
number was right.

**The decisive addition is the power column.** Trades required for t = 2.0:

- at 4bp/3: **14,643 trades ≈ 34 years** at 1.18 trades/day
- at 2bp/2: **6,717 trades ≈ 15.6 years**
- **at zero cost: 3,337 trades ≈ 7.7 years**

At a physically impossible zero-cost venue the live edge is t = +0.40 with a
**34.5% probability the true mean is ≤ 0.** Cost is not the binding constraint
at the live gross level; removing 100% of it leaves an edge that cannot be
signed inside a decade.

**Root cause, one line:** live gross realized **27% of backtest gross**
(+0.085 R vs +0.317 R, identical config). The same 6.5y backtest shows
t = +5.68 at maker costs. Backtest significance is granite; live significance is
noise. Fee reduction moves a term that was never what failed.

---

## 4. What is corrected, and what stands

### Corrected

- **`v2_lessons_and_design_2026-08-04.md` §1, final line:** *"but it **is** a
  much better starting point than 'the class is dead.'"* — **This is wrong, and
  it is the one substantive error found.** §3 above shows the 8bp gap is not the
  binding constraint: at *zero* cost the edge remains unsignable (t = +0.40,
  P(mean ≤ 0) = 0.345, 7.7 years to significance). A venue at 8bp does not
  produce a better starting point; it produces the same unprovable coin-flip
  with a positive sign. The sentence should read: *fixing fees does not improve
  the starting point, because fees were not what made the edge unprovable.*
  (The same document's own preceding sentences — "statistically
  indistinguishable from zero", "not a business" — are correct and unaffected.)

### Stands, reproduced independently

- Break-even 8bp vs 10bp executed (§3 here reproduces it).
- Gross edge real, net negative, costs 114% of gross.
- Maker execution NO-GO — **not re-run here**, and correctly so: the adverse-
  selection mechanism (a resting sell-limit for a SHORT fails to fill exactly
  when price gaps away = exactly what a winner is) is directional and not
  fixable by tuning. `maker_execution_verdict_2026-08-05.md` stands unmodified.
- Wider stops — settled, not re-run.
- The viability frontier's screening rule and its lever-1 argument (cut fees →
  live gross lands *on* the boundary, inside its own noise band). §3 is that
  argument, measured.

### Added

- The class is a short-vol product, r = −0.588 vs BTC, 9/9 in BTC-down quarters
  (§2). Never previously stated in the record.
- Non-crash windows are weakly positive, p = 0.027 (§2a) — so "it only works in
  crashes" would be too strong a closure rationale, and was never the stated
  one.
- Power arithmetic at reduced cost stacks (§3) — the quantitative form of the
  frontier's lever-1 claim.

---

## 5. Disposition

**The closure stands, on strengthened grounds.** The honest one-line reason is
not "no edge outside crashes" (there is one, p = 0.027) and not "costs killed
it" (removing all costs does not rescue it). It is:

> The live edge is ~1/4 of backtest gross, statistically unsignable at any
> achievable cost basis, and 94% concentrated in 20 of 1,324 days.

**Do not re-open.** Nothing here is a promotion trigger, and §1 shows the
shadow-based case for revival fails the pre-registered gate 0/9.

### The one genuinely open question — and it belongs elsewhere

As a **standalone alpha book** this class is dead at any cost basis. As a
**hedge overlay** on a long book, the bar is different: an instrument that is
9/9 positive in BTC-down quarters with r = −0.588 does not need provable
positive expectancy to be worth holding — a hedge is permitted to cost
something. That is a different product with a different success criterion.

It belongs to **fin-equity-lab** (which has a long book to hedge), not to a
revival here, and it would require its own pre-registration — including how a
94%-in-20-days payout profile behaves when sized as an overlay, which is not
answered by anything in this repo.

---

## Reproduction

```bash
# live + shadow book, anti-cluster gate (§1)
#   flat glob for live; archive/ MUST stay excluded (25x overstatement trap)

# full-history backtest (§2, §3): per-symbol config with
#   csv_path -> merged 1m CSVs, ema_mode: true, target_rr: 6.0
go build -o /tmp/bt ./cmd/backtest
/tmp/bt --config <per-symbol>.yaml --signal-tf 4H --side-filter short \
        --max-hold-hours 504 --funding-csv-dir data/funding \
        --fee-bps 10 --stop-slippage-bps 5 --journal-dir <out>/<SYM>
# NOTE: target_rr is config-only; there is no --target-rr flag.

# regime classification from BTC 1m CSVs -> daily closes -> quarterly returns.
# Classification must never read strategy P&L.
```
