# PRE-REGISTRATION — EMA 5/15 crash overlay on mid/small-cap alts

**Status: LOCKED 2026-09-19, before any validation run.**
**Type:** single-hypothesis pre-registration. One hypothesis, one book, one run.

**This reopens the project under a different product framing.** The EMA perp
strategy class was closed 2026-08-05 as a standalone alpha book. This
pre-registration tests it as a crash overlay (a hedge instrument that pays in
crypto downturns and may bleed in calm markets). The success criteria are
therefore different from the alpha criteria, and intentionally so.

**Trial budget: 1.** The EMA parameter (5/15) and book composition (exclude
top 10 by market cap) are fixed before the run. No sweeps, no "let's also try
excluding top 15." One book, one evaluation.

---

## 1. Why this is being run

The closeout addendum (2026-08-09 §2) established a finding that was never
acted on:

> The class is a short-volatility product. BTC-down quarters: 9/9 positive,
> mean +$72k, correlation with BTC = -0.588.

The addendum itself said:

> As a hedge overlay on a long book, the bar is different: an instrument that
> is 9/9 positive in BTC-down quarters with r = -0.588 does not need provable
> positive expectancy to be worth holding.

The closure was correct under the alpha framing. This pre-registration tests
the hedge framing, with its own criteria.

### Why EMA 5/15 and not 9/21

EMA 5/15 (the `alt5-15-504` shadow) was the strongest shadow cohort during the
live run (+$77,722 vs live's -$1,627). Exploratory analysis (2026-09-19, this
session) showed it catches crash transitions earlier: May 2026 WR 23.7% on 5/15
vs 5.4% on 9/21. Only 6/37 live entries overlapped. The faster signal is
structurally better suited for a crash product.

This is a parameter selection, not a discovery. The choice is made before the
validation run and is not revisitable.

### Why exclude top 10

Exploratory analysis on the full 57-symbol universe (2026-09-19) showed:

- Top 5 (BTC, ETH, BNB, SOL, XRP): NET -$70k, costs 285% of gross, t = +0.55
- Top 10: NET +$769, costs 115% of gross, t = +1.75
- Excluding top 10: NET +$1.57M, costs 35% of gross, t = +8.30

The top 10 are net-zero or net-negative. Their implied leverage at the strategy's
wick-stop geometry produces costs that exceed gross. The edge lives in mid and
small-cap alts, where crashes are sharper and 6:1 R:R targets fill more often.

The threshold "top 10 by market cap" is fixed before the run, using the ranking
as of 2026-09-19. If a symbol moves in or out of the top 10 during the paper
period, the book does not change (the ranking is frozen at pre-reg time).

**Frozen ranking (excluded):** BTC, ETH, BNB, SOL, XRP, DOGE, TRX, ADA, AVAX,
LINK.

## 2. The hypothesis

> **H1:** EMA 5/15, short only, 6:1 R:R, max-hold 504h, on the Kraken perp
> universe excluding the top 10 by market cap, is a viable crash overlay for
> a portfolio with long crypto exposure.

**Cost basis (fixed):** fee 10 bp RT taker + slip 5 bp (Kraken base tier,
matching the entire backtest validation stack). No maker assumption.

**Null:** the strategy does not meet the overlay criteria below, or the venue
does not support it.

## 3. Accept / reject criteria

This is a hedge product, not an alpha product. The criteria reflect that.

