# #25 failed-pump follow-ups — pre-registered decision rule (2026-06-10)

**Status:** LOCKED before running. Executes the three follow-up requirements named in
`results/failed_pump_verdict_2026-06-10.md` plus two standing-gate checks. Research-only.
Outcome space: lead UPGRADED (eligible for shadow pre-reg at milestone-2), DOWNGRADED, or
KILLED. No outcome makes it deployable now.

## F1 — Intraday (1m) re-sim on the 1m-available subset

Trades whose symbol is in the local 57-symbol 1m universe re-simulated on 1m bars:
entry at first 1m open of entry day; stop/target touch via 1m high/low in TRUE sequence
(replaces the daily-sim's pessimistic stop-first tie-break); time exit last 1m close of
the hold window. Compare vs the daily sim RESTRICTED TO THE SAME SUBSET (apples-to-apples).

- **Bar:** subset 1m-sim mean net > 0 AND median > 0, AND 1m mean ≥ (daily-subset mean
  − 1.5pp). If subset n < 50, F1 is INFORMATIVE-ONLY (stated now, before seeing n).
- Fail → lead KILLED (daily-bar fills were load-bearing).

## F2 — Panic-slippage stress

Reprice the full 515-trade primary cell at 50bp/side (100bp RT) and 100bp/side (200bp RT)
on top of funding; also report the breakeven RT cost.

- **Bar:** mean net still > 0 at 200bp RT. Fail → lead DOWNGRADED to "liquid-subset-only"
  (revisit only with per-symbol liquidity filter).

## F3 — 2026 decay check

2026 YTD (n≈40, mean −2.5%) vs history: z = (2026 mean − pooled pre-2026 mean) /
SE(2026 mean).

- **Bar (flag, not kill):** |z| < 2 → consistent with variance, lead stands. z ≤ −2 →
  decay flag recorded; lead kept but milestone-2 pre-reg must include a fresh-window
  re-test before shadow.

## F4 — Diversifier check vs live book

Monthly #25 returns (primary cell) vs live-config monthly PnL (`results/live_journals`).

- **Bar:** |corr| < 0.5 (standing gate 3). Fail → lead DOWNGRADED (it would be the same
  bet as the live book; value case collapses).

## F5 — Listing-age decomposition (report-only)

Trades split by symbol age at entry (<90d, 90d–1y, >1y since first kline). No bar; checks
whether the edge is secretly the dead #12 new-listing family. Material concentration in
<90d → noted in verdict for milestone-2 design.

## Aggregate disposition (locked)

- All bars pass → **LEAD CONFIRMED**: #25 becomes the named priority for milestone-2
  shadow pre-registration (still nothing live).
- F1 or F2 fail → KILLED / DOWNGRADED as specified above.
- F3/F4/F5 only modulate the milestone-2 requirements; they cannot upgrade a failed F1/F2.
