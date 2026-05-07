# Cat X — Cross-Exchange OOS Replication on Bybit — PRE-REGISTERED Decision Rule

**Status:** Locked before any data is pulled or backtest runs. Same
discipline as F1, F2, Cat G, Cat G', sample-split bootstrap, edge stability,
kill-bar/drift-detector calibrations.

This is the **first cross-axis hypothesis** after the F-series exhaustion
(Cat G + Cat G' both REJECTED). Per the lessons.md "≥5 tests, marginal-or-
negative, pivot axes" rule, we are pivoting away from F1×F2 mechanism
parameter tuning and onto an entirely different alpha axis: independent
out-of-sample validation on a different exchange.

## Question

The deployed strategy (4H short EMA9/21, mh504, RR6, fee=10/slip=5) was
developed and validated entirely on **Binance** USDT-Perp data. The
+$130k/yr cumulative claim, the bootstrap CI [+$42k, +$220k]/yr, and the
walk-forward CI [-$111k, +$372k]/yr all rest on Binance kline data.

**Risk:** the strategy's edge could be Binance-specific microstructure.
Binance has the deepest crypto-perp liquidity and most predictable
funding mechanism. Other exchanges differ on tick size, taker fee
schedule, funding rate dynamics, and order-book microstructure. If the
strategy works *only* on Binance, deploying real money assumes Binance-
specific characteristics persist; if those change, the edge dies.

**Test:** does the same strategy produce **comparable** results on Bybit
USDT-Perp data for the same symbols and time period?

## Method

### Data acquisition
- Pull 4H klines from Bybit `v5/market/kline` for the deployed-16
  symbols, full available history (most launched 2021-2022)
- Naming: 1000SHIBUSDT → SHIB1000USDT on Bybit (verified, only known
  mismatch)

### Backtester
- Pure Python, 4H-granularity stop/target resolution. The Go backtest
  binary uses 1m kline → tick expansion which we will NOT replicate at
  cross-exchange level (data volume is prohibitive in one session).
- Strategy logic exactly matching deployed:
  - 4H EMA9/21 cross signal: bearish cross fires SHORT
  - Side filter: SHORT only
  - Entry at 4H candle close
  - Stop = candle high × (1 + stop_buffer_pct) — using the default
    `stop_buffer_pct=0` (wick-based, locked from deployed config)
  - Target = entry − stop_dist × 6 (RR=6:1)
  - Max-hold force-close at 504h
- Cost stack: 10bp round-trip fee, 5bp slip on losers. **No funding
  modeled in this analysis** (caveat documented; impact is small per A1
  finding that net funding ≈ $0 in steady state).
- Same-bar resolution: when both stop and target lie within the same
  subsequent 4H bar's high/low range, default to STOP (pessimistic for
  SHORT — high-first ordering).

### Apples-to-apples baseline
- Run the **same 4H-granularity backtester** on Binance 4H klines (built
  from cached 1m data) to establish a direct comparison baseline. The
  $130k/yr Binance number from A1 uses 1m granularity which is finer.
  Comparing 4H Bybit to 1m Binance would conflate granularity with
  exchange.

### Comparable windows
Bybit data availability constrains the analysis to windows where all
deployed-16 symbols are tradable on Bybit:

| Window | Range | Bybit coverage |
|:---:|:---|:---|
| W0 | 2022-05 → 2023-04 | partial (APT from Oct 2022) |
| W1 | 2023-05 → 2024-04 | full |
| W2 | 2024-05 → 2025-04 | full |
| W3 | 2025-05 → 2026-04 | partial (sample ends Apr 2025) |

**4 comparable windows.** W-2 and W-1 are excluded (most Bybit symbols
not yet listed).

## Decision rule (LOCKED)

For each comparable window, compute Bybit total NET and Binance-4H
total NET on the SAME deployed-16 symbol set with the SAME backtester.

- **Sign match**: window has same NET sign on both exchanges (both >0 or both ≤0)
- **Bybit total NET ratio**: sum(Bybit NET across 4 windows) / sum(Binance-4H NET across 4 windows)

