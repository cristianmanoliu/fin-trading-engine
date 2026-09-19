# PRE-REGISTRATION — Crash overlay paper trading criteria

**Status: LOCKED 2026-09-19, before paper trading begins.**
**Type:** paper trading gate. Mechanical pass/fail after 3 months of live paper data.
**Prerequisite:** C5 (venue access) from `crash_overlay_revival_prereg_2026-09-19.md` must PASS first. This document does not activate until C5 is confirmed.

---

## 1. Purpose

The backtest verdict (`crash_overlay_revival_verdict_2026-09-19.md`) returned CONDITIONAL ACCEPT with 4/4 gates passing. This pre-registration defines the paper trading criteria that must hold before real money is deployed.

Paper trading tests what backtest cannot: execution reliability on a real venue, realized slippage under live order book conditions, and the engine's ability to run unattended for months.

## 2. Paper trading parameters (frozen)

| Parameter | Value | Source |
|---|---|---|
| Venue | Kraken Futures | C5 gate |
| Symbols | 13 (frozen in backtest pre-reg) | `crash_overlay_revival_prereg_2026-09-19.md` §3 |
| EMA | 5/15 | Frozen in backtest pre-reg |
| Signal TF | 4H | Frozen |
| Side filter | short only | Frozen |
| R:R | 6:1 fixed | Frozen |
| Max hold | 504h | Frozen |
| Stake | $500 per trade | Frozen (liquidity bound) |
| Fee model | Kraken taker (confirmed on first manual trade) | C5 gate |

**Symbol list:** 1000SHIB, AAVE, ARB, BCH, CRV, HBAR, INJ, LTC, NEAR, SUI, UNI, WLD, XLM.

**Kraken symbol mapping:** PF_SHIBUSD (raw SHIB, engine must ×1000 unit conversion), PF_AAVEUSD, PF_ARBUSD, PF_BCHUSD, PF_CRVUSD, PF_HBARUSD, PF_INJUSD, PF_LTCUSD, PF_NEARUSD, PF_SUIUSD, PF_UNIUSD, PF_WLDUSD, PF_XLMUSD.

## 3. Duration

**Minimum 3 calendar months** from the first automated trade. No early termination unless a kill trigger fires (see §5).

Rationale: the crash overlay is a regime product. 3 months gives enough time to observe both bleed (non-crash) and possibly one crash payout. At ~1.5 trades/day across 13 symbols, expect ~135 trades in 3 months.

## 4. Accept criteria (all must hold)

### P1. Execution reliability

All entries and exits must execute without manual intervention. "All" means: every signal the engine emits results in a Kraken order that fills, and every stop/target/max-hold exit results in a close order that fills.

**Threshold:** >= 95% automated fill rate. Failures must be logged and explained (API timeout, order book gap, rate limit). Systematic failures (same symbol, same error) are a FAIL regardless of rate.

### P2. Realized slippage

Realized slippage (difference between modeled fill price and actual fill price) must be within 2x of the backtest model.

**Threshold:** median realized slippage <= 10 bp (2x the 5 bp modeled in backtest).

Measured on all closed trades, entry and exit separately. Entry slippage: |actual_entry - signal_entry| / signal_entry. Exit slippage: |actual_exit - modeled_exit| / modeled_exit.

### P3. No single-symbol concentration

No single symbol may contribute > 40% of cumulative P&L (positive or negative).

**Rationale:** a crash overlay must not degenerate into a single-name bet. The backtest book is diversified; paper trading confirms the live book stays diversified.

### P4. Bounded bleed

In any rolling 30-day non-crash window (defined as BTC return >= -5% over the same 30d), cumulative P&L must not exceed -$7,500.

**Rationale:** the backtest median non-crash quarter was -$10,576 (~-$3,525/month). The $7,500/month ceiling is ~2.1x the backtest monthly bleed rate. This catches execution-cost surprises (wider real spreads, funding differences) without rejecting normal bleed.

### P5. Fee confirmation

Realized per-trade fee must match the Kraken fee tier used in the backtest model (10 bp round-trip taker at base tier, or lower if a fee tier is reached).

**Threshold:** realized round-trip fee <= 12 bp (20% tolerance over 10 bp model). If realized fees exceed 12 bp, the cost model must be re-evaluated before proceeding.

## 5. Kill triggers (immediate stop, no recovery)

- Engine offline > 24h without operator awareness (missed crash exposure).
- Kraken API access revoked or account restricted.
- Any single trade with realized slippage > 100 bp (order book failure).
- Cumulative P&L < -$25,000 (5x the quarterly bleed median; catastrophic execution failure, not normal bleed).

## 6. What ACCEPT would mean

Paper trading ACCEPT means the crash overlay is operationally viable on Kraken: the engine executes reliably, slippage is within model, and the book is diversified.

**Next step on ACCEPT:** pre-register real-money sizing protocol (separate document). Expected sizing: $500/trade (same as paper), ~$15k capital allocation.

**This is a hedge product.** Real-money deployment does not require positive paper-trading P&L. The paper period tests execution, not edge. The edge case was settled in the backtest verdict.

## 7. What REJECT would mean

Paper trading REJECT means the execution environment is not viable at the current stake and symbol set. Possible responses (to be decided at rejection time, not pre-committed):

- Reduce stake to $250 (halves notional, may widen the viable symbol set).
- Drop symbols that fail execution reliability.
- Evaluate OKX EU as alternative venue.
- Close the project if no viable venue/stake combination exists.

## Cross-references

- Backtest pre-reg: `results/crash_overlay_revival_prereg_2026-09-19.md`
- Backtest verdict: `results/crash_overlay_revival_verdict_2026-09-19.md`
- Venue scouting: `results/venue_scouting_2026-06-10.md`
- Quiz primer: `docs/kraken_appropriateness_quiz_primer.md`
- Quant method: `docs/QUANT_METHOD.md`