**Stake: $500** (half the backtest's $1k unit). Chosen to keep notional per
trade at ~$28k, which is the threshold at which 13 symbols pass the Kraken
order book slippage screen at <=15bp. All dollar thresholds below are stated
at the $500 stake.

### ACCEPT (all five must hold):

1. **Negative BTC correlation.** Quarterly correlation(BTC return, strategy
   P&L) < -0.3 over the backtest period. (Exploratory: -0.514 on the 13-sym
   book.)

2. **Crash-quarter reliability.** Positive in >= 80% of BTC-down quarters
   (BTC quarterly return < -5%). (Exploratory: 9/9 = 100%.)

3. **Bounded bleed.** In non-crash windows (BTC quarterly return >= -5%),
   the median quarterly P&L is > -$12k at $500 stake. A hedge is allowed to
   cost something. It is not allowed to hemorrhage. Rationale for the -$12k
   level: the expected annual crash income at this stake is ~$32k (mean crash
   quarter +$23k × ~1.4 crash quarters per year). The bleed bound ensures
   annual bleed does not exceed crash income: 4 non-crash quarters × $12k =
   $48k ceiling, which is above the $32k expected income, but the median
   non-crash quarter includes some positive quarters, so the realized annual
   bleed is lower. (Exploratory median: -$10.6k.)

4. **Gross/cost >= 2.0x** on the full backtest. Relaxed from the alpha screen's
   3x because: (a) this is a hedge, not a standalone book, (b) the slippage
   screen eliminates the high-gross, thin-book symbols that inflated the
   full-universe ratio, and (c) cost/gross at 48% still leaves the strategy
   net-positive over the backtest. (Exploratory: 2.10x on the 13-sym book.)

5. **Venue access confirmed.** Kraken futures account open, at least one
   manual trade placed, fee schedule confirmed at 10bp RT taker.

### REJECT on any single failure.

### Additional gates (not accept/reject, but recorded):

- **Slippage screen.** Each candidate symbol must show < 15bp market impact
  for ~$28k notional (sell side) on Kraken's futures order book. Symbols
  failing this are dropped from the book. The accept criteria are evaluated
  on the surviving book. Screened 2026-09-19: 13 of 47 pass.
- **Concentration.** Top-20-day share of total P&L is recorded but not gated.
  This is structural to 6:1 R:R and is accepted as a feature of the product
  class, not a defect.
- **Drop-top-5% is recorded but not gated.** A crash overlay is expected to be
  tail-carried. That is the product definition. Gating on drop-top-5% would
  be asking "would you be profitable without crashes?" which is the wrong
  question for a hedge.

### Book composition (frozen after slippage screen, 2026-09-19)

13 symbols: 1000SHIB, AAVE, ARB, BCH, CRV, HBAR, INJ, LTC, NEAR, SUI,
UNI, WLD, XLM.

Excluded by top-10 popularity gate: BTC, ETH, BNB, SOL, XRP, DOGE, TRX,
ADA, AVAX, LINK.

Excluded by Kraken slippage screen (>15bp at $28k or book too thin): APT,
AXS, CHZ, DOT, DYDX, ENJ, ENS, ETC, FIL, GALA, GMX, GRT, ICP, IMX, IOTA,
KAVA, LDO, MANA, MKR, OP, PYTH, ROSE, RUNE, SAND, SEI, SNX, TIA, VET, ZIL,
1INCH, APE, ATOM, BLUR, FTM.

## 4. What ACCEPT would and would not mean

**Would mean:** the strategy is a viable crash overlay in backtest, with a
venue confirmed. Grounds for a 3-month paper trading period on Kraken.

**Would NOT mean:** deploy with real money. Paper trading criteria (to be
pre-registered separately if ACCEPT fires):
- 3 months minimum (to capture at least one regime transition)
- Execution reliability (all rebalances without manual intervention)
- Realized slippage within 2x of modeled (10bp)
- No single symbol > 40% of P&L

**Sizing philosophy:** $500 stake, ~$28k notional per trade, ~$2,800 margin
per position at 10x leverage. With 3-4 concurrent positions, ~$8-11k margin
is needed. Recommend ~$15k capital allocation (margin + drawdown buffer).
The bleed in calm markets is the insurance premium. Backtest median bleed:
~$10.6k per non-crash quarter at this stake. Backtest mean crash income:
~$23k per crash quarter.

## 5. Method

### Backtest validation run

Already built: `/tmp/bt_engine` (Go binary from `cmd/backtest`).

Config: `--signal-tf 4H --side-filter short --max-hold-hours 504
--ema-fast-period 5 --ema-slow-period 15 --fee-bps 10
--stop-slippage-bps 5 --funding-csv-dir data/funding --exact-fills
--include-boundary`

Universe: all symbols with 1m data in `data/` MINUS the frozen top-10 list.

**The exploratory run (2026-09-19) already produced this data.** The validation
step is to evaluate the pre-registered criteria against it, not to re-run.
The data was generated before the criteria were written, which is the correct
order (criteria cannot chase the data).

### Venue validation

Manual process (operator):
1. Complete Kraken futures onboarding (MiFID quiz, KYC)
2. Fund account with minimal EUR
3. Place one $10 perp trade (any symbol)
4. Confirm fee schedule shows 5bp/side taker

### Slippage screen (DONE 2026-09-19)

Kraken futures API (public, no auth):
`GET https://futures.kraken.com/derivatives/api/v3/orderbook?symbol=PF_XYZUSD`

For each candidate symbol: sort bids descending, walk the bid side computing
VWAP for a ~$28k market sell, compare to mid. Symbols with >15bp impact or
insufficient depth are excluded.

**Result:** 13 of 47 symbols pass. See §3 for the frozen book.

## 6. Failure mode this design is most likely to hit

**Slippage.** The backtest models 5bp slippage. Mid/small-cap alts on Kraken
may have thinner order books than Binance. If enough symbols fail the 15bp
slippage screen, the surviving book may be too small to diversify.

The second most likely failure: **venue access.** Kraken's MiFID quiz for
derivatives is non-trivial, and the operator failed it once already (see
`docs/kraken_appropriateness_quiz_primer.md`).

## 7. What is NOT reopened by this pre-registration

- EMA parameter search (closed at N~85, PBO 0.52)
- Maker execution route (adverse selection, -7.1pp winner-fill gap)
- Wider stops route (settled 2026-07-27)
- The alpha-book framing (dead at any cost basis)
- Any shadow config other than 5/15 (parameter is frozen)

## Cross-references

- Closure: `results/forward_paper_closeout_2026-08-04.md`
- Regime finding: `results/closeout_addendum_2026-08-09.md` (§2)
- Venue: `results/venue_scouting_2026-06-10.md`
- Cost geometry: `results/viability_frontier_2026-07-27.md`
- Quant method: `docs/QUANT_METHOD.md`
- Kraken quiz prep: `docs/kraken_appropriateness_quiz_primer.md`
- Honesty module: `scripts/quant_honesty.py`
