# Batch-3 candidate search (#21–#25) — SYNTHESIS (2026-06-10)

**Status:** CLOSED. All 5 resolved same-day. **4 NO-GO · 1 PASS-on-locked-bar (MARGINAL
LEAD, not deployable).** Live config / VPS / forward-paper untouched throughout.

**Pre-reg:** `results/strategy_candidates_batch3_2026-06-10.md` (locked before first run,
commit `2404a60`). Scope: 1 open ledger item + 4 strategy STRUCTURES absent from both
closed searches (price-only + 20-candidate orthogonal).

## Ledger

| # | candidate | verdict | one-line why |
|---|---|---|---|
| 21 | RSI/MACD deep validation | **NO-GO (both)** — ledger item CLOSED | RSI = live edge in a different hat: corr 0.844, 3/7 years, edge all 2022+2025; MACD below baseline everywhere |
| 23 | BTC→alt lead-lag | **NO-GO** (0/12 cells) | fully arbed: 2–6bp gross vs 15bp cost, t≈0; first-run t=37 was a resample-label look-ahead (documented trap) |
| 22 | ETH/BTC RV pair | **NO-GO** | grid-mean Sharpe −0.157 vs 0.8 bar; ratio trends but too slow for 30bp pair costs; fading it actively loses |
| 25 | failed-pump cascade short | **PASS locked bar → MARGINAL LEAD** | n=515 de-survivorship: mean +2.38%, median +6.74%, t=2.36, drop5 +0.75%, 5/7 yrs, holds ex-2025 — but thin cushion, 2026 negative, tiny capacity, daily-bar sim |
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
3. **#25 is the only structure that found anything** — and it's the one that bets WITH
   the established mechanism (overcrowded alt longs collapsing) on the de-survivorship
   universe, not a new information axis. Consistent with the whole research arc: the
   only live edges in this market class are short-side stress harvests.
4. **#24 closes the vol-timing expression space.** Directional gates failed (20-candidate
   search); the direction-free harvest now failed too. "Axes mark WHEN, not WHICH-WAY"
   is final — there is no third expression.

## Disposition

- **#25 recorded as a milestone-2 lead** alongside #15/#16, with locked follow-ups
  before it can even be pre-registered for a shadow: intraday (1m) re-sim on the liquid
  subset, panic-slippage stress, 2026 forward re-check. NO shadow now, NO deployment,
  no live-config change — charter freeze applies.
- Everything else: closed permanently. The RSI/MACD backlog entry is settled (no longer
  "pending fresh pre-reg").
- **Operate-and-wait remains the standing recommendation.** Forward-paper is still inside
  its 60d/150-trade power floor; nothing in this batch changes that.
