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

## F6 — Full-history re-run (ADDED after F5 exposed a data-coverage truncation; locked
## BEFORE the full-history fetch or any full-history number is observed)

F5 revealed `data/listing/klines` holds only ~30-60 days post-listing per symbol (it was
fetched for #12's listing-window study). #25 as run is therefore a NEW-LISTING pump-fail
strategy, not a general one. F6 decides which it is:

- **Fetch:** full daily klines + funding history, all 732 ever-listed perps, via
  `fapi.binance.com` public REST (paced ≤8 req/s, 60s backoff on 418/429 per the
  reconciler lesson — never tighter than the poll interval). Data gitignored.
- **Run:** identical locked #25 mechanics (P=25% primary, same trigger/stop/target/hold,
  70bp RT + funding) on (a) the FULL history, and (b) the full history EXCLUDING each
  symbol's first 90 days (the pure post-listing-window complement).
- **Bars (locked):**
  - (a) full-history primary cell passes the original #25 bar (mean>0 t>2, median>0,
    drop5>0, ≥5/7 yrs) → lead is GENERAL; framing upgraded.
  - (b) if (a) passes but the ex-90d slice fails (mean ≤0 or t<1), the edge is
    listing-window-only → lead reframed as "#12-family conditioned variant",
    milestone-2 priority DOWNGRADED below #15/#16 (inherits listing-crowding risks).
  - (a) fails → original PASS was a truncation artifact; lead KILLED, verdict amended.

## Aggregate disposition (locked)

- All bars pass → **LEAD CONFIRMED**: #25 becomes the named priority for milestone-2
  shadow pre-registration (still nothing live).
- F1 or F2 fail → KILLED / DOWNGRADED as specified above.
- F3/F4/F5 only modulate the milestone-2 requirements; they cannot upgrade a failed F1/F2.
