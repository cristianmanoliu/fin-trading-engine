# Deployed-16 Alternative Signals & Sides — VERDICT: NOTHING BEATS THE LIVE CONFIG

**Status:** Research finding, 2026-07-27. Direct test on the **deployed-16** (not universe-57)
of the two questions the operator asked: "maybe not shorts?" and "other technical indicators?"
**Answer to both: no.**

**Span:** TRAIN 2020-01 → 2023-12, TEST 2024-01 → 2025-04. Deployed-16 only, live costs
(`--fee-bps 10 --stop-slippage-bps 5`), audit fills, historical funding.

## 1. Side test — longs lose money on this book

| Side | Trades | WR | Gross | Fees | NET |
|---|---:|---:|---:|---:|---:|
| **SHORT (live)** | 1,526 | 21.1% | $471,081 | $79,935 | **$378,951** |
| LONG | 1,710 | 15.7% | $68,545 | $96,895 | **−$111,118** |
| BOTH | 2,600 | 18.9% | $521,620 | $135,682 | $314,710 |

Longs are **−$111k** on the deployed-16. Both-sides dilutes short-only by −$64k: the long leg
adds trades and fees without adding edge. The short-only filter is confirmed on this book, not
merely inherited from the universe-57 result.

## 2. Alternative signals — all lose to EMA on TRAIN

All short-only, live costs, deployed-16:

| Signal | Trades | WR | Gross | Fees | NET |
|---|---:|---:|---:|---:|---:|
| **EMA 9/21 (live)** | 1,526 | 21.1% | $471,081 | $79,935 | **$378,951** |
| MACD 12/26/9 | 2,431 | 18.1% | $300,878 | $125,141 | $170,329 |
| RSI-14 cross-50 | 2,657 | 19.0% | $271,608 | $108,279 | $168,365 |
| Bollinger 20/2.0 | 1,535 | 21.4% | $207,648 | $47,637 | $147,644 |
| Momentum candle | 3,518 | 17.8% | $188,312 | $128,718 | $76,952 |
| VWAP deviation 2% | 409 | 33.0% | −$33,819 | $28,179 | −$70,646 |
| PDH/PDL breakout | 2,836 | 15.0% | −$29,606 | $318,821 | **−$470,833** |

Note PDH/PDL: $318,821 of fees on $2.8k of gross — a textbook restatement of the cost geometry
(tight stops → enormous notional → fee death).

## 3. The one apparent TEST winner is noise (RSI)

RSI edged EMA on the held-out period ($312,807 vs $301,810). Paired per-symbol test:

| Span | Δ (RSI − EMA) | t | perm p | beats | median | drop top-3 |
|---|---:|---:|---:|---:|---:|---:|
| TRAIN | **−$210,586** | −3.13 | **0.006** | 3/16 | −$8,980 | −$236,899 |
| TEST | +$10,997 | +0.17 | 0.865 | 11/16 | +$3,965 | −$46,309 |

**RSI loses significantly on TRAIN (p=0.006, 3/16) and wins insignificantly on TEST (p=0.865).**
A sign flip across periods with a non-significant winner is the signature of noise, not edge.
Dropping the top-3 symbols flips TEST negative too. **No promotion.**

## Verdict

Combined with `deployed16_direct_sweep_verdict_2026-07-27.md` (15 arms: stops, exits, filters),
this closes the parameter and signal search on the actual traded book:

- **Sides:** short-only confirmed; longs −$111k, both-sides −$64k vs live.
- **Signals:** EMA 9/21 beats MACD, RSI, Bollinger, momentum, VWAP, PDH/PDL on TRAIN; the only
  TEST "winner" fails significance and flips sign.
- **Stops/exits/filters:** live config ranks 2/15, sole arm above it is noise.

**The live configuration is the best available parameterization of this strategy class for
these 16 symbols.** The problem is not the signal, the side, the indicator, the stop, the exit,
or the filter — it is the cost geometry (67× leverage; live gross +$3,489 vs $11,238 costs;
breakeven fee 0.20 bp) documented in `stop_distance_cost_filter_verdict_2026-07-26.md`.

## Budget note

This arc adds ~12 trials (N ≈ 73 → ~85). Every arm raises the multiple-testing bar for
everything already found. **The search is closed; further arms are actively counterproductive.**