| Tier | Conditions (ALL must hold) |
|---|---|
| **ROBUST**          | Bybit total NET ≥ 0.5× Binance-4H total NET AND ≥3/4 windows sign-match AND Bybit total NET > 0 |
| **WEAK_REPLICATION** | Bybit total NET > 0 AND ≥2/4 windows sign-match |
| **BINANCE-SPECIFIC** | Bybit total NET ≤ 0 OR <2/4 windows sign-match |

Tiebreaker: if WEAK_REPLICATION conditions hold but ROBUST also holds,
ROBUST wins (more specific).

## What is NOT permitted

- ❌ No symbol substitution (deployed-16 only; if a Bybit symbol's
     coverage is short, that symbol contributes $0 in early windows)
- ❌ No alternate cost stack (10bp/5bp locked, no funding modeled
     uniformly across both)
- ❌ No alternate granularity (4H locked for both exchanges)
- ❌ No alternate same-bar resolution rule
- ❌ No re-running with different time windows
- ❌ No "saving" BINANCE-SPECIFIC by reframing to "regional" or "venue"
- ❌ No cherry-picking which windows count

A BINANCE-SPECIFIC outcome is decisive: the deployed strategy's edge
does not generalize to Bybit at this granularity and cost stack. The
follow-up question (separate pre-reg) would be "what's different — the
funding mechanics, the microstructure, the participants?" — but that
is a separate hypothesis.

A WEAK_REPLICATION is moderately encouraging — mechanism partially
generalizes but with reduced strength. Most likely outcome given the
known volume and microstructure differences.

A ROBUST outcome is strong: the mechanism is exchange-independent.

## Caveats acknowledged in advance (NOT loopholes)

1. **No funding modeled in either backtest.** Aggregate funding ≈ $0
   over 5y per A1, but per-window funding can swing ±$10k. This adds
   noise but doesn't bias toward one exchange.
2. **4H granularity coarsens stop/target resolution.** Both backtests
   suffer this equally; the comparison is internally consistent.
3. **Bybit volume is lower for most altcoins.** This affects realized
   slippage in real trading but not the modeled-cost backtest. The
   verdict reflects "would the modeled strategy work" — real-money
   slippage divergence is a separate concern.
4. **Bybit kline timestamps are at the open of each bar.** Standardized
   to UTC; same convention as Binance. No timestamp drift.

These are caveats, not get-out clauses. The verdict applies mechanically
regardless.

## Pre-registered priors

| Outcome | Prior |
|---|---:|
| ROBUST              | 35% |
| WEAK_REPLICATION    | 35% |
| BINANCE-SPECIFIC    | 30% |

Reasoning: the strategy mechanism (EMA cross + RR + max-hold) is
broad enough to plausibly work on any liquid USDT-Perp exchange. But
Bybit's lower volume and slightly different microstructure could dampen
the edge. Equal mass on ROBUST/WEAK reflects real uncertainty; the 30%
on BINANCE-SPECIFIC captures the genuine risk that some piece of
Binance-specific structure is load-bearing.

## What this WILL and WILL NOT produce

WILL: a mechanical tier verdict comparing 4-window Bybit vs Binance-4H
totals. Per-window breakdown for diagnostic.

WILL NOT: a re-validation of the +$130k/yr Binance claim — that's
separately validated by A1, A3, edge-stability. Cat X tests
generalization, not the original claim.

## Files

- `results/cat_x_bybit_replication_decision_rule_2026-05-08.md` — this file
- `scripts/cat_x_bybit_pull.py` — Bybit data downloader
- `scripts/cat_x_bybit_replication.py` — analyzer with 4H backtester
- `data/bybit/<SYMBOL>-4h.csv` — pulled Bybit data (gitignored if large)
- `results/cat_x_bybit_replication_2026-05-08.txt` — raw output
- `results/cat_x_bybit_replication_verdict_2026-05-08.md` — verdict
