# Forward-paper close-out — 2026-08-04

**Status: TERMINATED EARLY by operator decision.** The pre-registered run was
scheduled to resolve ~2026-08-25. It is being stopped at day 88 of ~112, with
2 positions still open and the formal verdict never fired.

This document is the final record. It states what the run measured, what that
does and does not establish, and what was left unresolved. It is written
against `results/journal_cache/` as fetched 2026-08-04T18:30Z.

---

## 1. Final book

| | |
|---|---|
| Trades closed | **134** |
| First close | 2026-05-08T08:31:59Z |
| Last close | 2026-08-04T02:36:48Z |
| Calendar days | 88 |
| Wins | 24 |
| Win rate | **17.9%** |
| **Net P&L** | **−$1,626.58** |
| Gross P&L | +$11,420.10 |
| Fees | $9,176.84 |
| Slippage | $3,869.84 |
| **Costs / gross** | **114.2%** |

Stake $1,000/trade. Paper only — **zero real money was ever deployed.**

### Open at termination (2 positions, never resolved)

```
IMXUSDT  SHORT  2026-07-27T16:00:00Z  entry 0.1221  stop 0.1263262  target 0.0967428
APTUSDT  SHORT  2026-07-28T00:00:00Z  entry 0.5921  stop 0.6104098  target 0.4822412
```

Both were favorable at last mark (2026-08-02: +3.08R and +1.55R). Their
outcomes are unknown and will remain so. **The book above excludes them** —
if both had run to target the net would be positive; if both had stopped it
would be roughly −$3.7k. This is the single largest source of uncertainty in
the final number, and it is uncertainty the run was designed to eliminate by
letting the book self-resolve by 08-18.

---

## 2. The result

**Gross edge was real. Net edge was negative because costs exceeded it.**

The strategy made **+$11,420 gross** over 134 trades. It paid **$13,047** to
do so. That is the entire finding, and it was the finding the run existed to
test.

### Cost geometry — the binding constraint

| | |
|---|---|
| Median notional per trade | $56,381 |
| Max notional | $231,616 |
| Implied median leverage | **56×** |

The strategy sizes to risk, not notional: a $1,000 stake against a tight 4H
wick stop (median ~1.8% of price) requires ~56× implicit leverage. Fees are
charged on **notional × 2 sides**, not on stake. So a trade risking $1,000
pays fees on ~$56,000 of exposure, twice.

This is structural, not a parameter. It does not improve with a better entry
filter, a different R:R, or a regime overlay — all of which were tested and
rejected (see §4). It improves only with **lower fees per notional** (maker
orders, a cheaper venue) or **wider stops** (which changes the strategy into
something else that was not validated).

### Monthly path

| Month | n | Net |
|---|---|---|
| 2026-05 | 37 | −$27,020.71 |
| 2026-06 | 40 | +$28,414.84 |
| 2026-07 | 52 | −$1,379.61 |
| 2026-08 | 5 | −$1,641.10 |

The run was net-positive as recently as 2026-07-29 (+$3,410.83) and crossed
negative on 2026-08-02. **Neither crossing was informative** — at WR 17.9%
with 6:1 R:R, single trades move the book by more than a month of drift. The
sign of the final number is close to a coin flip; the **ratio** (costs 114%
of gross) is the stable result.

### Per-symbol

Five symbols carried all the profit; eleven lost money.

```
1000SHIBUSDT  n= 6   +$19,496.26        APTUSDT     n= 9    −$892.37
ADAUSDT       n= 9   +$18,051.59        ENSUSDT     n= 8  −$1,546.02
GRTUSDT       n= 6    +$7,512.98        1INCHUSDT   n= 9  −$1,633.02
XLMUSDT       n= 7    +$4,985.93        BCHUSDT     n= 5  −$1,758.50
ROSEUSDT      n= 9    +$2,345.07        KAVAUSDT    n= 8  −$2,106.62
                                        IMXUSDT     n= 7  −$3,921.57
                                        DOTUSDT     n=10  −$4,095.56
                                        AVAXUSDT    n=11  −$5,055.18
                                        FILUSDT     n= 7  −$7,414.62
                                        RUNEUSDT    n= 8  −$8,836.49
                                        ETCUSDT     n=15 −$16,758.46
```

At n=5–15 per symbol this is **noise, not symbol selection signal.** Do not
read a shortlist out of it. The pre-registered "no single symbol >40% of P&L"
gate passed throughout.

---

## 3. Journal label defect (documented, uncorrected upstream)

**7 of 24 winners are mislabeled.** The engine writes `outcome: "TARGET"` on
504h max-hold force-closes even when the target was never touched:

