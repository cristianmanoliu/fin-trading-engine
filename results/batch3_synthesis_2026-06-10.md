# Batch-3 candidate search (#21–#25) — SYNTHESIS (2026-06-10)

**Status:** CLOSED. All 5 resolved same-day. **FINAL TALLY: 5/5 NO-GO.** (#25's initial
PASS was overturned same-day by the locked F6 full-history re-run — a data-truncation
artifact; see the amended verdict + `failed_pump_followup_decision_rule_2026-06-10.md`.)
Live config / VPS / forward-paper untouched throughout.

**Pre-reg:** `results/strategy_candidates_batch3_2026-06-10.md` (locked before first run,
commit `2404a60`). Scope: 1 open ledger item + 4 strategy STRUCTURES absent from both
closed searches (price-only + 20-candidate orthogonal).

## Ledger

| # | candidate | verdict | one-line why |
|---|---|---|---|
| 21 | RSI/MACD deep validation | **NO-GO (both)** — ledger item CLOSED | RSI = live edge in a different hat: corr 0.844, 3/7 years, edge all 2022+2025; MACD below baseline everywhere |
| 23 | BTC→alt lead-lag | **NO-GO** (0/12 cells) | fully arbed: 2–6bp gross vs 15bp cost, t≈0; first-run t=37 was a resample-label look-ahead (documented trap) |
| 22 | ETH/BTC RV pair | **NO-GO** | grid-mean Sharpe −0.157 vs 0.8 bar; ratio trends but too slow for 30bp pair costs; fading it actively loses |
| 25 | failed-pump cascade short | **KILLED (F6)** | initial PASS (+2.38%, t=2.36) was a truncation artifact — listing-klines = 30-60d windows; full history (n=3,898): mean −0.99%, t=−3.05, 0/7 yrs; ex-90d −1.33%, t=−3.79. Edge = dead #12 family in disguise |
| 24 | vol-event two-sided breakout | **NO-GO** (0/3 gate families) | means positive (settlement +45bp) but median −31…−62bp, drop5 negative everywhere — breakout straddle = whipsaw-financed lottery ticket |

## Cross-cutting findings

1. **The honesty block keeps earning its keep.** #24 had three positive means (one at
   t=1.98 on n=9,340) and 5-6/7 positive years — and is still untradeable, because the
   median filled event loses and the top 5% carries everything. Mean/t/by-year alone
   would have shipped a lottery ticket. Same kill as #11/#12/#20.
2. **Two new audit traps documented (both caught before any conclusion):**
   - `pandas.resample().last()` left-edge labels = look-ahead factory (#23 first run:
     t=37 of pure contemporaneous co-movement). Right-edge labels mandatory.
   - `pd.Series(series, index=ts)` REINDEXES instead of assigning — silently produced a
     funding≡0 join in #25. Use `.values`.
3. **#25's rise and fall is the batch's defining lesson.** It passed the full honesty
   block (median, drop5, by-year, funding, de-survivorship, diversifier corr −0.02) and
   STILL died — because the universe itself was silently truncated to listing windows
   (data fetched for #12 reused without span verification). A third audit trap class:
   **verify the SPAN of inherited datasets, not just their schema.** The F5 listing-age
   decomposition (a report-only afterthought) was what exposed it; the F6 full-history
   re-fetch then showed the general strategy is significantly NEGATIVE (t=−3.05, 0/7
   years). Honesty checks on a biased universe validate the bias, not the strategy.
4. **#24 closes the vol-timing expression space.** Directional gates failed (20-candidate
   search); the direction-free harvest now failed too. "Axes mark WHEN, not WHICH-WAY"
   is final — there is no third expression.

## Disposition

- **All five candidates closed permanently.** #25 KILLED by locked F6 (full-history
  mean −0.99%, t=−3.05, 0/7 years). If its listing-window subgroup (+2.38%, t=2.36,
  n=515, first-90d-only) is ever revisited, it is a FRESH #12-family pre-registration
  at milestone-2 carrying the negative-general-strategy prior — not a batch-3 survivor.
- The RSI/MACD backlog entry is settled (no longer "pending fresh pre-reg").
- **Milestone-2 leads remain #15 + #16 only.**
- **Operate-and-wait remains the standing recommendation.** Forward-paper is still inside
  its 60d/150-trade power floor; nothing in this batch changes that.
