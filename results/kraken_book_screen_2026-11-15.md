# Kraken book screen at $56k notional, and the unmodeled entry cost (2026-11-15)

**Status: exploratory. Not a verdict. The locked pre-regs stay unchanged.**

**Disposition 2026-11-15: the operator closed the crash overlay on Result 3. No C5 trade, no paper test.**

Question: does the frozen 13-symbol crash overlay book hold at a $1k stake
(about $56k notional per trade, double the pre-reg $500 stake)?

Method: `scripts/kraken_book_screen.py`. Same method as
`crash_overlay_revival_prereg_2026-09-19.md` §5: walk the bid side for a
market sell, VWAP vs mid, gate < 15bp. Median of 3 public order book
snapshots, 20s apart. Taken on a Sunday, so books may be thinner than on
weekdays. The 2026-09-19 screen was on a Saturday.

## Result 1: 11 of 13 pass at $56k

| | touch (half spread) | $14k | $28k | $56k |
|---|---:|---:|---:|---:|
| Book median impact, bp | 4.2 | 8.9 | 10.0 | 12.1 |

- Fail at $56k: 1000SHIB (25bp) and CRV (20 to 23bp). Both also fail at $28k today.
- Of the 34 excluded symbols, only DOT passes at $56k (14bp). The book is
  frozen, so DOT stays out.

## Result 2 (the important one): entry cost is not in the backtest

`pkg/execution/stub.go` charges `StopSlippageBps` on stop and time-stop exits
only. Entries fill at the signal price with zero slippage. Take-profit exits
are limit orders and also slip zero.

A live market sell pays the impact above: about 10bp at $28k. Stop exits pay
about 10bp, not the modeled 5bp. Extra cost per trade, at about 80% stop exits:

- $500 stake: 10 + 0.8 x 5 = about 14bp. At about $1,250 a year per bp
  (482 trades a year, about $2.6 per bp per trade), that is about $17k a year.
- $1k stake: 12 + 0.8 x 7 = about 18bp, about $45k a year.

Backtest net (verdict 2026-09-19): $19.7k a year at $500, $39.5k at $1k.
**At today's Kraken books, the extra cost takes about all of the backtest
profit at $500, and more than all of it at $1k.** A smaller stake does not
fix it: the half spread alone is 4bp, and $14k still costs 9bp.

## What this means

- P2 of the paper pre-reg (median realized slippage <= 10bp) sits right at
  today's measured level.
- A cost-corrected backtest (entry slip about 10bp, stop slip about 10bp) is
  the cheap next check. If it is near zero, the 3-month paper test is moot.

## Result 3: the backtest at real costs (same day)

Run: pre-reg config, 13 symbols, $500 stake, 2020-01 to 2026-07,
`--signal-tf 4H --side-filter short --max-hold-hours 504 --ema-fast-period 5
--ema-slow-period 15 --fee-bps 10 --stop-slippage-bps 5 --funding-csv-dir data/funding
--exact-fills --include-boundary --journal-dir <out>/<SYM>`. Reproduces the
verdict: 3,174 trades, net $126.6k (verdict $128.3k).

`scripts/crash_overlay_reprice.py <dir>` adds the book cost to each journal
trade: entry slip on all trades, and stop slip in place of 5bp on market exits
(stops and time stops, 86% of trades). Median notional is $19.6k at $500, so
the cost comes from the book curve at that size.

| scenario, $k | 2020 | 2021 | 2022 | 2023 | 2024 | 2025 | 2026 | total | per year | worst 2 years |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| $500, backtest costs | -17 | -37 | 39 | 18 | 40 | 79 | 4 | 127 | 19.3 | -54 |
| $250, 8bp | -11 | -22 | 14 | -2 | 12 | 32 | -2 | 20 | 3.0 | -33 |
| $500, 9.5bp | -23 | -46 | 25 | -10 | 19 | 59 | -7 | 17 | 2.6 | -69 |
| $500, 7bp (weekday guess) | -21 | -43 | 29 | -1 | 26 | 66 | -3 | 55 | 8.4 | -64 |
| $1k, 11bp | -49 | -96 | 43 | -32 | 29 | 111 | -18 | -12 | -1.8 | -145 |

Break-even at $500: 10 to 11bp of book cost. Measured: about 9.5bp.

**Reading:** at real Kraken costs the overlay makes about $3k a year at $500,
against a worst two years of -$69k. This is before the live gap of the
2026-08-09 addendum (live gross was 27% of backtest gross). Not worth a
3-month paper test as a money maker. The locked pre-regs are not changed by
this note.