```
2026-06-05T16:00:05Z  ROSEUSDT      +4.09R
2026-06-22T08:00:03Z  IMXUSDT       +2.69R
2026-07-08T20:00:00Z  1000SHIBUSDT  +4.21R
2026-07-08T20:00:00Z  APTUSDT       +0.95R
2026-07-09T16:00:05Z  1INCHUSDT     +0.31R
2026-07-28T04:00:00Z  XLMUSDT       +4.95R
2026-08-03T04:00:00Z  BCHUSDT       +2.94R
```

Realized-R truth: **17 genuine TARGET / 7 max-hold WIN / 110 STOP / 0
max-hold LOSS.**

P&L is unaffected (`pnl_usd` is computed from actual exit price). Only the
`outcome` string lies. Any future analysis of this journal must classify by
realized R — `R = (entry − exit) / |entry − stop|` for SHORT — not by label.
Documented in `CLAUDE.md`.

---

## 4. What this establishes

**Established:**

1. **The strategy class is not viable at taker fees on this venue.** Gross
   edge +$11.4k against $13.0k costs over 134 trades. The mechanism is
   understood (56× implicit leverage on a tight wick stop, fees on notional).
2. **The engine works.** 88 days, 16 symbols, 8 shadows, zero unrecovered
   crashes, journal-replay survived restarts, no orphaned positions.
3. **Consistency with backtest.** The final weekly drift decomposition
   (2026-08-02) was BENIGN-consistent: target-win like-for-like gap −0.8%,
   stop-overshoot +1.6bp. Live geometry matched the reference — the strategy
   did what the backtest said it would. **It was the cost model, not the
   signal, that decided the outcome.**

**NOT established:**

1. **The formal verdict never fired.** Terminated at 134 trades against a
   pre-registered ≥150 threshold, and at day 88 of a run whose open book
   would have self-resolved by 08-18. The promotion check read BLOCKED
   throughout, but it was never allowed to read anything else.
2. **The final P&L sign is not a result.** See §2. Two unresolved positions
   could have moved it either way.
3. **Nothing about other venues.** Maker rebates or a cheaper fee structure
   were never tested. The cost-geometry finding says this strategy fails *at
   these fees*; it does not say it fails everywhere.

**Prior closed threads** (do not reopen — each has a pre-registered verdict):
strategy-class search closed at N≈85 (`docs/RESEARCH_BACKLOG.md`); regime
overlays VOID (F=0/9, `project_regime_switch_void`); alt-signals NO-GO;
sub-5m strategies presumptively fee-dead (`project_purgatory_method_feedeath`).

---

## 5. Why it was stopped early

Operator decision, 2026-08-04. Three independent constraints, none of which
any verdict outcome would have moved:

1. **Ceiling.** $575–1,725/mo at full STAGE_4 sizing, on the strategy's own
   honest numbers.
2. **Cost geometry.** Structural (§2), not a tuning problem.
3. **Venue access.** Binance confirmed 2026-06-10 that EEA futures activation
   is not planned, with no timeline. Real money would have required a full
   executor port to another venue plus fresh Layer 2/3 validation.

The counter-argument, recorded honestly: **a completed pre-registered record
is worth more than a truncated one**, and 21 more days would have cost ~€15
and about an hour a week. That argument was made and rejected. The cost of
early termination is §4's "NOT established" list — the run answered the
scientific question (costs exceed edge) but not the procedural one (what the
locked rule outputs when allowed to fire).

---

## 6. Final state

- **Real money deployed: $0.** At no point did this system trade real funds.
- Last drift checkpoint: 2026-08-02, **HOLD (13th consecutive)**, no escalation.
- Last commit before close-out: `cf0daa8`.
- 2 positions open and abandoned (§1).
- **VPS decommissioned and destroyed 2026-08-04.** 16 live engines + 8 research
  shadows stopped and disabled, timers and cron removed, `/opt/trading-engine`
  and `/var/log/paper-live` deleted, `/etc/paper-live/env` shredded, Hetzner
  server destroyed from console. **Nothing of this system runs anywhere.**
- `results/journal_cache/` is gitignored. All 417 raw journal files (live +
  8 shadows + Layer 3) were archived before shutdown to:

  ```
  ~/Main/notes/ai/fin-trading-engine_journals_final_2026-08-04.tar.gz
  151 KB   sha256 46e2cb5719c16a5b02e6a66a887e12e007f11c0c24949b474ef9686614e7876a
  ```

  Verified restorable before the wipe: extracted clean, 417 files, reproduced
  the 134-trade / −$1,626.58 book exactly.

  > **This tarball is now the ONLY copy of the raw trade data.** The VPS was
  > destroyed and the local `results/journal_cache/` is gitignored and
  > untracked. If both are lost, the per-trade record is unrecoverable and only
  > this document plus the committed weekly snapshots in
  > `results/decision_snapshots/` and `results/forward_paper_snapshots/`
  > survive. Consider a second copy if the raw stream has any future value.

The strategy was falsified on cost grounds, with the mechanism understood and
the gross edge confirmed real. That is a complete finding, arrived at without
losing money.
